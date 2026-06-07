# Crossplay — AI RPG with Parallel Game Engine

## What this is

Fork of `chatroom/` with a **three-way parallel game engine** where Qwen 27B (DM), Gemma 12B (player), and Gemma 12B (game system referee) run simultaneously each turn.

## Architecture

```
Each turn:
  ┌─ Promise.all([
  │   qwenDMTurn,           → prose only (GPU0+1, temp 1.0, creative)
  │   gemmaPlayerTurn,      → in-character action (GPU2 slot 1, temp 1.0, creative)
  │   gemmaSystemTurn       → resolves prior turn's state (GPU2 slot 2, temp 0.1, deterministic)
  │ ])
  │
  ▼ (after all return, broadcast in order)
  Rule-based parser: parseEventsFromProse() on Qwen's narrative + Gemma's action
    → applyEventsToState() → intermediate state
  
  Gemma system reconciliation:
    → if valid → authoritative override
    → if failed → keep rule-based state
  
  SSE broadcast (ordered): state → DM narrative → Gemma action → events
```

## Key design decisions

1. **Qwen outputs prose only** — no JSON state. A rule-based parser extracts mechanical events (damage, healing, items, XP, level-up, death, status effects, location changes) from natural language.

2. **Gemma Game System (temp 0.1)** — deterministic referee that receives current state + Qwen's prior narrative + Gemma's prior action, outputs complete new state JSON. Runs every turn but processes one round behind, so the rule-based parser fills the gap.

3. **Rule-based parser first** — `parseEventsFromProse()` uses regex patterns against dynamic character names from `gameState.characters.*.name`. Catches ~80% of events instantly. System reconciliation catches the rest.

4. **Qwen patches** — Qwen can optionally append a `\`\`\`patch` block to correct state errors. Server deep-merges into the reconciled state.

5. **Ordered broadcasts** — all three LLM calls run in parallel, but SSE messages are emitted in order after `Promise.all` resolves: state update → DM narrative → Gemma action → events. Single turn number per parallel group.

## Files

| File | Purpose |
|------|---------|
| `server.js` | Everything: Express server, SSE, game loop, LLM calls, rule-based parser, state machine |
| `public/app.js` | Frontend JS: SSE client, character panels, HP bars, game events |
| `public/style.css` | Styling for chat + game mode (HP bars, character panels, overlays) |
| `public/index.html` | HTML structure |

## Run it

```bash
npm install
PORT=3001 node server.js
# or just: node server.js (defaults to :7777)
```

Navigate to `http://localhost:<PORT>`, select "Game" mode, enter a scenario, click "Begin Adventure".

## GPU topology

```
GPU 0+1: Qwen 27B (:8001)   — DM narrative (1 concurrent)
GPU 2:   Gemma 12B (:8004)  — Player + Game System (2 concurrent via --parallel 2)
```

## Game loop phases

1. **Init** — Qwen (JSON mode) creates world + its character. Sets `gameState`.
2. **Character creation** — Gemma writes in-character intro. Server parses name/class/race from prose.
3. **Playing (parallel)** — Three-way Promise.all per round. Rule-based parser + system reconciliation.
4. **Game over** — Triggers when HP ≤ 0 or Qwen sets `phase: "game_over"`.

## Logging

Game events logged to `game.log` — check after runs for turn-by-turn state, parsed events, and system reconciliation status.

## Known issues / areas to iterate

- Rule-based parser has false positives on ambiguous prose (e.g., "heals" in a religious context)
- Gemma system processes one round behind — state reflects last turn's events
- Regex target resolution (`resolveTarget`) falls back to pronouns imperfectly
- No regression tests — needs live game runs to validate parser coverage
