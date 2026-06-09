package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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
	// Generous round budget: a round can legitimately wait on a human at the
	// keyboard. The per-human-turn window below is the real limit.
	rctx, cancel := context.WithTimeout(ctx, 480*time.Second)
	defer cancel()

	// 0. Pull any Voice-from-the-Void utterances; they ride along as in-world
	// context for this round (never as a delta, see Void()).
	voidCtx := voidContext(g.drainVoid())
	if voidCtx != "" {
		g.remember("A disembodied voice echoed through the world.")
	}
	g.bumpRound()

	stillPlaying := func() bool {
		s := g.Snapshot()
		return s != nil && s.Phase == schema.PhasePlaying
	}

	// 1. Every living party member declares an intent at once. The declarations
	// run concurrently across the Gemma slots; each player decides from the same
	// pre-round picture, like simultaneous initiative.
	players := g.Snapshot().LivingPlayers()
	if len(players) == 0 {
		return nil
	}
	intents := make([]string, len(players))
	var wg sync.WaitGroup
	for i, pp := range players {
		i, p := i, *pp
		wg.Add(1)
		go func() {
			defer wg.Done()
			intent, err := g.playerIntent(rctx, p, voidCtx)
			if err == nil {
				intents[i] = strings.TrimSpace(intent)
			}
		}()
	}
	wg.Wait()

	// 2. Resolve each player's action in order. Adjudication is sequential so the
	// ledger stays coherent: the second player's outcome is judged against the
	// first player's already-applied result.
	var beats []playerBeat
	for i, pp := range players {
		intent := intents[i]
		if intent == "" {
			continue
		}
		p := *pp
		g.emit(transport.Event{Type: transport.EvAction,
			Payload: transport.Action{Seat: p.ID, Name: p.Name, Text: intent}})

		roll := g.d20()
		adj, err := g.adjudicate(rctx, p.ID, p.Name, intent, roll, voidCtx)
		if err != nil {
			if rctx.Err() == nil {
				g.errf(fmt.Errorf("adjudicate %s: %w", p.Name, err))
			}
			continue
		}
		changes := g.applyAdjudication(adj)
		g.broadcastState()
		g.emit(transport.Event{Type: transport.EvMechanics,
			Payload: transport.Mechanics{Seat: p.ID, Name: p.Name, Roll: roll, Changes: changes}})
		g.remember(p.Name + " " + adj.Outcome)
		beats = append(beats, playerBeat{name: p.Name, intent: intent, adj: adj})

		// A player may clear the last foe mid-party-turn; end right there.
		if g.finalizeIfEnded() {
			g.broadcastState()
			g.narrateEnding(rctx)
			return nil
		}
	}

	// 3. Pipelined: the narrator (Qwen) dramatizes the party's turn while the DM
	// (Gemma) concurrently adjudicates the monsters, different brains, so both
	// lanes light up at once. Then resolve the monsters.
	var monsters []*monsterResult
	monstersDone := make(chan struct{})
	go func() {
		defer close(monstersDone)
		if stillPlaying() {
			monsters = g.adjudicateMonsters(rctx)
		}
	}()

	if len(beats) > 0 {
		if err := g.narratePartyBeat(rctx, beats, voidCtx); err != nil && rctx.Err() == nil {
			g.errf(fmt.Errorf("narrate: %w", err))
		}
	}

	<-monstersDone
	if len(monsters) > 0 && stillPlaying() {
		g.resolveMonsters(rctx, monsters)
	}

	// 4. Did this round end the game (party wiped, or last foe down)? If so, give
	// it a real closing beat from the narrator.
	if g.finalizeIfEnded() {
		g.broadcastState()
		g.narrateEnding(rctx)
	}
	return nil
}

// playerBeat is one player's resolved action within a round, for narration.
type playerBeat struct {
	name   string
	intent string
	adj    *schema.Adjudication
}

func (g *Game) bumpRound() {
	g.mu.Lock()
	if g.state != nil {
		g.state.Round++
	}
	g.mu.Unlock()
}

