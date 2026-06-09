package agents

import (
	"context"
	"sync"
)

// HumanBrain is a Brain backed by a person at the UI rather than a model. Its
// Stream announces that a seat needs input (via ask) and blocks until the
// matching reply arrives through Deliver, or the context is done. Because it
// satisfies the same Brain interface, the engine and scheduler treat a human
// seat exactly like an AI one: "AI plays" and "you play" share one path.
type HumanBrain struct {
	id  string
	ask func(seat string) // notify the UI that this seat owes an action

	mu      sync.Mutex
	pending map[string]chan string // seat -> reply channel
}

func NewHumanBrain(id string, ask func(seat string)) *HumanBrain {
	return &HumanBrain{id: id, ask: ask, pending: make(map[string]chan string)}
}

func (b *HumanBrain) ID() string { return b.id }

// MaxConcurrent is generous: humans do not contend for the GPU.
func (b *HumanBrain) MaxConcurrent() int { return 16 }

// Deliver hands a human's typed reply to the Stream waiting on that seat.
func (b *HumanBrain) Deliver(seat, text string) {
	b.mu.Lock()
	ch := b.pending[seat]
	b.mu.Unlock()
	if ch != nil {
		select {
		case ch <- text:
		default: // no one waiting, or already answered
		}
	}
}

func (b *HumanBrain) Stream(ctx context.Context, _ []Message, opts CallOpts) (<-chan Token, error) {
	seat := opts.Seat
	reply := make(chan string, 1)
	b.mu.Lock()
	b.pending[seat] = reply
	b.mu.Unlock()

	if b.ask != nil {
		b.ask(seat)
	}

	out := make(chan Token)
	go func() {
		defer close(out)
		defer func() {
			b.mu.Lock()
			delete(b.pending, seat)
			b.mu.Unlock()
		}()
		select {
		case text := <-reply:
			select {
			case out <- Token{Text: text}:
			case <-ctx.Done():
			}
		case <-ctx.Done():
			// timed out or cancelled; close empty so the caller can fall back
		}
	}()
	return out, nil
}

var _ Brain = (*HumanBrain)(nil)
