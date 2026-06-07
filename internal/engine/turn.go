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
			return // the ending (if any) was already narrated by runRound
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

	// 0. Pull any Voice-from-the-Void utterances; they ride along as in-world
	// context for this round (never as a delta, see Void()).
	voidCtx := voidContext(g.drainVoid())
	if voidCtx != "" {
		g.remember("A disembodied voice echoed through the world.")
	}

	// 1. Player declares an action.
	intent, err := g.playerTurn(rctx, voidCtx)
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
	adj, err := g.adjudicate(rctx, intent, roll, voidCtx)
	if err != nil {
		return fmt.Errorf("adjudicate: %w", err)
	}

	// 4. Apply validated deltas, broadcast the new ledger + the mechanics.
	changes := g.applyAdjudication(adj)
	g.bumpRound()
	g.broadcastState()
	g.emit(transport.Event{Type: transport.EvMechanics,
		Payload: transport.Mechanics{Seat: SeatPlayer1, Name: pname, Roll: roll, Changes: changes}})
	g.remember(pname + " " + adj.Outcome)

	// 5 + 6 pipelined: the narrator (Qwen) dramatizes the player's beat while the
	// DM (Gemma) concurrently adjudicates the monsters, different brains, so both
	// lanes light up at once and the round is shorter. Then resolve the monsters.
	stillPlaying := func() bool {
		s := g.Snapshot()
		return s != nil && s.Phase == schema.PhasePlaying
	}

	var monsters []*monsterResult
	monstersDone := make(chan struct{})
	go func() {
		defer close(monstersDone)
		if stillPlaying() {
			monsters = g.adjudicateMonsters(rctx)
		}
	}()

	if err := g.narrateBeat(rctx, pname, intent, adj, voidCtx); err != nil && rctx.Err() == nil {
		g.errf(fmt.Errorf("narrate: %w", err))
	}

	<-monstersDone
	if len(monsters) > 0 && stillPlaying() {
		g.resolveMonsters(rctx, monsters)
	}

	// 7. Did this round end the game (player fell, or last foe defeated)? If so,
	// give it a real closing beat from the narrator.
	if g.finalizeIfEnded() {
		g.broadcastState()
		g.narrateEnding(rctx)
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
func (g *Game) playerTurn(ctx context.Context, voidCtx string) (string, error) {
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
		{Role: "system", Content: fmt.Sprintf("You are %s, %s. You are the protagonist of an interactive story. Decide your character's next action, in character and true to the setting. Be decisive and specific, and commit to finishing a threat rather than circling it. If your recent attempts have stalled or repeated, change tactics. State ONLY what you attempt, never narrate the outcome. One or two sentences. %s", name, persona, genreRule)},
		{Role: "user", Content: joinNonEmpty("\n", sceneBrief(st), g.recentContext(), voidCtx, "What do you do?")},
	}
	intent, err := g.sched.Complete(ctx, BrainGemma, msgs, agents.CallOpts{
		Temperature: 1.0, Priority: 50, MaxTokens: 160,
	})
	return strings.TrimSpace(intent), err
}

// adjudicate has the DM seat (Gemma, low temp) turn the attempt + dice roll into
// validated mechanical deltas. The engine already rolled; the DM only interprets.
func (g *Game) adjudicate(ctx context.Context, intent string, roll int, voidCtx string) (*schema.Adjudication, error) {
	st := g.Snapshot()

	g.seat(SeatDM, BrainGemma, "thinking")
	defer g.seat(SeatDM, BrainGemma, "idle")

	sys := "You are the Game System (referee) for an interactive story. You decide the mechanical outcome of the player's attempt and output it as JSON deltas. " + genreRule + " " +
		"A d20 has already been rolled for the attempt: 1 is a critical failure, 10 to 11 is average, 20 is a critical success. Interpret the roll in context, a high roll succeeds well, a low roll fails or backfires. " +
		"Target entities by their id. Keep damage proportional (typically 2 to 10). You may add status effects or grant/consume items freely, but only to the listed entities. " +
		"A foe leaves the fight only when its HP reaches 0: to finish or remove an enemy, deal damage that brings it to 0, do not merely tag it as defeated. Make decisive progress; do not let the same standoff repeat. " +
		"Award the player XP (xp delta, typically 3 to 10, more for defeating a foe) when they make meaningful progress. Be deterministic and concise. Output only the JSON."

	user := joinNonEmpty("\n",
		sceneBrief(st),
		fmt.Sprintf("The player (%s) attempts: %s", playerName(st), intent),
		fmt.Sprintf("Action roll (d20): %d", roll),
		voidCtx,
		"Decide the outcome.")

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
func (g *Game) narrateBeat(ctx context.Context, pname, intent string, adj *schema.Adjudication, voidCtx string) error {
	st := g.Snapshot()
	sys := "You are the Narrator of a text RPG. Dramatize the resolved beat in vivid prose, second person toward the player, present tense, 1 to 2 short paragraphs. Stay consistent with the facts given; do not invent damage, deaths, or items beyond what's stated, and do not break character. If a disembodied voice is mentioned, weave it in as an eerie phenomenon without acknowledging its source."
	user := joinNonEmpty("\n",
		sceneBrief(st),
		fmt.Sprintf("%s attempted: %s", pname, intent),
		"Resolved: "+adj.Outcome,
		adj.Narration,
		voidCtx,
		"Narrate this beat.")
	return g.streamNarration(ctx, []agents.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: user},
	})
}

func playerName(st *schema.GameState) string {
	if p := st.Player(); p != nil {
		return p.Name
	}
	return "the player"
}

// joinNonEmpty joins only the non-blank parts, so optional context (like the
// void) cleanly drops out when absent.
func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}
