// Package engine owns the authoritative game ledger and the turn loop. Models
// (via the scheduler) only ever propose changes; the engine validates and
// applies them, and is the single source of state broadcast to clients.
package engine

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

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
	mu          sync.Mutex
	state       *schema.GameState
	recent      []string // rolling beat summaries for model continuity
	voidPending []string // queued Voice-from-the-Void utterances

	sched *agents.Scheduler
	emit  func(transport.Event)

	rngMu sync.Mutex
	rng   *rand.Rand

	loopCancel context.CancelFunc // cancels the running play loop, if any
}

func New(sched *agents.Scheduler, emit func(transport.Event)) *Game {
	seed := uint64(time.Now().UnixNano())
	return &Game{
		sched: sched,
		emit:  emit,
		rng:   rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)),
	}
}

// d20 rolls a fair, seeded twenty-sided die. The engine owns all randomness.
func (g *Game) d20() int {
	g.rngMu.Lock()
	defer g.rngMu.Unlock()
	return g.rng.IntN(20) + 1
}

// remember appends a beat summary to the rolling context (bounded).
func (g *Game) remember(line string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.recent = append(g.recent, line)
	if len(g.recent) > 4 {
		g.recent = g.recent[len(g.recent)-4:]
	}
}

// recentContext returns the recent beats as a single block, or "".
func (g *Game) recentContext() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.recent) == 0 {
		return ""
	}
	return "Recently: " + joinLines(g.recent)
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

func joinLines(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
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
