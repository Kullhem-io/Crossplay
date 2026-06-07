// Package agents defines the Brain abstraction, a model connection with a
// concurrency budget, and a scheduler that enforces those budgets with
// priority. Seats (DM, narrator, players) are bound to brains elsewhere; a
// brain knows nothing about the game.
package agents

import "context"

// Message is one chat turn. Mirrors the OpenAI chat format.
type Message struct {
	Role    string `json:"role"` // "system" | "user" | "assistant"
	Content string `json:"content"`
}

// CallOpts tunes a single completion.
type CallOpts struct {
	Temperature float64
	MaxTokens   int      // 0 = let the server decide
	Stop        []string // optional stop sequences
	// JSONSchema, if set, asks the server to constrain output to this JSON
	// schema (structured output). Used by the DM/worldgen seats.
	JSONSchema []byte
	// Priority orders queued calls on a saturated brain; higher runs first.
	// Player/DM turns should outrank speculative narrator pre-builds.
	Priority int
}

// Token is one streamed chunk. A Token with Err set is terminal; the channel
// is closed afterward.
type Token struct {
	Text string
	Err  error
}

// Brain is a model connection. Implementations must respect ctx cancellation
// (abort the in-flight request) and close the returned channel when finished.
type Brain interface {
	ID() string
	MaxConcurrent() int
	Stream(ctx context.Context, msgs []Message, opts CallOpts) (<-chan Token, error)
}
