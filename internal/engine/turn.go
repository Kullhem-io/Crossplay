package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Kullhem-io/Crossplay/internal/agents"
	"github.com/Kullhem-io/Crossplay/internal/schema"
	"github.com/Kullhem-io/Crossplay/internal/transport"
)

// startLoop (re)starts the autonomous play loop. Any prior loop is cancelled.
func (g *Game) startLoop() {
	g.mu.Lock()
	if g.loopCancel != nil {
		g.loopCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	g.loopCancel = cancel
	g.mu.Unlock()
	go g.playLoop(ctx)
}

// playLoop runs rounds until the game ends or the loop is cancelled.
func (g *Game) playLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		st := g.Snapshot()
		if st == nil || st.Phase != schema.PhasePlaying {
			if st != nil && st.Phase == schema.PhaseGameOver {
				g.logf("— game over —")
			}
			return
		}
		if err := g.runRound(ctx); err != nil {
			if ctx.Err() == nil {
				g.errf(err)
			}
			return
		}
		select {
		case <-time.After(1500 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
}

// runRound is one player beat: intent -> roll -> DM ruling -> apply -> narrate.
// (Monsters act in M4.)
func (g *Game) runRound(ctx context.Context) error {
	rctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()

	// 1. Player declares an action.
	intent, err := g.playerTurn(rctx)
	if err != nil {
		return fmt.Errorf("player turn: %w", err)
	}
	st := g.Snapshot()
	pname := "Player"
	if p := st.Player(); p != nil {
		pname = p.Name
	}
	g.emit(transport.Event{Type: transport.EvAction,
		Payload: transport.Action{Seat: SeatPlayer1, Name: pname, Text: intent}})

	// 2. Engine rolls the dice (fair, seeded).
	roll := g.d20()

	// 3. DM adjudicates the attempt given the roll.
	adj, err := g.adjudicate(rctx, intent, roll)
	if err != nil {
		return fmt.Errorf("adjudicate: %w", err)
	}

	// 4. Apply validated deltas, broadcast the new ledger immediately.
	g.applyAdjudication(adj)
	g.bumpRound()
	g.broadcastState()
	g.remember(pname + " " + adj.Outcome)

	// 5. Narrator dramatizes the resolved beat.
	if err := g.narrateBeat(rctx, pname, intent, adj); err != nil && rctx.Err() == nil {
		g.errf(fmt.Errorf("narrate: %w", err))
	}

	// 6. Enemies' turn — DM drives living monsters (skipped if the player just died).
	if s := g.Snapshot(); s != nil && s.Phase == schema.PhasePlaying {
		g.monsterPhase(rctx)
	}
	return nil
}

func (g *Game) bumpRound() {
	g.mu.Lock()
	if g.state != nil {
		g.state.Round++
	}
	g.mu.Unlock()
}

// playerTurn asks the player seat (Gemma, high temp) for an in-character action.
func (g *Game) playerTurn(ctx context.Context) (string, error) {
	st := g.Snapshot()
	p := st.Player()
	persona := "an adventurer"
	if p != nil && p.Desc != "" {
		persona = p.Desc
	}
	name := "the adventurer"
	if p != nil {
		name = p.Name
	}

	g.seat(SeatPlayer1, BrainGemma, "thinking")
	defer g.seat(SeatPlayer1, BrainGemma, "idle")

	msgs := []agents.Message{
		{Role: "system", Content: fmt.Sprintf("You are %s — %s. You are playing a text RPG dungeon crawl. Decide your character's next action, in character. Be decisive and specific. State ONLY what you attempt — never narrate the outcome. One or two sentences.", name, persona)},
		{Role: "user", Content: sceneBrief(st) + "\n" + g.recentContext() + "\nWhat do you do?"},
	}
	intent, err := g.sched.Complete(ctx, BrainGemma, msgs, agents.CallOpts{
		Temperature: 1.0, Priority: 50, MaxTokens: 160,
	})
	return strings.TrimSpace(intent), err
}

// adjudicate has the DM seat (Gemma, low temp) turn the attempt + dice roll into
// validated mechanical deltas. The engine already rolled; the DM only interprets.
func (g *Game) adjudicate(ctx context.Context, intent string, roll int) (*schema.Adjudication, error) {
	st := g.Snapshot()

	g.seat(SeatDM, BrainGemma, "thinking")
	defer g.seat(SeatDM, BrainGemma, "idle")

	sys := "You are the Game System (referee) for a text RPG. You decide the mechanical outcome of the player's attempt and output it as JSON deltas. " +
		"A d20 has already been rolled for the attempt: 1 is a critical failure, 10–11 is average, 20 is a critical success. Interpret the roll in context — a high roll succeeds well, a low roll fails or backfires. " +
		"Target entities by their id. Keep damage proportional (typically 2–10). You may add status effects or grant/consume items freely, but only to the listed entities. Be deterministic and concise. Output only the JSON."

	user := fmt.Sprintf("%s\nThe player (%s) attempts: %s\nAction roll (d20): %d\nDecide the outcome.",
		sceneBrief(st), playerName(st), intent, roll)

	raw, err := g.sched.Complete(ctx, BrainGemma, []agents.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: user},
	}, agents.CallOpts{
		Temperature: 0.2, Priority: 40, MaxTokens: 500, JSONSchema: schema.AdjudicationSchema,
	})
	if err != nil {
		return nil, err
	}
	var adj schema.Adjudication
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &adj); err != nil {
		return nil, fmt.Errorf("parse adjudication: %w (raw: %.200s)", err, raw)
	}
	return &adj, nil
}

// narrateBeat streams Qwen's prose for an already-resolved beat.
func (g *Game) narrateBeat(ctx context.Context, pname, intent string, adj *schema.Adjudication) error {
	st := g.Snapshot()

	g.seat(SeatNarrator, BrainQwen, "thinking")
	defer g.seat(SeatNarrator, BrainQwen, "idle")

	sys := "You are the Narrator of a text RPG. Dramatize the resolved beat in vivid prose — second person toward the player, present tense, 1–2 short paragraphs. Stay consistent with the facts given; do not invent damage, deaths, or items beyond what's stated, and do not break character."
	user := fmt.Sprintf("%s\n%s attempted: %s\nResolved: %s\n%s\nNarrate this beat.",
		sceneBrief(st), pname, intent, adj.Outcome, adj.Narration)

	ch, err := g.sched.Stream(ctx, BrainQwen, []agents.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: user},
	}, agents.CallOpts{Temperature: 1.0, Priority: 10})
	if err != nil {
		return err
	}
	first := true
	for t := range ch {
		if t.Err != nil {
			return t.Err
		}
		if first {
			g.seat(SeatNarrator, BrainQwen, "streaming")
			first = false
		}
		g.narrate(t.Text)
	}
	return nil
}

func playerName(st *schema.GameState) string {
	if p := st.Player(); p != nil {
		return p.Name
	}
	return "the player"
}
