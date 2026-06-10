package engine

import (
	"fmt"
	"strings"

	"github.com/Kullhem-io/Crossplay/internal/schema"
)

// applyAdjudication applies a DM ruling to the ledger under lock, enforcing
// invariants (HP bounds, non-negative item counts, death) and returning human
// log lines. actor is the id of the acting entity for a player turn (empty for
// the monster phase); self-rewards (XP, picked-up items) are pinned to the actor
// because the DM routinely mis-targets them onto a teammate. Broadcasting is the
// caller's job.
func (g *Game) applyAdjudication(adj *schema.Adjudication, actor string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state == nil {
		return nil
	}

	if actor != "" {
		g.pinSelfRewards(adj, actor)
	}

	var logs []string
	for _, d := range adj.Deltas {
		if line := g.applyDelta(d); line != "" {
			logs = append(logs, line)
		}
	}

	// Leveling is engine-owned (thresholds), like death below.
	logs = append(logs, g.levelUps()...)

	// Death is an engine-owned consequence, never trusted to the model. An
	// entity dies at 0 HP; additionally, a non-player foe the DM tags with a
	// removal status ("destroyed", "defeated", ...) is taken out of the fight,
	// even if it still has HP. Without this, the DM "defeats" a foe with a
	// status while its HP stays positive, so victory never triggers and the
	// fight loops. The game-over transition itself is decided by finalizeIfEnded.
	for i := range g.state.Entities {
		e := &g.state.Entities[i]
		if !e.Alive {
			continue
		}
		diedByHP := e.HP <= 0
		removed := e.Kind != schema.KindPlayer && hasRemovalStatus(e)
		if diedByHP || removed {
			e.HP = 0
			e.Alive = false
			if diedByHP {
				logs = append(logs, e.Name+" falls.")
			} else {
				logs = append(logs, e.Name+" is out of the fight.")
			}
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
	switch {
	case len(g.state.LivingPlayers()) == 0:
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

// pinSelfRewards rewrites a player's own gains to point at the acting player.
// XP always belongs to the actor. A picked-up item belongs to the actor too,
// with one exception: when the same ruling also removes that item from someone,
// it is a deliberate hand-off (giver loses it, receiver gains it), so the DM's
// target is honored. Assumes g.mu held.
func (g *Game) pinSelfRewards(adj *schema.Adjudication, actor string) {
	for i := range adj.Deltas {
		d := &adj.Deltas[i]
		switch d.Type {
		case schema.DeltaXP:
			d.Target = actor
		case schema.DeltaItemAdd:
			tgt := g.resolveTarget(d.Target)
			if tgt != nil && tgt.Kind == schema.KindPlayer && tgt.ID != actor && itemAlsoRemoved(adj, d.Item) {
				continue
			}
			if tgt == nil || tgt.Kind == schema.KindPlayer {
				d.Target = actor
			}
		}
	}
}

// itemAlsoRemoved reports whether the ruling removes the named item from
// anyone, the signature of a hand-off rather than a mis-targeted pickup.
func itemAlsoRemoved(adj *schema.Adjudication, item string) bool {
	for _, d := range adj.Deltas {
		if d.Type == schema.DeltaItemRemove && strings.EqualFold(strings.TrimSpace(d.Item), strings.TrimSpace(item)) {
			return true
		}
	}
	return false
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

// removalStatuses are status words that mean a foe is out of the fight. The DM
// sometimes "defeats" an enemy with one of these instead of dealing lethal
// damage; the engine treats them as removal so the encounter can actually end.
var removalStatuses = map[string]bool{
	"dead": true, "destroyed": true, "defeated": true, "slain": true,
	"killed": true, "incapacitated": true, "unconscious": true,
	"neutralized": true, "subdued": true, "vanquished": true,
	"banished": true, "eliminated": true, "down": true, "out": true,
}

func hasRemovalStatus(e *schema.Entity) bool {
	for _, s := range e.Status {
		if removalStatuses[strings.ToLower(strings.TrimSpace(s))] {
			return true
		}
	}
	return false
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
