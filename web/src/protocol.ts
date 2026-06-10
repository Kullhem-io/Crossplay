// Wire protocol shared with the Go backend (internal/transport/events.go).
// Ledger types are generated from Go, see src/gen/schema.ts (make gen-types).

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
  type:
    | 'hello'
    | 'agent_status'
    | 'action'
    | 'mechanics'
    | 'void'
    | 'narration'
    | 'state'
    | 'log'
    | 'error'
    | 'await_input'
    | 'joined'
    | 'left'
  payload?: unknown
}

export interface ActionPayload {
  seat: string
  name: string
  text: string
}

export interface MechanicsPayload {
  seat: string
  name: string
  roll: number
  changes: string[]
}

// A rendered transcript entry, actions, mechanics, prose, void whispers, and
// round markers interleaved in order.
export interface TranscriptEntry {
  id: number
  kind: 'action' | 'prose' | 'void' | 'mechanics' | 'round'
  seat?: string
  name?: string
  text?: string
  roll?: number
  changes?: string[]
  round?: number
}

// Client -> server envelope.
export interface ClientMessage {
  type: 'start' | 'void' | 'join' | 'player_input' | 'leave'
  payload: {
    topic?: string
    text?: string
    seat?: string
    name?: string
    class?: string
    desc?: string
  }
}
