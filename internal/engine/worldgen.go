package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kullhem-io/Crossplay/internal/agents"
	"github.com/Kullhem-io/Crossplay/internal/schema"
)

// Start seeds a new game from a human-supplied world topic: Qwen generates a
// structured world (grammar-constrained), the engine builds the canonical
// ledger from it and broadcasts it, then Qwen streams the opening scene.
func (g *Game) Start(ctx context.Context, topic string) error {
	g.logf("weaving a world from: " + topic)
	g.seat(SeatNarrator, BrainQwen, "thinking")

	result, err := g.worldgen(ctx, topic)
	if err != nil {
		g.seat(SeatNarrator, BrainQwen, "idle")
		g.errf(fmt.Errorf("worldgen: %w", err))
		return err
	}

	state := buildState(topic, result)
	g.mu.Lock()
	g.state = state
	g.mu.Unlock()
	g.broadcastState()

	// Opening scene, grounded in the freshly generated world.
	if err := g.openingScene(ctx); err != nil {
		g.errf(fmt.Errorf("opening scene: %w", err))
	}
	g.seat(SeatNarrator, BrainQwen, "idle")

	// Hand off to the autonomous play loop (runs on its own long-lived context,
	// not the short-lived setup ctx).
	g.startLoop()
	return nil
}

func (g *Game) worldgen(ctx context.Context, topic string) (*schema.WorldgenResult, error) {
	msgs := []agents.Message{
		{Role: "system", Content: "You design the opening of an interactive story. Given a world topic, invent a vivid starting location, one fitting main character, and 1 to 3 adversaries or hazards that genuinely belong in that place (a person, an animal, a machine, an environmental danger, whatever suits it). " + genreRule + " Keep HP values in the 8 to 30 range. Output only the requested JSON."},
		{Role: "user", Content: "World topic: " + topic},
	}
	raw, err := g.sched.Complete(ctx, BrainQwen, msgs, agents.CallOpts{
		Temperature: 0.8,
		Priority:    10,
		JSONSchema:  schema.WorldgenSchema,
	})
	if err != nil {
		return nil, err
	}
	var out schema.WorldgenResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &out); err != nil {
		return nil, fmt.Errorf("parse worldgen json: %w (raw: %.200s)", err, raw)
	}
	return &out, nil
}

// buildState turns a validated worldgen result into the canonical ledger,
// filling engine-owned fields (ids, alive flags, hp=maxHp).
func buildState(topic string, r *schema.WorldgenResult) *schema.GameState {
	clampHP := func(n int) int {
		if n < 1 {
			return 10
		}
		if n > 200 {
			return 200
		}
		return n
	}

	player := schema.Entity{
		ID:        SeatPlayer1,
		Name:      orDefault(r.Player.Name, "Adventurer"),
		Kind:      schema.KindPlayer,
		Class:     orDefault(r.Player.Class, "Adventurer"),
		Level:     1,
		XP:        0,
		MaxHP:     clampHP(r.Player.MaxHP),
		Alive:     true,
		Status:    []string{},
		Inventory: r.Player.Inventory,
		Desc:      r.Player.Desc,
	}
	player.HP = player.MaxHP
	if player.Inventory == nil {
		player.Inventory = []schema.Item{}
	}

	entities := []schema.Entity{player}
	for i, m := range r.Monsters {
		e := schema.Entity{
			ID:        fmt.Sprintf("monster-%d", i+1),
			Name:      orDefault(m.Name, fmt.Sprintf("Creature %d", i+1)),
			Kind:      schema.KindMonster,
			Level:     1,
			MaxHP:     clampHP(m.MaxHP),
			Alive:     true,
			Status:    []string{},
			Inventory: []schema.Item{},
			Desc:      m.Desc,
		}
		e.HP = e.MaxHP
		entities = append(entities, e)
	}

	return &schema.GameState{
		Topic:    topic,
		Phase:    schema.PhasePlaying,
		Round:    1,
		Location: r.Location,
		Entities: entities,
		Log:      []string{"The adventure begins."},
	}
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
