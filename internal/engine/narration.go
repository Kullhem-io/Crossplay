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
	return g.streamNarration(ctx, []agents.Message{
		{Role: "system", Content: "You are the Narrator of an interactive story. Write vivid, atmospheric prose in second person, present tense, 2 to 3 short paragraphs. Establish the scene and the threat, end on a hook. " + genreRule + " Do not invent mechanics, HP, or items beyond what you're told; do not ask the player questions or break character."},
		{Role: "user", Content: "Narrate the opening. " + sceneBrief(st)},
	})
}

// narrateEnding streams a closing passage when the game ends, fitted to whether
// the player triumphed or fell. This is the curtain the loop used to skip.
func (g *Game) narrateEnding(ctx context.Context) {
	st := g.Snapshot()
	if st == nil {
		return
	}
	var sys, ask string
	if st.Outcome == schema.OutcomeVictory {
		g.logf("victory")
		sys = "You are the Narrator of an interactive story, writing the final passage. The player has overcome every threat. Write a satisfying, vivid closing: the last adversary falling, the quiet after, what the player has won. Second person, present tense, 1 to 2 short paragraphs. " + genreRule + " Do not start a new conflict or ask questions. End the story."
		ask = "Narrate the victory and the aftermath."
	} else {
		g.logf("the adventure is over")
		sys = "You are the Narrator of an interactive story, writing the final passage. The player has fallen. Write a somber, vivid closing of their last moments and how the scene falls still around them. Second person shifting to a final remove, present tense, 1 to 2 short paragraphs. " + genreRule + " Do not revive them or ask questions. End the story."
		ask = "Narrate the player's defeat and final moments."
	}
	if err := g.streamNarration(ctx, []agents.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: joinNonEmpty("\n", sceneBrief(st), g.recentContext(), ask)},
	}); err != nil && ctx.Err() == nil {
		g.errf(fmt.Errorf("narrate ending: %w", err))
	}
}

// narrateArrival streams a short beat introducing a newly joined character into
// the current scene, so a human (or any added party member) is written into the
// story rather than just appearing on the ledger.
func (g *Game) narrateArrival(ctx context.Context, e schema.Entity) {
	st := g.Snapshot()
	if st == nil {
		return
	}
	g.narrate("\n\n") // start a fresh paragraph so the entrance doesn't run on
	sys := "You are the Narrator of an interactive story. A new character is entering the scene mid-story. Introduce them naturally and vividly, weaving their arrival into the present moment, 1 to 2 short sentences, present tense. " + genreRule + " Do not restate the whole scene and do not break character."
	who := fmt.Sprintf("A newcomer joins the party: %s", e.Name)
	if e.Class != "" {
		who += ", " + e.Class
	}
	if e.Desc != "" {
		who += " (" + e.Desc + ")"
	}
	if err := g.streamNarration(ctx, []agents.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: joinNonEmpty("\n", sceneBrief(st), who+".", "Narrate their entrance into the current scene.")},
	}); err != nil && ctx.Err() == nil {
		g.errf(fmt.Errorf("narrate arrival: %w", err))
	}
}

// streamNarration runs a narrator (Qwen) call and streams its tokens out,
// managing the seat lane status. It retries once if the connection drops before
// any tokens arrive (e.g. transient GPU contention / EOF); a mid-stream drop
// keeps the partial prose rather than restarting.
func (g *Game) streamNarration(ctx context.Context, msgs []agents.Message) error {
	g.seat(SeatNarrator, BrainQwen, "thinking")
	defer g.seat(SeatNarrator, BrainQwen, "idle")

	for attempt := 0; attempt < 2; attempt++ {
		ch, err := g.sched.Stream(ctx, BrainQwen, msgs, agents.CallOpts{Temperature: 1.0, Priority: 10})
		if err != nil {
			if attempt == 0 && ctx.Err() == nil {
				continue // retry from scratch
			}
			return err
		}
		emitted := false
		failed := false
		for t := range ch {
			if t.Err != nil {
				if !emitted && attempt == 0 && ctx.Err() == nil {
					failed = true // nothing shown yet: safe to retry
					break
				}
				return t.Err
			}
			if !emitted {
				g.seat(SeatNarrator, BrainQwen, "streaming")
				emitted = true
			}
			g.narrate(t.Text)
		}
		if emitted || !failed {
			return nil
		}
	}
	return nil
}

// sceneBrief renders the ledger as a compact factual brief for model context:
// the "hard state" the prose must stay consistent with.
func sceneBrief(st *schema.GameState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Location: %s, %s\n", st.Location.Name, st.Location.Description)
	if p := st.Player(); p != nil {
		fmt.Fprintf(&b, "Player: %s (HP %d/%d)", p.Name, p.HP, p.MaxHP)
		if p.Desc != "" {
			fmt.Fprintf(&b, ", %s", p.Desc)
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
			fmt.Fprintf(&b, "  - %s (HP %d/%d), %s\n", m.Name, m.HP, m.MaxHP, m.Desc)
		}
	}
	return b.String()
}
