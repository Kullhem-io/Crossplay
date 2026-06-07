import { useEffect, useRef, useState } from 'react'
import { useCrossplay } from './useCrossplay'
import type { AgentStatus, Entity, TranscriptEntry } from './protocol'
import './App.css'

// The three seats shown as live lanes. (M0: static roster; later this comes
// from the engine so added players appear automatically.)
const LANES: { seat: string; label: string; brain: string }[] = [
  { seat: 'narrator', label: 'Narrator', brain: 'Qwen' },
  { seat: 'dm', label: 'Dungeon Master', brain: 'Gemma · low temp' },
  { seat: 'player-1', label: 'Player 1', brain: 'Gemma · high temp' },
]

export default function App() {
  const { conn, seats, state, log, transcript, start, speakVoid } = useCrossplay()
  const [topic, setTopic] = useState('')
  const [voidText, setVoidText] = useState('')
  const started = state != null
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [transcript])

  const onStart = () => topic.trim() && start(topic.trim())
  const onVoid = () => {
    if (!voidText.trim()) return
    speakVoid(voidText.trim())
    setVoidText('')
  }

  return (
    <div className="app">
      <header className="topbar">
        <h1>Crossplay</h1>
        <span className={`conn conn-${conn}`}>{conn}</span>
      </header>

      <section className="lanes">
        {LANES.map((l) => (
          <Lane key={l.seat} label={l.label} brain={l.brain} status={seats[l.seat]} />
        ))}
      </section>

      <main className="stage">
        {!started ? (
          <div className="setup">
            <h2>Set the world</h2>
            <p className="hint">
              Anything goes — a flooded cathedral, a Target on Black Friday, the inside of a whale.
            </p>
            <div className="row">
              <input
                value={topic}
                onChange={(e) => setTopic(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && onStart()}
                placeholder="Describe the world…"
                autoFocus
              />
              <button onClick={onStart} disabled={conn !== 'open'}>
                Begin
              </button>
            </div>
          </div>
        ) : (
          <div className="scene">
            <div className="location">
              <h2>{state!.location.name}</h2>
              <p className="muted">{state!.location.description}</p>
            </div>
            <article className="narrative" ref={scrollRef}>
              {transcript.length === 0 ? (
                <em>The world is taking shape…</em>
              ) : (
                transcript.map((e) => <Beat key={e.id} e={e} />)
              )}
            </article>
          </div>
        )}

        <aside className="side">
          {started && (
            <div className="entities">
              {state!.entities.map((e) => (
                <EntityCard key={e.id} e={e} />
              ))}
            </div>
          )}
          <div className="log">
            {log.length === 0 ? (
              <span className="muted">engine log…</span>
            ) : (
              log.map((line, i) => <div key={i}>{line}</div>)
            )}
          </div>
        </aside>
      </main>

      <footer className="voidbar">
        <input
          value={voidText}
          onChange={(e) => setVoidText(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && onVoid()}
          placeholder="Speak into the void…"
        />
        <button onClick={onVoid} disabled={conn !== 'open'}>
          Whisper
        </button>
      </footer>
    </div>
  )
}

function Beat({ e }: { e: TranscriptEntry }) {
  if (e.kind === 'action') {
    return (
      <p className="beat-action">
        <span className="beat-name">{e.name}</span> {e.text}
      </p>
    )
  }
  return <p className="beat-prose">{e.text}</p>
}

function Lane({ label, brain, status }: { label: string; brain: string; status?: AgentStatus }) {
  const s = status?.status ?? 'idle'
  return (
    <div className={`lane lane-${s}`}>
      <div className="lane-head">
        <span className="lane-label">{label}</span>
        <span className={`pip pip-${s}`} />
      </div>
      <div className="lane-brain">{brain}</div>
      <div className="lane-status">{s}</div>
    </div>
  )
}

function EntityCard({ e }: { e: Entity }) {
  const pct = e.maxHp > 0 ? Math.max(0, Math.round((e.hp / e.maxHp) * 100)) : 0
  return (
    <div className={`ent ent-${e.kind} ${e.alive ? '' : 'ent-dead'}`}>
      <div className="ent-head">
        <span className="ent-name">{e.name}</span>
        <span className="ent-hp">
          {e.hp}/{e.maxHp}
        </span>
      </div>
      <div className="hpbar">
        <div className="hpfill" style={{ width: `${pct}%` }} />
      </div>
      {e.status.length > 0 && (
        <div className="ent-status">{e.status.join(', ')}</div>
      )}
    </div>
  )
}
