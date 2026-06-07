package transport

// Event is the envelope for every server->client message. Type discriminates
// the payload; clients switch on it. Keeping a single envelope keeps the
// WebSocket protocol uniform and easy to extend.
type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload,omitempty"`
}

// Server -> client event types.
const (
	EvHello       = "hello"        // sent once on connect
	EvAgentStatus = "agent_status" // a seat changed state (idle/thinking/streaming)
	EvAction      = "action"       // an actor (player/monster) declared an action
	EvMechanics   = "mechanics"    // dice roll + applied ledger changes for a beat
	EvVoid        = "void"         // echo of a Voice-from-the-Void utterance
	EvNarration   = "narration"    // a chunk of narrator prose
	EvState       = "state"        // full or partial game state
	EvLog         = "log"          // human-readable engine log line
	EvError       = "error"        // something went wrong
)

// Action is the payload for EvAction.
type Action struct {
	Seat string `json:"seat"`
	Name string `json:"name"`
	Text string `json:"text"`
}

// Mechanics is the payload for EvMechanics, the dice roll and the engine's
// applied changes for one actor's beat, surfaced for game-feel transparency.
type Mechanics struct {
	Seat    string   `json:"seat"`
	Name    string   `json:"name"`
	Roll    int      `json:"roll"`
	Changes []string `json:"changes"`
}

// Seat status values surfaced in the agent lanes.
const (
	StatusIdle      = "idle"
	StatusThinking  = "thinking"
	StatusStreaming = "streaming"
)

// AgentStatus is the payload for EvAgentStatus.
type AgentStatus struct {
	Seat   string `json:"seat"`   // "narrator" | "dm" | "player-1" | ...
	Brain  string `json:"brain"`  // backing brain id, for display
	Status string `json:"status"` // one of the Status* constants
	Note   string `json:"note,omitempty"`
}

// Inbound is the envelope for every client->server message.
type Inbound struct {
	Type    string          `json:"type"`
	Payload InboundPayload  `json:"payload"`
}

// InboundPayload carries the union of fields any client message might send.
// Kept flat for M0; will split per-type as the protocol grows.
type InboundPayload struct {
	Topic string `json:"topic,omitempty"` // for "start"
	Text  string `json:"text,omitempty"`  // for "void"
}

// Client -> server message types.
const (
	MsgStart = "start" // begin a game with a world topic
	MsgVoid  = "void"  // a Voice-from-the-Void utterance
)
