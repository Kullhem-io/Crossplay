// Package engine owns the authoritative game ledger and the turn loop. Models
// (via the scheduler) only ever propose changes; the engine validates and
// applies them, and is the single source of state broadcast to clients.
package engine

import (
	"sync"

	"github.com/Kullhem-io/Crossplay/internal/agents"
	"github.com/Kullhem-io/Crossplay/internal/schema"
	"github.com/Kullhem-io/Crossplay/internal/transport"
)

// Seat ids and their default brain bindings.
const (
	SeatNarrator = "narrator"
	SeatDM       = "dm"
	SeatPlayer1  = "player-1"

	BrainQwen  = "qwen-local"
	BrainGemma = "gemma-local"
)

// Game holds the canonical state and orchestrates seats. One per process for now.
type Game struct {
	mu    sync.Mutex
	state *schema.GameState

	sched *agents.Scheduler
	emit  func(transport.Event)
}

func New(sched *agents.Scheduler, emit func(transport.Event)) *Game {
	return &Game{sched: sched, emit: emit}
}

// --- emit helpers (small, so seats read cleanly) ---

func (g *Game) seat(seat, brain, status string) {
	g.emit(transport.Event{Type: transport.EvAgentStatus,
		Payload: transport.AgentStatus{Seat: seat, Brain: brain, Status: status}})
}

func (g *Game) logf(line string) {
	g.emit(transport.Event{Type: transport.EvLog, Payload: map[string]any{"line": line}})
}

func (g *Game) errf(err error) {
	g.emit(transport.Event{Type: transport.EvError, Payload: map[string]any{"message": err.Error()}})
}

func (g *Game) narrate(token string) {
	g.emit(transport.Event{Type: transport.EvNarration, Payload: map[string]any{"token": token}})
}

// broadcastState marshals and emits a snapshot of the ledger. Caller must NOT
// hold g.mu (this takes it).
func (g *Game) broadcastState() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.emit(transport.Event{Type: transport.EvState, Payload: g.state})
}

// Snapshot returns a shallow copy of the current state (or nil).
func (g *Game) Snapshot() *schema.GameState {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state == nil {
		return nil
	}
	cp := *g.state
	return &cp
}
