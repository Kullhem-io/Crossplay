package transport

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// InboundHandler receives a decoded client message. The hub calls it for every
// inbound frame; the engine wires its own handler in. Keeping this an injected
// func keeps transport free of any game knowledge.
type InboundHandler func(msg Inbound)

// Hub fans server events out to every connected client and funnels inbound
// client messages to a single handler. One Hub is shared process-wide.
type Hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
	onMsg   InboundHandler
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*client]struct{})}
}

// OnMessage registers the handler invoked for each inbound client message.
func (h *Hub) OnMessage(fn InboundHandler) { h.onMsg = fn }

// Broadcast sends an event to every connected client. Slow clients are dropped
// rather than allowed to block the engine.
func (h *Hub) Broadcast(ev Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		log.Printf("hub: marshal %s: %v", ev.Type, err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			// Buffer full: client is too slow. Close it; it can reconnect.
			c.cancel()
		}
	}
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

type client struct {
	conn   *websocket.Conn
	send   chan []byte
	cancel context.CancelFunc
}

// ServeWS upgrades an HTTP request to a WebSocket and runs the read/write
// pumps until either side closes.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// M0: dev convenience. Tighten OriginPatterns before any deploy.
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("hub: accept: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	c := &client{conn: conn, send: make(chan []byte, 64), cancel: cancel}
	h.add(c)
	log.Printf("hub: client connected (%d total)", len(h.clients))

	defer func() {
		h.remove(c)
		cancel()
		conn.Close(websocket.StatusNormalClosure, "")
		log.Printf("hub: client disconnected (%d total)", len(h.clients))
	}()

	// Greet the new client.
	if data, err := json.Marshal(Event{Type: EvHello, Payload: map[string]any{
		"msg": "connected to crossplay",
	}}); err == nil {
		c.send <- data
	}

	go h.writePump(ctx, c)
	h.readPump(ctx, c)
}

func (h *Hub) writePump(ctx context.Context, c *client) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-c.send:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.conn.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				c.cancel()
				return
			}
		}
	}
}

func (h *Hub) readPump(ctx context.Context, c *client) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return // normal on disconnect
		}
		var msg Inbound
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("hub: bad inbound: %v", err)
			continue
		}
		if h.onMsg != nil {
			h.onMsg(msg)
		}
	}
}
