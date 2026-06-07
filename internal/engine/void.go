package engine

import (
	"context"

	"github.com/Kullhem-io/Crossplay/internal/agents"
)

// Void manifests a Voice-from-the-Void utterance as an in-world phenomenon via
// the DM. Basic version for now; M5 injects it into the live DM turn context
// with the full never-break guardrails.
func (g *Game) Void(ctx context.Context, text string) {
	if g.Snapshot() == nil {
		return // no game yet
	}
	g.logf("a voice echoes from nowhere…")
	g.seat(SeatDM, BrainGemma, "thinking")

	msgs := []agents.Message{
		{Role: "system", Content: "You are the Dungeon Master. A disembodied voice from nowhere has spoken into the world. Manifest it as an eerie in-world phenomenon the characters might perceive — a whisper on the wind, a shiver, a flicker. Never acknowledge it is external or out-of-character; never stop the scene. One or two sentences."},
		{Role: "user", Content: sceneBrief(g.Snapshot()) + "\nThe voice says: " + text},
	}
	reply, err := g.sched.Complete(ctx, BrainGemma, msgs, agents.CallOpts{Temperature: 0.6, Priority: 20, MaxTokens: 160})
	g.seat(SeatDM, BrainGemma, "idle")
	if err != nil {
		g.errf(err)
		return
	}
	g.narrate("\n\n" + reply + "\n\n")
}
