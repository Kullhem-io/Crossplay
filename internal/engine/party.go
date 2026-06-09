package engine

import (
	"fmt"

	"github.com/Kullhem-io/Crossplay/internal/schema"
	"github.com/Kullhem-io/Crossplay/internal/transport"
)

// Join adds a human-controlled player to the party. The new character is a
// normal player entity bound to the human brain, so from the next round the
// turn loop asks the person for its action like any other seat. No-op if there
// is no game in progress.
func (g *Game) Join(name, class, desc string) {
	g.mu.Lock()
	if g.state == nil || g.state.Phase != schema.PhasePlaying {
		g.mu.Unlock()
		return
	}
	id := fmt.Sprintf("player-%d", len(g.state.Players())+1)
	e := schema.Entity{
		ID:        id,
		Name:      orDefault(name, "Newcomer"),
		Kind:      schema.KindPlayer,
		Class:     orDefault(class, "Wanderer"),
		Level:     1,
		MaxHP:     22,
		HP:        22,
		Alive:     true,
		Status:    []string{},
		Inventory: []schema.Item{},
		Desc:      desc,
	}
	g.state.Entities = append(g.state.Entities, e)
	g.seatBrain[id] = BrainHuman
	g.mu.Unlock()

	g.logf(e.Name + " joins the party")
	g.remember(e.Name + " joins the party")
	g.broadcastState()
	g.emit(transport.Event{Type: transport.EvJoined,
		Payload: map[string]any{"seat": id, "name": e.Name}})
}

// Leave hands a human seat back to the AI. The character stays in the story and
// plays autonomously from the next round.
func (g *Game) Leave(seat string) {
	g.mu.Lock()
	g.seatBrain[seat] = BrainGemma
	name := ""
	if e := g.state.FindEntity(seat); e != nil {
		name = e.Name
	}
	g.mu.Unlock()

	if name != "" {
		g.logf(name + " steps back; instinct takes the reins")
	}
	g.emit(transport.Event{Type: transport.EvLeft, Payload: map[string]any{"seat": seat}})
}
