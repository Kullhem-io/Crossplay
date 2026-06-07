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

// monsterPhase resolves every living monster's action for the round. The DM
// seat decides each one as an independent focused call; with the player idle,
// these run concurrently across the two Gemma slots (the scheduler caps it at
// 2 in flight). Results are applied sequentially to avoid racing the ledger,
// then narrated as a single beat (Qwen is serial).
func (g *Game) monsterPhase(ctx context.Context) {
	living := g.Snapshot().LivingMonsters()
	if len(living) == 0 {
		return
	}

	type res struct {
		name string
		id   string
		adj  *schema.Adjudication
	}
	results := make([]*res, len(living))

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
				return // a single monster failing shouldn't abort the round
			}
			results[i] = &res{name: m.Name, id: m.ID, adj: adj}
		}()
	}
	wg.Wait()
	g.seat(SeatDM, BrainGemma, "idle")

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
