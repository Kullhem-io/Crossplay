// Wire protocol shared with the Go backend (internal/transport/events.go).
// Hand-maintained for M0; will be replaced by Go->TS codegen in M2.

export type SeatStatus = 'idle' | 'thinking' | 'streaming'

export interface AgentStatus {
  seat: string
  brain: string
  status: SeatStatus
  note?: string
}

// Server -> client envelope.
export interface ServerEvent {
  type: 'hello' | 'agent_status' | 'narration' | 'state' | 'log' | 'error'
  payload?: unknown
}

// Client -> server envelope.
export interface ClientMessage {
  type: 'start' | 'void'
  payload: { topic?: string; text?: string }
}
