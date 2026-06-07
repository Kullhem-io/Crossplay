package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/Kullhem-io/Crossplay/internal/agents"
	"github.com/Kullhem-io/Crossplay/internal/schema"
)

// openingScene streams an atmospheric opening from the narrator, grounded in
// the generated world so prose and ledger agree from turn one.
func (g *Game) openingScene(ctx context.Context) error {
	st := g.Snapshot()
	if st == nil {
		return fmt.Errorf("no state")
	}

	msgs := []agents.Message{
		{Role: "system", Content: "You are the Narrator of a text RPG. Write vivid, atmospheric prose in second person, present tense — 2–3 short paragraphs. Establish the scene and the threat, end on a hook. Do not invent mechanics, HP, or items beyond what you're told; do not ask the player questions or break character."},
		{Role: "user", Content: "Narrate the opening. " + sceneBrief(st)},
	}

	ch, err := g.sched.Stream(ctx, BrainQwen, msgs, agents.CallOpts{Temperature: 1.0, Priority: 10})
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

// sceneBrief renders the ledger as a compact factual brief for model context —
// the "hard state" the prose must stay consistent with.
func sceneBrief(st *schema.GameState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Location: %s — %s\n", st.Location.Name, st.Location.Description)
	if p := st.Player(); p != nil {
		fmt.Fprintf(&b, "Player: %s (HP %d/%d)", p.Name, p.HP, p.MaxHP)
		if p.Desc != "" {
			fmt.Fprintf(&b, " — %s", p.Desc)
		}
		if len(p.Inventory) > 0 {
			var items []string
			for _, it := range p.Inventory {
				items = append(items, fmt.Sprintf("%s×%d", it.Name, it.Qty))
			}
			fmt.Fprintf(&b, ". Carrying: %s", strings.Join(items, ", "))
		}
		b.WriteString("\n")
	}
	living := st.LivingMonsters()
	if len(living) > 0 {
		b.WriteString("Threats present:\n")
		for _, m := range living {
			fmt.Fprintf(&b, "  - %s (HP %d/%d) — %s\n", m.Name, m.HP, m.MaxHP, m.Desc)
		}
	}
	return b.String()
}
