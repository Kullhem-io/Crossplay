import { useEffect, useRef, useState } from 'react'
import { useCrossplay } from './useCrossplay'
import type { AgentStatus, Entity, TranscriptEntry } from './protocol'
import './App.css'

// Fixed seats always shown; player lanes are derived from the ledger so the
// party (one or more players) appears automatically.
const FIXED_LANES: { seat: string; label: string; brain: string }[] = [
  { seat: 'narrator', label: 'Narrator', brain: 'Qwen' },
  { seat: 'dm', label: 'Dungeon Master', brain: 'Gemma · low temp' },
]

export default function App() {
  const { conn, seats, state, log, transcript, mySeat, awaitingSeat, start, speakVoid, join, sendInput, leave } =
    useCrossplay()
  const [topic, setTopic] = useState('')
  const [voidText, setVoidText] = useState('')
  const [showJoin, setShowJoin] = useState(false)
  const [joinName, setJoinName] = useState('')
  const [joinClass, setJoinClass] = useState('')
  const [joinDesc, setJoinDesc] = useState('')
  const [turnText, setTurnText] = useState('')
  const started = state != null
  const playing = started && state!.phase === 'playing'
  const myName = mySeat ? state?.entities.find((e) => e.id === mySeat)?.name : undefined
  const myTurn = mySeat != null && awaitingSeat === mySeat
  const scrollRef = useRef<HTMLDivElement>(null)

  const playerLanes = (state?.entities ?? [])
    .filter((e) => e.kind === 'player')
    .map((e) => ({ seat: e.id, label: e.name, brain: 'Gemma · high temp' }))
  const lanes = [
    ...FIXED_LANES,
    ...(playerLanes.length ? playerLanes : [{ seat: 'player-1', label: 'Player', brain: 'Gemma · high temp' }]),
  ]

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [transcript])

  const onStart = () => topic.trim() && start(topic.trim())
  const onVoid = () => {
    if (!voidText.trim()) return
    speakVoid(voidText.trim())
    setVoidText('')
  }
  const onJoin = () => {
    if (!joinName.trim()) return
    join(joinName.trim(), joinClass.trim(), joinDesc.trim())
    setShowJoin(false)
    setJoinName('')
    setJoinClass('')
    setJoinDesc('')
  }
  const onTurn = () => {
    if (!turnText.trim()) return
    sendInput(turnText.trim())
    setTurnText('')
  }

  return (
    <div className="app">
      <header className="topbar">
        <h1>Crossplay</h1>
        <div className="topmeta">
          {started && <span className="round">Round {state!.round}</span>}
          <span className={`conn conn-${conn}`}>{conn}</span>
        </div>
      </header>

      <section className="lanes">
        {lanes.map((l) => (
          <Lane key={l.seat} label={l.label} brain={l.brain} status={seats[l.seat]} />
        ))}
      </section>

      <main className="stage">
        {!started ? (
          <div className="setup">
            <h2>Set the world</h2>
            <p className="hint">
              Anything goes: a flooded cathedral, a Target on Black Friday, the inside of a whale.
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
            {state!.phase === 'game_over' && (
              <div className={`gameover gameover-${state!.outcome || 'defeat'}`}>
                <span>{state!.outcome === 'victory' ? 'Victory.' : 'The adventure has ended.'}</span>
                <button onClick={() => location.reload()}>New world</button>
              </div>
            )}
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

      {playing && (
        <section className="playerbar">
          {myTurn ? (
            <>
              <input
                className="turn-input"
                value={turnText}
                onChange={(e) => setTurnText(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && onTurn()}
                placeholder={`You are ${myName}. What do you do?`}
                autoFocus
              />
              <button onClick={onTurn} disabled={conn !== 'open'}>
                Act
              </button>
            </>
          ) : mySeat ? (
            <div className="player-status">
              <span>
                You are <strong>{myName}</strong>. Waiting for your turn…
              </span>
              <button className="ghost" onClick={leave}>
                Leave
              </button>
            </div>
          ) : showJoin ? (
            <div className="join-form">
              <input
                value={joinName}
                onChange={(e) => setJoinName(e.target.value)}
                placeholder="Character name"
                autoFocus
              />
              <input
                value={joinClass}
                onChange={(e) => setJoinClass(e.target.value)}
                placeholder="Class / role (optional)"
              />
              <input
                value={joinDesc}
                onChange={(e) => setJoinDesc(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && onJoin()}
                placeholder="One-line description (optional)"
              />
              <button onClick={onJoin} disabled={conn !== 'open' || !joinName.trim()}>
                Enter the story
              </button>
              <button className="ghost" onClick={() => setShowJoin(false)}>
                Cancel
              </button>
            </div>
          ) : (
            <button className="join-cta" onClick={() => setShowJoin(true)}>
              Join the party
            </button>
          )}
        </section>
      )}

      <footer className="voidbar">
        <input
          value={voidText}
          onChange={(e) => setVoidText(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && onVoid()}
          placeholder={started ? 'Speak into the void…' : 'Begin a world to speak into the void'}
          disabled={!started || state!.phase === 'game_over'}
        />
        <button onClick={onVoid} disabled={conn !== 'open' || !started || state!.phase === 'game_over'}>
          Whisper
        </button>
      </footer>
    </div>
  )
}

function Beat({ e }: { e: TranscriptEntry }) {
  if (e.kind === 'action') {
    const monster = e.seat?.startsWith('monster')
    return (
      <p className={`beat-action ${monster ? 'beat-action-monster' : 'beat-action-player'}`}>
        <span className="beat-name">{e.name}</span> {e.text}
      </p>
    )
  }
  if (e.kind === 'mechanics') {
    if (!e.roll && (!e.changes || e.changes.length === 0)) return null
    return (
      <p className="beat-mech">
        <span className="mech-roll">🎲 {e.roll}</span>
        {e.changes && e.changes.length > 0 && <span className="mech-sep">·</span>}
        {e.changes?.map((c, i) => (
          <span key={i} className="mech-change">
            {c}
          </span>
        ))}
      </p>
    )
  }
  if (e.kind === 'void') {
    return (
      <p className="beat-void">
        <span className="beat-void-label">a voice from the void</span> “{e.text}”
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
  const isPlayer = e.kind === 'player'
  const xpNext = e.level >= 1 ? e.level * 10 : 10
  const xpPct = Math.max(0, Math.min(100, Math.round((e.xp / xpNext) * 100)))
  const subtitle = [e.class, e.level ? `Lv ${e.level}` : ''].filter(Boolean).join(' · ')
  return (
    <div className={`ent ent-${e.kind} ${e.alive ? '' : 'ent-dead'}`}>
      <div className="ent-head">
        <span className="ent-name">{e.name}</span>
        <span className="ent-hp">
          {e.hp}/{e.maxHp}
        </span>
      </div>
      {subtitle && <div className="ent-sub">{subtitle}</div>}
      <div className="hpbar">
        <div className="hpfill" style={{ width: `${pct}%` }} />
      </div>
      {isPlayer && (
        <>
          <div className="xpbar">
            <div className="xpfill" style={{ width: `${xpPct}%` }} />
          </div>
          <div className="ent-xp">
            XP {e.xp}/{xpNext}
          </div>
        </>
      )}
      {e.desc && <div className="ent-desc">{e.desc}</div>}
      {e.status.length > 0 && <div className="ent-status">{e.status.join(', ')}</div>}
    </div>
  )
}
