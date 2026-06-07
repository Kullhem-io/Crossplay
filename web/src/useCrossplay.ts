import { useCallback, useEffect, useRef, useState } from 'react'
import type {
  ActionPayload,
  AgentStatus,
  ClientMessage,
  GameState,
  ServerEvent,
  SeatStatus,
  TranscriptEntry,
} from './protocol'

export type ConnState = 'connecting' | 'open' | 'closed'

export interface CrossplayState {
  conn: ConnState
  seats: Record<string, AgentStatus> // keyed by seat id
  state: GameState | null
  log: string[]
  transcript: TranscriptEntry[]
  start: (topic: string) => void
  speakVoid: (text: string) => void
}

// useCrossplay owns the single WebSocket to the engine, auto-reconnects, and
// reduces server events into render-ready state.
export function useCrossplay(): CrossplayState {
  const [conn, setConn] = useState<ConnState>('connecting')
  const [seats, setSeats] = useState<Record<string, AgentStatus>>({})
  const [state, setState] = useState<GameState | null>(null)
  const [log, setLog] = useState<string[]>([])
  const [transcript, setTranscript] = useState<TranscriptEntry[]>([])
  const nextId = useRef(0)
  const wsRef = useRef<WebSocket | null>(null)
  const retryRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const send = useCallback((msg: ClientMessage) => {
    wsRef.current?.readyState === WebSocket.OPEN &&
      wsRef.current.send(JSON.stringify(msg))
  }, [])

  useEffect(() => {
    let closed = false

    const connect = () => {
      const proto = location.protocol === 'https:' ? 'wss' : 'ws'
      const ws = new WebSocket(`${proto}://${location.host}/ws`)
      wsRef.current = ws
      setConn('connecting')

      ws.onopen = () => setConn('open')
      ws.onclose = () => {
        setConn('closed')
        if (!closed) retryRef.current = setTimeout(connect, 1000)
      }
      ws.onmessage = (e) => {
        let ev: ServerEvent
        try {
          ev = JSON.parse(e.data)
        } catch {
          return
        }
        switch (ev.type) {
          case 'agent_status': {
            const s = ev.payload as AgentStatus
            setSeats((prev) => ({ ...prev, [s.seat]: s }))
            break
          }
          case 'state': {
            setState(ev.payload as GameState)
            break
          }
          case 'action': {
            const a = ev.payload as ActionPayload
            setTranscript((t) => [
              ...t,
              { id: nextId.current++, kind: 'action', name: a.name, text: a.text },
            ])
            break
          }
          case 'void': {
            const p = ev.payload as { text?: string }
            if (p.text)
              setTranscript((t) => [
                ...t,
                { id: nextId.current++, kind: 'void', text: p.text! },
              ])
            break
          }
          case 'narration': {
            const p = ev.payload as { token?: string }
            if (!p.token) break
            setTranscript((t) => {
              const last = t[t.length - 1]
              if (last && last.kind === 'prose') {
                return [...t.slice(0, -1), { ...last, text: last.text + p.token }]
              }
              return [...t, { id: nextId.current++, kind: 'prose', text: p.token! }]
            })
            break
          }
          case 'log': {
            const p = ev.payload as { line?: string }
            if (p.line) setLog((l) => [...l, p.line!])
            break
          }
          case 'error': {
            const p = ev.payload as { message?: string }
            if (p.message) setLog((l) => [...l, `⚠ ${p.message}`])
            break
          }
        }
      }
    }

    connect()
    return () => {
      closed = true
      if (retryRef.current) clearTimeout(retryRef.current)
      wsRef.current?.close()
    }
  }, [])

  const start = useCallback(
    (topic: string) => send({ type: 'start', payload: { topic } }),
    [send],
  )
  const speakVoid = useCallback(
    (text: string) => send({ type: 'void', payload: { text } }),
    [send],
  )

  return { conn, seats, state, log, transcript, start, speakVoid }
}

export const SEAT_STATUS_FALLBACK: SeatStatus = 'idle'
