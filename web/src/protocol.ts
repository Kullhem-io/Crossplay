// Wire protocol shared with the Go backend (internal/transport/events.go).
// Ledger types are generated from Go — see src/gen/schema.ts (make gen-types).

export type { GameState, Entity, Location, Item, Phase, EntityKind } from './gen/schema'

export type SeatStatus = 'idle' | 'thinking' | 'streaming'

export interface AgentStatus {
  seat: string
  brain: string
  status: SeatStatus
  note?: string
}

// Server -> client envelope.
export interface ServerEvent {
  type: 'hello' | 'agent_status' | 'action' | 'void' | 'narration' | 'state' | 'log' | 'error'
  payload?: unknown
}

export interface ActionPayload {
  seat: string
  name: string
  text: string
}

// A rendered transcript entry — actions, prose, and void whispers interleaved.
export interface TranscriptEntry {
  id: number
  kind: 'action' | 'prose' | 'void'
  name?: string
  text: string
}

// Client -> server envelope.
export interface ClientMessage {
  type: 'start' | 'void'
  payload: { topic?: string; text?: string }
}
