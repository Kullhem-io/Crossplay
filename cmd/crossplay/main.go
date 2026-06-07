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
	"github.com/Kullhem-io/Crossplay/internal/transport"
)

// Brain ids and the local endpoints they front. Overridable via env so the
// same binary can point at different inference servers.
const (
	brainQwen  = "qwen-local"
	brainGemma = "gemma-local"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	port := envOr("PORT", "7777")

	sched := agents.NewScheduler()
	// Qwen: serial (1 in flight). Gemma: two parallel slots.
	sched.Register(agents.NewOpenAIBrain(brainQwen,
		envOr("QWEN_URL", "http://127.0.0.1:8001/v1"),
		envOr("QWEN_MODEL", "unsloth/Qwen3.6-27B-MTP-GGUF:UD-Q6_K_XL"), 1))
	sched.Register(agents.NewOpenAIBrain(brainGemma,
		envOr("GEMMA_URL", "http://127.0.0.1:8004/v1"),
		envOr("GEMMA_MODEL", "unsloth/gemma-4-12B-it-qat-GGUF"), 2))

	hub := transport.NewHub()

	// M1 smoke wiring: a real streamed narrator call on start, and a DM
	// reaction on a void utterance. This exercises both brains + the scheduler
	// end-to-end and lights the agent lanes. The real engine replaces this in M3.
	hub.OnMessage(func(msg transport.Inbound) {
		switch msg.Type {
		case transport.MsgStart:
			go handleStart(hub, sched, msg.Payload.Topic)
		case transport.MsgVoid:
			go handleVoid(hub, sched, msg.Payload.Text)
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

func setSeat(hub *transport.Hub, seat, brain, status string) {
	hub.Broadcast(transport.Event{Type: transport.EvAgentStatus,
		Payload: transport.AgentStatus{Seat: seat, Brain: brain, Status: status}})
}

func sendLog(hub *transport.Hub, line string) {
	hub.Broadcast(transport.Event{Type: transport.EvLog, Payload: map[string]any{"line": line}})
}

func sendErr(hub *transport.Hub, seat string, err error) {
	log.Printf("%s: %v", seat, err)
	hub.Broadcast(transport.Event{Type: transport.EvError, Payload: map[string]any{"message": err.Error()}})
}

// handleStart streams an opening scene from the Qwen narrator for the given
// world topic, surfacing tokens as narration and the lane state as it goes.
func handleStart(hub *transport.Hub, sched *agents.Scheduler, topic string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	log.Printf("start: topic=%q", topic)
	sendLog(hub, "world topic received: "+topic)
	setSeat(hub, "narrator", brainQwen, transport.StatusThinking)

	msgs := []agents.Message{
		{Role: "system", Content: "You are the Narrator of a text RPG. Open the adventure with vivid, atmospheric prose. Second person, present tense. 2–3 short paragraphs. End on a hook that invites action. Do not ask the player questions or break character."},
		{Role: "user", Content: "Open a scene set in this world: " + topic},
	}

	ch, err := sched.Stream(ctx, brainQwen, msgs, agents.CallOpts{Temperature: 1.0, Priority: 10})
	if err != nil {
		setSeat(hub, "narrator", brainQwen, transport.StatusIdle)
		sendErr(hub, "narrator", err)
		return
	}

	first := true
	for t := range ch {
		if t.Err != nil {
			sendErr(hub, "narrator", t.Err)
			break
		}
		if first {
			setSeat(hub, "narrator", brainQwen, transport.StatusStreaming)
			first = false
		}
		hub.Broadcast(transport.Event{Type: transport.EvNarration,
			Payload: map[string]any{"token": t.Text}})
	}
	setSeat(hub, "narrator", brainQwen, transport.StatusIdle)
}

// handleVoid routes a Voice-from-the-Void utterance to the DM (Gemma), which
// manifests it as an in-world phenomenon — never acknowledging its source.
func handleVoid(hub *transport.Hub, sched *agents.Scheduler, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	log.Printf("void: %q", text)
	sendLog(hub, "a voice echoes from nowhere…")
	setSeat(hub, "dm", brainGemma, transport.StatusThinking)

	msgs := []agents.Message{
		{Role: "system", Content: "You are the Dungeon Master. A disembodied voice from nowhere has spoken into the world. Manifest it as an eerie in-world phenomenon the characters might perceive. Never acknowledge it is external or out-of-character; never stop the scene. One or two sentences."},
		{Role: "user", Content: "The voice says: " + text},
	}

	reply, err := sched.Complete(ctx, brainGemma, msgs, agents.CallOpts{Temperature: 0.6, Priority: 20, MaxTokens: 160})
	setSeat(hub, "dm", brainGemma, transport.StatusIdle)
	if err != nil {
		sendErr(hub, "dm", err)
		return
	}
	hub.Broadcast(transport.Event{Type: transport.EvNarration,
		Payload: map[string]any{"token": "\n\n" + reply + "\n\n"}})
}