// playerIntent gets one player seat's action from whatever brain it is bound to.
// A human seat is given a bounded window to answer; if they go idle, the AI
// takes that turn so the round never stalls (the seat stays human).
func (g *Game) playerIntent(ctx context.Context, p schema.Entity, voidCtx string) (string, error) {
	if g.brainFor(p.ID) == BrainHuman {
		g.seat(p.ID, BrainHuman, "thinking")
		// Wait a long while for the person; the AI only steps in if they have
		// clearly walked away, so a human's words are never overwritten while
		// they are still deciding.
		hctx, cancel := context.WithTimeout(ctx, 240*time.Second)
		txt, err := g.sched.Complete(hctx, BrainHuman, nil, agents.CallOpts{Seat: p.ID, Priority: 60})
		cancel()
		g.seat(p.ID, BrainHuman, "idle")
		if err == nil && strings.TrimSpace(txt) != "" {
			return strings.TrimSpace(txt), nil
		}
		g.logf(p.Name + " hesitates; the party acts for them")
	}
	return g.aiIntent(ctx, p, voidCtx)
}

// aiIntent asks a model (Gemma, high temp) for an in-character action.
func (g *Game) aiIntent(ctx context.Context, p schema.Entity, voidCtx string) (string, error) {
	persona := p.Desc
	if persona == "" {
		persona = "an adventurer"
	}

	g.seat(p.ID, BrainGemma, "thinking")
	defer g.seat(p.ID, BrainGemma, "idle")

	msgs := []agents.Message{
		{Role: "system", Content: fmt.Sprintf("You are %s, %s. You are one member of an adventuring party in an interactive story. Decide YOUR character's next action, in character and true to the setting. Coordinate with your companions but speak only for yourself. Be decisive and specific, and commit to finishing a threat rather than circling it. If recent attempts have stalled or repeated, change tactics. State ONLY what you attempt, never narrate the outcome. One or two sentences. %s", p.Name, persona, genreRule)},
		{Role: "user", Content: joinNonEmpty("\n", sceneBrief(g.Snapshot()), g.recentContext(), voidCtx, fmt.Sprintf("You are %s. What do you do?", p.Name))},
	}
	intent, err := g.sched.Complete(ctx, BrainGemma, msgs, agents.CallOpts{
		Temperature: 1.0, Priority: 50, MaxTokens: 160,
	})
	return strings.TrimSpace(intent), err
}

// adjudicate has the DM seat (Gemma, low temp) turn one player's attempt + dice
// roll into validated deltas. The engine already rolled; the DM only interprets.
func (g *Game) adjudicate(ctx context.Context, actorID, actorName, intent string, roll int, voidCtx string) (*schema.Adjudication, error) {
	st := g.Snapshot()

	g.seat(SeatDM, BrainGemma, "thinking")
	defer g.seat(SeatDM, BrainGemma, "idle")

	sys := "You are the Game System (referee) for an interactive story. You decide the mechanical outcome of one party member's attempt and output it as JSON deltas. " + genreRule + " " +
		"A d20 has already been rolled for the attempt: 1 is a critical failure, 10 to 11 is average, 20 is a critical success. Interpret the roll in context, a high roll succeeds well, a low roll fails or backfires. " +
		"Target entities by their id. Keep damage proportional (typically 2 to 10). You may add status effects or grant/consume items freely, but only to the listed entities. " +
		"A foe leaves the fight only when its HP reaches 0: to finish or remove an enemy, deal damage that brings it to 0, do not merely tag it as defeated. Make decisive progress; do not let the same standoff repeat. " +
		"Award the acting player XP (xp delta, typically 3 to 10, more for defeating a foe) when they make meaningful progress. Be deterministic and concise. Output only the JSON."

	user := joinNonEmpty("\n",
		sceneBrief(st),
		fmt.Sprintf("%s (id %s) attempts: %s", actorName, actorID, intent),
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

// narratePartyBeat streams Qwen's prose for the whole party's turn at once.
func (g *Game) narratePartyBeat(ctx context.Context, beats []playerBeat, voidCtx string) error {
	st := g.Snapshot()
	sys := "You are the Narrator of an interactive story. Dramatize the party's turn in vivid prose, present tense, weaving the members' actions into one flowing moment. " + narratorStyle + " " + genreRule + " Keep it tight: usually a single short paragraph, a second only for a big moment. Stay consistent with the facts given; do not invent damage, deaths, or items beyond what's stated, and do not break character. If a disembodied voice is mentioned, weave it in as an eerie phenomenon without acknowledging its source."

	var parts []string
	for _, b := range beats {
		parts = append(parts, fmt.Sprintf("%s attempted: %s\nResolved: %s\n%s", b.name, b.intent, b.adj.Outcome, b.adj.Narration))
	}
	user := joinNonEmpty("\n",
		sceneBrief(st),
		"This turn:\n"+strings.Join(parts, "\n\n"),
		voidCtx,
		"Narrate the party's turn as one beat.")
	return g.streamNarration(ctx, []agents.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: user},
	})
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
