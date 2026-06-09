// Command crossplay is the Crossplay v2 game server: a Go backend that
// orchestrates parallel LLM "seats" (DM, narrator, players) over a single
// WebSocket, owning the authoritative game ledger.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Kullhem-io/Crossplay/internal/agents"
	"github.com/Kullhem-io/Crossplay/internal/engine"
	"github.com/Kullhem-io/Crossplay/internal/transport"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	port := envOr("PORT", "3001")

	sched := agents.NewScheduler()
	// Qwen: serial (1 in flight). Gemma: two parallel slots.
	sched.Register(agents.NewOpenAIBrain(engine.BrainQwen,
		envOr("QWEN_URL", "http://127.0.0.1:8001/v1"),
		envOr("QWEN_MODEL", "unsloth/Qwen3.6-27B-MTP-GGUF:UD-Q6_K_XL"), 1))
	sched.Register(agents.NewOpenAIBrain(engine.BrainGemma,
		envOr("GEMMA_URL", "http://127.0.0.1:8004/v1"),
		envOr("GEMMA_MODEL", "unsloth/gemma-4-12B-it-qat-GGUF"), 3))

	hub := transport.NewHub()

	// The human brain announces an awaited seat over the WebSocket and blocks
	// until the browser sends that seat's action back.
	human := agents.NewHumanBrain(engine.BrainHuman, func(seat string) {
		hub.Broadcast(transport.Event{Type: transport.EvAwaitInput,
			Payload: map[string]any{"seat": seat}})
	})
	sched.Register(human)

	game := engine.New(sched, hub.Broadcast)

	hub.OnMessage(func(msg transport.Inbound) {
		switch msg.Type {
		case transport.MsgStart:
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
				defer cancel()
				_ = game.Start(ctx, msg.Payload.Topic)
			}()
		case transport.MsgVoid:
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				game.Void(ctx, msg.Payload.Text)
			}()
		case transport.MsgJoin:
			go game.Join(msg.Payload.Name, msg.Payload.Class, msg.Payload.Desc)
		case transport.MsgPlayerInput:
			human.Deliver(msg.Payload.Seat, msg.Payload.Text)
		case transport.MsgLeave:
			game.Leave(msg.Payload.Seat)
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
	if _, err := os.Stat("web/dist"); err == nil {
		mux.Handle("/", http.FileServer(http.Dir("web/dist")))
	}

	srv := &http.Server{Addr: ":" + port, Handler: mux}
	log.Printf("crossplay listening on http://localhost:%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
