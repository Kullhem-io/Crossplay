package engine

import (
	"testing"

	"github.com/Kullhem-io/Crossplay/internal/schema"
	"github.com/Kullhem-io/Crossplay/internal/transport"
)

// newTestGame builds a Game with no scheduler (these tests never call models)
// and a no-op emitter, seeded with the given entities.
func newTestGame(entities ...schema.Entity) *Game {
	g := New(nil, func(transport.Event) {})
	g.state = &schema.GameState{Phase: schema.PhasePlaying, Round: 1, Entities: entities}
	return g
}

func player(hp, max int) schema.Entity {
	return schema.Entity{ID: SeatPlayer1, Name: "Hero", Kind: schema.KindPlayer, Level: 1, HP: hp, MaxHP: max, Alive: true}
}

func monster(id, name string, hp, max int) schema.Entity {
	return schema.Entity{ID: id, Name: name, Kind: schema.KindMonster, Level: 1, HP: hp, MaxHP: max, Alive: true}
}

func TestDamageClampsAndKills(t *testing.T) {
	g := newTestGame(player(20, 20), monster("monster-1", "Goblin", 5, 12))
	g.applyAdjudication(&schema.Adjudication{Deltas: []schema.Delta{
		{Type: schema.DeltaDamage, Target: "monster-1", Amount: 99},
	}})
	m := g.state.FindEntity("monster-1")
	if m.HP != 0 {
		t.Fatalf("expected HP clamped to 0, got %d", m.HP)
	}
	if m.Alive {
		t.Fatal("expected monster dead after lethal damage")
	}
}

func TestHealCapsAtMax(t *testing.T) {
	g := newTestGame(player(8, 20))
	g.applyAdjudication(&schema.Adjudication{Deltas: []schema.Delta{
		{Type: schema.DeltaHeal, Target: SeatPlayer1, Amount: 100},
	}})
	if hp := g.state.Player().HP; hp != 20 {
		t.Fatalf("expected heal capped at maxHP 20, got %d", hp)
	}
}

func TestXPTriggersLevelUp(t *testing.T) {
	g := newTestGame(player(20, 20)) // level 1 needs 10 XP
	g.applyAdjudication(&schema.Adjudication{Deltas: []schema.Delta{
		{Type: schema.DeltaXP, Target: SeatPlayer1, Amount: 12},
	}})
	p := g.state.Player()
	if p.Level != 2 {
		t.Fatalf("expected level 2, got %d", p.Level)
	}
	if p.XP != 2 { // 12 - 10 carried over
		t.Fatalf("expected 2 carryover XP, got %d", p.XP)
	}
	if p.MaxHP != 24 { // +4 on level up
		t.Fatalf("expected maxHP 24 after level up, got %d", p.MaxHP)
	}
}

func TestFinalizeDefeatOnPlayerDeath(t *testing.T) {
	g := newTestGame(player(3, 20), monster("monster-1", "Goblin", 10, 10))
	g.applyAdjudication(&schema.Adjudication{Deltas: []schema.Delta{
		{Type: schema.DeltaDamage, Target: SeatPlayer1, Amount: 5},
	}})
	if !g.finalizeIfEnded() {
		t.Fatal("expected game to end on player death")
	}
	if g.state.Outcome != schema.OutcomeDefeat {
		t.Fatalf("expected defeat, got %q", g.state.Outcome)
	}
	// A second call must not re-fire (so the ending narrates once).
	if g.finalizeIfEnded() {
		t.Fatal("finalize should only fire on the ending round")
	}
}

func TestFinalizeVictoryWhenAllMonstersDead(t *testing.T) {
	g := newTestGame(player(20, 20), monster("monster-1", "Goblin", 4, 12))
	g.applyAdjudication(&schema.Adjudication{Deltas: []schema.Delta{
		{Type: schema.DeltaDamage, Target: "monster-1", Amount: 10},
	}})
	if !g.finalizeIfEnded() {
		t.Fatal("expected victory when the last monster falls")
	}
	if g.state.Outcome != schema.OutcomeVictory {
		t.Fatalf("expected victory, got %q", g.state.Outcome)
	}
}

func TestItemCountsStayNonNegative(t *testing.T) {
	p := player(20, 20)
	p.Inventory = []schema.Item{{Name: "Potion", Qty: 1}}
	g := newTestGame(p)
	g.applyAdjudication(&schema.Adjudication{Deltas: []schema.Delta{
		{Type: schema.DeltaItemRemove, Target: SeatPlayer1, Item: "Potion", Qty: 5},
	}})
	if len(g.state.Player().Inventory) != 0 {
		t.Fatalf("expected item removed entirely, got %+v", g.state.Player().Inventory)
	}
}
