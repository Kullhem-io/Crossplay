// Command crossplay is the Crossplay v2 game server: a Go backend that
// orchestrates parallel LLM "seats" (DM, narrator, players) over a single
// WebSocket, owning the authoritative game ledger.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Kullhem-io/Crossplay/internal/transport"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "7777"
	}

	hub := transport.NewHub()

	// M0 stub: echo inbound client messages back as agent-lane activity so the
	// full UI<->server round-trip is observable. The real engine replaces this.
	hub.OnMessage(func(msg transport.Inbound) {
		switch msg.Type {
		case transport.MsgStart:
			log.Printf("start: topic=%q", msg.Payload.Topic)
			hub.Broadcast(transport.Event{Type: transport.EvLog,
				Payload: map[string]any{"line": "world topic received: " + msg.Payload.Topic}})
			demoSeatBlip(hub, "narrator", "qwen-local")
		case transport.MsgVoid:
			log.Printf("void: %q", msg.Payload.Text)
			hub.Broadcast(transport.Event{Type: transport.EvLog,
				Payload: map[string]any{"line": "a voice echoes from nowhere…"}})
			demoSeatBlip(hub, "dm", "gemma-local")
		default:
			log.Printf("unknown inbound type: %q", msg.Type)
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/ws", hub.ServeWS)

	// Serve the built SPA if present (web/dist). In dev the Vite server runs
	// separately and proxies /ws + /healthz here.
	if _, err := os.Stat("web/dist"); err == nil {
		mux.Handle("/", http.FileServer(http.Dir("web/dist")))
	}

	srv := &http.Server{
		Addr:        ":" + port,
		Handler:     mux,
		ReadTimeout: 0, // WebSocket connections are long-lived
	}
	log.Printf("crossplay listening on http://localhost:%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

// demoSeatBlip flips a seat thinking->idle so the lanes light up in M0.
func demoSeatBlip(hub *transport.Hub, seat, brain string) {
	hub.Broadcast(transport.Event{Type: transport.EvAgentStatus,
		Payload: transport.AgentStatus{Seat: seat, Brain: brain, Status: transport.StatusThinking}})
	go func() {
		time.Sleep(1200 * time.Millisecond)
		hub.Broadcast(transport.Event{Type: transport.EvAgentStatus,
			Payload: transport.AgentStatus{Seat: seat, Brain: brain, Status: transport.StatusIdle}})
	}()
}
