package agents

import (
	"container/heap"
	"context"
	"fmt"
	"strings"
	"sync"
)

// Scheduler enforces each brain's concurrency budget. Calls beyond the budget
// queue and are admitted highest-priority-first (FIFO within a priority). This
// is what keeps "Qwen = 1 in flight, Gemma = 2" honest while letting urgent
// player/DM turns jump ahead of speculative narrator work.
type Scheduler struct {
	mu     sync.RWMutex
	brains map[string]Brain
	gates  map[string]*gate
}

func NewScheduler() *Scheduler {
	return &Scheduler{brains: map[string]Brain{}, gates: map[string]*gate{}}
}

// Register adds a brain and creates its concurrency gate.
func (s *Scheduler) Register(b Brain) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.brains[b.ID()] = b
	s.gates[b.ID()] = newGate(b.MaxConcurrent())
}

// Stream acquires a slot on the named brain (blocking by priority until one is
// free or ctx is cancelled), then streams the completion. The slot is released
// when the returned channel closes.
func (s *Scheduler) Stream(ctx context.Context, brainID string, msgs []Message, opts CallOpts) (<-chan Token, error) {
	s.mu.RLock()
	b, ok := s.brains[brainID]
	g := s.gates[brainID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("scheduler: unknown brain %q", brainID)
	}

	if err := g.acquire(ctx, opts.Priority); err != nil {
		return nil, err
	}

	raw, err := b.Stream(ctx, msgs, opts)
	if err != nil {
		g.release()
		return nil, err
	}

	out := make(chan Token)
	go func() {
		defer close(out)
		defer g.release()
		for t := range raw {
			select {
			case out <- t:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// Complete is a convenience wrapper that collects a full (non-streamed)
// response. Used by seats that need the whole answer before acting (worldgen,
// adjudication).
func (s *Scheduler) Complete(ctx context.Context, brainID string, msgs []Message, opts CallOpts) (string, error) {
	ch, err := s.Stream(ctx, brainID, msgs, opts)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for t := range ch {
		if t.Err != nil {
			return sb.String(), t.Err
		}
		sb.WriteString(t.Text)
	}
	return sb.String(), nil
}

// --- per-brain priority gate ---

type gate struct {
	mu     sync.Mutex
	n      int // max concurrent
	active int
	seq    int64
	waits  reqHeap
}

func newGate(n int) *gate {
	if n < 1 {
		n = 1
	}
	g := &gate{n: n}
	heap.Init(&g.waits)
	return g
}

func (g *gate) acquire(ctx context.Context, priority int) error {
	g.mu.Lock()
	if g.active < g.n {
		g.active++
		g.mu.Unlock()
		return nil
	}
	g.seq++
	r := &request{priority: priority, seq: g.seq, ready: make(chan struct{})}
	heap.Push(&g.waits, r)
	g.mu.Unlock()

	select {
	case <-r.ready:
		// Slot transferred to us by a release(); active already accounts for it.
		return nil
	case <-ctx.Done():
		g.mu.Lock()
		select {
		case <-r.ready:
			// Granted concurrently with cancellation: hand the slot back.
			g.mu.Unlock()
			g.release()
		default:
			r.cancelled = true // release() will skip us
			g.mu.Unlock()
		}
		return ctx.Err()
	}
}

func (g *gate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for g.waits.Len() > 0 {
		r := heap.Pop(&g.waits).(*request)
		if r.cancelled {
			continue
		}
		close(r.ready) // transfer the slot; active unchanged
		return
	}
	g.active--
}

type request struct {
	priority  int
	seq       int64
	ready     chan struct{}
	cancelled bool
	index     int
}

// reqHeap orders by priority (desc), then seq (asc) for FIFO fairness.
type reqHeap []*request

func (h reqHeap) Len() int { return len(h) }
func (h reqHeap) Less(i, j int) bool {
	if h[i].priority != h[j].priority {
		return h[i].priority > h[j].priority
	}
	return h[i].seq < h[j].seq
}
func (h reqHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}
func (h *reqHeap) Push(x any) {
	r := x.(*request)
	r.index = len(*h)
	*h = append(*h, r)
}
func (h *reqHeap) Pop() any {
	old := *h
	n := len(old)
	r := old[n-1]
	old[n-1] = nil
	*h = old[:n-1]
	return r
}
