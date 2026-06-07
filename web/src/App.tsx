import { useState } from 'react'
import { useCrossplay } from './useCrossplay'
import type { AgentStatus } from './protocol'
import './App.css'

// The three seats shown as live lanes. (M0: static roster; later this comes
// from the engine so added players appear automatically.)
const LANES: { seat: string; label: string; brain: string }[] = [
  { seat: 'narrator', label: 'Narrator', brain: 'Qwen' },
  { seat: 'dm', label: 'Dungeon Master', brain: 'Gemma · low temp' },
  { seat: 'player-1', label: 'Player 1', brain: 'Gemma · high temp' },
]

export default function App() {
  const { conn, seats, log, narration, start, speakVoid } = useCrossplay()
  const [topic, setTopic] = useState('')
  const [voidText, setVoidText] = useState('')
  const [started, setStarted] = useState(false)

  const onStart = () => {
    if (!topic.trim()) return
    start(topic.trim())
    setStarted(true)
  }
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
          <article className="narrative">{narration || <em>The world is taking shape…</em>}</article>
        )}

        <aside className="log">
          {log.length === 0 ? (
            <span className="muted">engine log…</span>
          ) : (
            log.map((line, i) => <div key={i}>{line}</div>)
          )}
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
