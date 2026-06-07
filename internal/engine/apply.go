package engine

import (
	"fmt"
	"strings"

	"github.com/Kullhem-io/Crossplay/internal/schema"
)

// applyAdjudication applies a DM ruling to the ledger under lock, enforcing
// invariants (HP bounds, non-negative item counts, death). Returns human log
// lines describing what actually changed. Broadcasting is the caller's job.
func (g *Game) applyAdjudication(adj *schema.Adjudication) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state == nil {
		return nil
	}

	var logs []string
	for _, d := range adj.Deltas {
		if line := g.applyDelta(d); line != "" {
			logs = append(logs, line)
		}
	}

	// Leveling is engine-owned (thresholds), like death below.
	logs = append(logs, g.levelUps()...)

	// Death is an engine-owned consequence, never trusted to the model. The
	// game-over transition itself is decided by finalizeIfEnded after the beat.
	for i := range g.state.Entities {
		e := &g.state.Entities[i]
		if e.HP <= 0 && e.Alive {
			e.HP = 0
			e.Alive = false
			logs = append(logs, e.Name+" falls.")
		}
	}

	g.state.Log = append(g.state.Log, logs...)
	return logs
}

// finalizeIfEnded checks win/lose conditions and, on the transition, sets the
// game-over phase and outcome. Returns true only on the round that ends the
// game, so the caller narrates the ending exactly once.
func (g *Game) finalizeIfEnded() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state == nil || g.state.Phase != schema.PhasePlaying {
		return false
	}
	p := g.state.Player()
	switch {
	case p == nil || !p.Alive:
		g.state.Phase = schema.PhaseGameOver
		g.state.Outcome = schema.OutcomeDefeat
	case len(g.state.LivingMonsters()) == 0:
		g.state.Phase = schema.PhaseGameOver
		g.state.Outcome = schema.OutcomeVictory
	default:
		return false
	}
	return true
}

// applyDelta mutates one entity; assumes g.mu held. Returns a log line or "".
func (g *Game) applyDelta(d schema.Delta) string {
	e := g.resolveTarget(d.Target)
	if e == nil {
		return "" // unresolved target: silently drop rather than corrupt the ledger
	}
	switch d.Type {
	case schema.DeltaDamage:
		amt := clampNonNeg(d.Amount)
		before := e.HP
		e.HP = before - amt
		if e.HP < 0 {
			e.HP = 0
		}
		return fmt.Sprintf("%s takes %d damage (%d→%d).", e.Name, amt, before, e.HP)
	case schema.DeltaHeal:
		if !e.Alive {
			return ""
		}
		amt := clampNonNeg(d.Amount)
		before := e.HP
		e.HP = before + amt
		if e.HP > e.MaxHP {
			e.HP = e.MaxHP
		}
		return fmt.Sprintf("%s heals %d (%d→%d).", e.Name, amt, before, e.HP)
	case schema.DeltaStatus:
		s := strings.TrimSpace(d.Status)
		if s == "" {
			return ""
		}
		if d.Remove {
			e.Status = removeStr(e.Status, s)
			return fmt.Sprintf("%s is no longer %s.", e.Name, s)
		}
		if !containsStr(e.Status, s) {
			e.Status = append(e.Status, s)
		}
		return fmt.Sprintf("%s is now %s.", e.Name, s)
	case schema.DeltaItemAdd:
		name := strings.TrimSpace(d.Item)
		if name == "" {
			return ""
		}
		qty := d.Qty
		if qty <= 0 {
			qty = 1
		}
		addItem(e, name, qty)
		return fmt.Sprintf("%s gains %s×%d.", e.Name, name, qty)
	case schema.DeltaItemRemove:
		name := strings.TrimSpace(d.Item)
		if name == "" {
			return ""
		}
		qty := d.Qty
		if qty <= 0 {
			qty = 1
		}
		if removeItem(e, name, qty) {
			return fmt.Sprintf("%s loses %s×%d.", e.Name, name, qty)
		}
		return ""
	case schema.DeltaXP:
		amt := clampNonNeg(d.Amount)
		if amt == 0 {
			return ""
		}
		e.XP += amt
		return fmt.Sprintf("%s gains %d XP.", e.Name, amt)
	}
	return ""
}

// levelUps advances any entity whose XP crossed its threshold. Leveling is an
// engine-owned consequence (like death), never trusted to the model. Assumes
// g.mu held.
func (g *Game) levelUps() []string {
	var logs []string
	for i := range g.state.Entities {
		e := &g.state.Entities[i]
		for e.Level >= 1 && e.XP >= e.XPForNext() {
			e.XP -= e.XPForNext()
			e.Level++
			e.MaxHP += 4
			if e.Alive {
				e.HP += 4 // the surge of a new level
			}
			logs = append(logs, fmt.Sprintf("%s reaches level %d!", e.Name, e.Level))
		}
	}
	return logs
}

// resolveTarget matches by id first, then case-insensitive name. Assumes lock.
func (g *Game) resolveTarget(ref string) *schema.Entity {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	for i := range g.state.Entities {
		if g.state.Entities[i].ID == ref {
			return &g.state.Entities[i]
		}
	}
	for i := range g.state.Entities {
		if strings.EqualFold(g.state.Entities[i].Name, ref) {
			return &g.state.Entities[i]
		}
	}
	return nil
}

func addItem(e *schema.Entity, name string, qty int) {
	for i := range e.Inventory {
		if strings.EqualFold(e.Inventory[i].Name, name) {
			e.Inventory[i].Qty += qty
			return
		}
	}
	e.Inventory = append(e.Inventory, schema.Item{Name: name, Qty: qty})
}

func removeItem(e *schema.Entity, name string, qty int) bool {
	for i := range e.Inventory {
		if strings.EqualFold(e.Inventory[i].Name, name) {
			e.Inventory[i].Qty -= qty
			if e.Inventory[i].Qty <= 0 {
				e.Inventory = append(e.Inventory[:i], e.Inventory[i+1:]...)
			}
			return true
		}
	}
	return false
}

func clampNonNeg(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func removeStr(ss []string, s string) []string {
	out := ss[:0]
	for _, x := range ss {
		if !strings.EqualFold(x, s) {
			out = append(out, x)
		}
	}
	return out
}
