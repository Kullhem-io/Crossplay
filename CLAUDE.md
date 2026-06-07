# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Crossplay is an AI text RPG where two local LLMs play an adventure together, driven by a custom parallel game engine. Qwen 27B is the Dungeon Master (narrative + its own character), Gemma 12B is the player character, and a second Gemma 12B slot acts as a deterministic game-system referee. All three run simultaneously each turn.

The same server also has a simpler **chat mode** (Qwen + Gemma conversing); game mode is the focus of active work.

## Commands

```bash
npm install
node server.js            # defaults to :7777
PORT=3001 node server.js  # override port
```

No build step, no test suite, no linter — the whole backend is a single `server.js` (ES modules, `"type": "module"`). Validation is done by running live games and inspecting `game.log`.

### Requires local LLM servers (OpenAI-compatible)
- **Qwen** on `127.0.0.1:8001` (GPU 0+1) — DM narrative, 1 concurrent
- **Gemma** on `127.0.0.1:8004` (GPU 2) — player + game-system, **must run with `--parallel 2`** (two concurrent slots)

Model IDs and endpoints are constants near the top of `server.js`; models can be overridden via `QWEN_MODEL` / `GEMMA_MODEL` env vars.

To use: open `http://localhost:<PORT>`, select "Game" mode, enter a scenario, click "Begin Adventure".

## Architecture

Everything backend lives in `server.js` (~1300 lines). The frontend is plain static files in `public/` (`index.html`, `app.js`, `style.css`) — no framework, no bundler. The browser talks to the server over **Server-Sent Events** (`GET /api/events`); `broadcastSSE()` is the single fan-out point to all connected clients.

State is module-level globals in `server.js` (no DB): `gameState`, `gamePhase`, `gameRound`, `pendingGemmaAction` for game mode; `messageHistory`, `conversationRunning/Paused` for chat. `POST /api/chat/start` with `{ mode: 'game' | 'chat' }` selects which loop runs — both modes share the start/stop/pause endpoints.

### The parallel game turn (the core idea)

Each round, `runGameLoop()` fires three LLM calls with `Promise.all`, then broadcasts results in a fixed order:

```
Promise.all([
  qwenDMTurn()      → prose narrative only, no JSON  (temp 1.0, creative)
  gemmaPlayerTurn() → in-character action            (temp 1.0, creative)
  gemmaSystemTurn() → recomputes state JSON          (temp 0.1, deterministic)
])
→ parseEventsFromProse() + applyEventsToState()  (rule-based, intermediate state)
→ mergeWithGameSystem(): if Gemma system output is valid, it overrides; else keep rule-based
→ SSE broadcast in order: state → DM narrative → Gemma action → events
```

Key consequences to keep in mind when editing:

1. **Qwen emits prose only.** Mechanical events (damage, healing, items, XP, level-up, death, status, location) are extracted by `parseEventsFromProse()` — regex against the *dynamic* character names from `gameState.characters.*.name`, not hardcoded. This is the first line of state truth and catches most events instantly.

2. **The Gemma game-system runs one round behind.** It receives the *prior* turn's narrative + action and reconciles state, so the rule-based parser fills the current-round gap. `mergeWithGameSystem()` prefers the system's JSON when valid, otherwise falls back to the parser's `applyEventsToState()` result.

3. **Qwen patches.** Qwen may append a ```` ```patch ```` block to correct state; `parsePatchBlock()` extracts it and `deepMerge()` folds it in (patch wins on scalar conflicts, arrays are replaced wholesale).

4. **Game phases** (`gamePhase`): `idle` → `initializing` (Qwen creates world + character in JSON mode) → character creation (`parseGemmaCharacter()` pulls name/class/race from Gemma's intro prose) → `playing` (the parallel loop) → `game_over` (HP ≤ 0 or Qwen sets the phase).

### Function map within server.js

- LLM I/O: `callLLM()` (shared by both modes), the per-role turn functions `qwenDMTurn` / `gemmaPlayerTurn` / `gemmaSystemTurn`.
- Rule engine: `parseEventsFromProse` → `applyEventsToState` → `mergeWithGameSystem`; helpers `resolveTarget`, `extractLocation`, `extractGameState`, `deepMerge`, `parsePatchBlock`.
- Loops: `runGameLoop` (game), `runConversationLoop` (chat).
- Prompts: large `*_SYSTEM_PROMPT` / `*_PROMPT` string constants near the top define each role's behavior — editing game behavior usually starts here.

## Known rough edges (from AGENTS.md)

- The rule-based parser has false positives on ambiguous prose (e.g. "heals" used non-mechanically).
- The Gemma system being one round behind means state lags the latest narrative by a turn.
- `resolveTarget` pronoun/target fallback is imperfect.
- No regression tests — parser coverage is only validated by live runs + `game.log`. Check `game.log` after a run for turn-by-turn state, parsed events, and reconciliation status.

`AGENTS.md` has additional architecture notes and the original design rationale.
