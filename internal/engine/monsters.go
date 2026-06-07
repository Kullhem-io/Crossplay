package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Kullhem-io/Crossplay/internal/agents"
	"github.com/Kullhem-io/Crossplay/internal/schema"
	"github.com/Kullhem-io/Crossplay/internal/transport"
)

type monsterResult struct {
	name string
	id   string
	adj  *schema.Adjudication
}

// adjudicateMonsters has the DM decide every living monster's action — each an
// independent focused call running concurrently across the two Gemma slots
// (scheduler-capped at 2). Pure: it only queries models and returns results, so
// it can run in parallel with Qwen narration of the player's beat. Returns nil
// if there are no living monsters.
func (g *Game) adjudicateMonsters(ctx context.Context) []*monsterResult {
	living := g.Snapshot().LivingMonsters()
	if len(living) == 0 {
		return nil
	}
	results := make([]*monsterResult, len(living))
	g.seat(SeatDM, BrainGemma, "thinking")
	var wg sync.WaitGroup
	for i, mp := range living {
		i, m := i, *mp
		wg.Add(1)
		go func() {
			defer wg.Done()
			roll := g.d20()
			adj, err := g.monsterTurn(ctx, m, roll)
			if err != nil {
				return // one monster failing shouldn't abort the round
			}
			results[i] = &monsterResult{name: m.Name, id: m.ID, adj: adj}
		}()
	}
	wg.Wait()
	g.seat(SeatDM, BrainGemma, "idle")
	return results
}

// resolveMonsters applies the (already-computed) monster rulings to the ledger
// and narrates the enemy turn as one streamed beat.
func (g *Game) resolveMonsters(ctx context.Context, results []*monsterResult) {
	var outcomes []string
	for _, r := range results {
		if r == nil {
			continue
		}
		g.emit(transport.Event{Type: transport.EvAction,
			Payload: transport.Action{Seat: r.id, Name: r.name, Text: r.adj.Outcome}})
		g.applyAdjudication(r.adj)
		outcomes = append(outcomes, r.name+": "+r.adj.Outcome)
	}
	if len(outcomes) == 0 {
		return
	}
	g.broadcastState()
	g.remember(strings.Join(outcomes, " "))

	if err := g.narrateMonsters(ctx, outcomes); err != nil && ctx.Err() == nil {
		g.errf(fmt.Errorf("narrate monsters: %w", err))
	}
}

// monsterTurn has the DM decide one monster's action + result, given a roll.
func (g *Game) monsterTurn(ctx context.Context, m schema.Entity, roll int) (*schema.Adjudication, error) {
	st := g.Snapshot()
	sys := "You are the Game System controlling a single monster in a text RPG. Decide what THIS monster does on its turn and the mechanical result, as JSON deltas. " +
		"A d20 has been rolled for it: 1 is a critical failure, 10–11 average, 20 a critical success. The monster acts according to its nature, usually against the player. " +
		"Target entities by id. Keep damage proportional (typically 2–10). Output only the JSON."
	user := fmt.Sprintf("%s\nIt is the turn of the monster: %s (id %s).\nIts action roll (d20): %d\nDecide its action and the outcome.",
		sceneBrief(st), m.Name, m.ID, roll)

	raw, err := g.sched.Complete(ctx, BrainGemma, []agents.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: user},
	}, agents.CallOpts{
		Temperature: 0.3, Priority: 40, MaxTokens: 400, JSONSchema: schema.AdjudicationSchema,
	})
	if err != nil {
		return nil, err
	}
	var adj schema.Adjudication
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &adj); err != nil {
		return nil, fmt.Errorf("parse monster adjudication: %w (raw: %.160s)", err, raw)
	}
	return &adj, nil
}

// narrateMonsters dramatizes the whole enemy turn in one streamed beat.
func (g *Game) narrateMonsters(ctx context.Context, outcomes []string) error {
	st := g.Snapshot()
	g.seat(SeatNarrator, BrainQwen, "thinking")
	defer g.seat(SeatNarrator, BrainQwen, "idle")

	sys := "You are the Narrator of a text RPG. Dramatize the enemies' turn in vivid prose — second person toward the player, present tense, 1–2 short paragraphs. Stay consistent with the stated outcomes; invent no extra damage, deaths, or items, and do not break character."
	user := fmt.Sprintf("%s\nOn the enemies' turn:\n- %s\nNarrate this.", sceneBrief(st), strings.Join(outcomes, "\n- "))

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
