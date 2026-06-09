import { useCallback, useEffect, useRef, useState } from 'react'
import type {
  ActionPayload,
  AgentStatus,
  ClientMessage,
  GameState,
  MechanicsPayload,
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
  mySeat: string | null // the seat this human controls, if any
  awaitingSeat: string | null // a seat currently awaiting human input
  start: (topic: string) => void
  speakVoid: (text: string) => void
  join: (name: string, klass: string, desc: string) => void
  sendInput: (text: string) => void
  leave: () => void
}

// useCrossplay owns the single WebSocket to the engine, auto-reconnects, and
// reduces server events into render-ready state.
export function useCrossplay(): CrossplayState {
  const [conn, setConn] = useState<ConnState>('connecting')
  const [seats, setSeats] = useState<Record<string, AgentStatus>>({})
  const [state, setState] = useState<GameState | null>(null)
  const [log, setLog] = useState<string[]>([])
  const [transcript, setTranscript] = useState<TranscriptEntry[]>([])
  const [mySeat, setMySeat] = useState<string | null>(null)
  const [awaitingSeat, setAwaitingSeat] = useState<string | null>(null)
  const mySeatRef = useRef<string | null>(null)
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
              { id: nextId.current++, kind: 'action', seat: a.seat, name: a.name, text: a.text },
            ])
            // My seat's action landed (mine or an AI fallback): stop awaiting.
            if (a.seat === mySeatRef.current) setAwaitingSeat(null)
            break
          }
          case 'mechanics': {
            const m = ev.payload as MechanicsPayload
            setTranscript((t) => [
              ...t,
              {
                id: nextId.current++,
                kind: 'mechanics',
                seat: m.seat,
                name: m.name,
                roll: m.roll,
                changes: m.changes ?? [],
              },
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
            const tok = p.token
            setTranscript((t) => {
              const last = t[t.length - 1]
              if (last && last.kind === 'prose') {
                return [...t.slice(0, -1), { ...last, text: (last.text ?? '') + tok }]
              }
              return [...t, { id: nextId.current++, kind: 'prose', text: tok }]
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
          case 'await_input': {
            const p = ev.payload as { seat?: string }
            if (p.seat && p.seat === mySeatRef.current) setAwaitingSeat(p.seat)
            break
          }
          case 'joined': {
            const p = ev.payload as { seat?: string }
            if (p.seat) {
              mySeatRef.current = p.seat
              setMySeat(p.seat)
            }
            break
          }
          case 'left': {
            const p = ev.payload as { seat?: string }
            if (p.seat && p.seat === mySeatRef.current) {
              mySeatRef.current = null
              setMySeat(null)
              setAwaitingSeat(null)
            }
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
  const join = useCallback(
    (name: string, klass: string, desc: string) =>
      send({ type: 'join', payload: { name, class: klass, desc } }),
    [send],
  )
  const sendInput = useCallback(
    (text: string) => {
      const seat = mySeatRef.current
      if (!seat) return
      send({ type: 'player_input', payload: { seat, text } })
      setAwaitingSeat(null)
    },
    [send],
  )
  const leave = useCallback(() => {
    const seat = mySeatRef.current
    if (seat) send({ type: 'leave', payload: { seat } })
  }, [send])

  return {
    conn,
    seats,
    state,
    log,
    transcript,
    mySeat,
    awaitingSeat,
    start,
    speakVoid,
    join,
    sendInput,
    leave,
  }
}

export const SEAT_STATUS_FALLBACK: SeatStatus = 'idle'
