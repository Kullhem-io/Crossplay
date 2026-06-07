# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Crossplay **v2**, an AI text RPG driven by a parallel multi-agent engine. Several local LLMs each take a *seat* (Dungeon Master, Narrator, Player[s]) and play an adventure together in a world seeded from a one-line human topic. This is a from-scratch rewrite; the original single-file Node prototype is preserved on the **`v1` branch** (do not build on it, `main` is the rewrite).

Stack: **Go backend** (orchestration + authoritative state) + **React/Vite TypeScript SPA** (live agent lanes + scene), talking over a single **WebSocket**.

## Commands

Go is installed at `/usr/local/go/bin` but the non-interactive tool shell does **not** source the profile, prefix Go commands with the PATH export:

```bash
export PATH="$PATH:/usr/local/go/bin:$HOME/go/bin"

go run ./cmd/crossplay      # backend on :3001 (PORT to override)
go build ./...              # compile everything
go vet ./...               # static checks
go test ./...              # engine unit tests (apply/finalize invariants)
make gen-types             # regenerate web/src/gen/schema.ts from Go schema (after editing internal/schema)
```

Front end:

```bash
cd web
npm install
npm run dev                # Vite dev server, proxies /ws + /healthz to :3001
npm run build              # tsc -b && vite build -> web/dist (served by Go if present)
```

For a single-process run, build the web once (`npm run build`) and the Go server
serves `web/dist` directly on :3001. Otherwise run the backend plus `npm run dev`
and open the Vite URL.

### Requires local LLM servers (OpenAI-compatible)
- **Qwen** on `127.0.0.1:8001`, **serial, 1 concurrent**. Narrator / world-building. Recommended temp **1.0**.
- **Gemma** on `127.0.0.1:8004` with **`--parallel 2`**, two slots: DM (temp ~0.2) + Player (temp 1.0).
- **Reasoning/thinking is OFF** on both models. Every model call must be a single, direct, schema-constrained judgment, never expect multi-step reasoning. All math/dice/bookkeeping lives in Go.

## Architecture, the core ideas

**1. Mechanics first, prose second.** The fatal flaw in v1 was extracting mechanics *out of* prose with regex. Here it's inverted: the Player states intent → the **DM** (low-temp Gemma) adjudicates it into validated structured **deltas** → the engine applies them → the **Narrator** (Qwen) paints prose on the *already-decided* outcome. Prose is always downstream of state.

**2. Code owns the ledger; the DM owns the world.** The Go engine is the authoritative state store (HP, inventory, position, dice, status timers), the "character sheet + dice + map". The DM model is the all-seeing referee but never holds the numbers. It's an *accountant, not a rules-lawyer*: it can authorize arbitrary novel outcomes (new items, status effects, world changes); the engine only enforces that the books balance (no negative counts, HP ≤ max, no unexplained teleports). Dice are seeded RNG in code → fair and replayable.

**3. Seat ≠ brain ≠ binding.** Three decoupled concepts (the key to extensibility):
- **Brain**, a model connection with a concurrency budget (`qwen-local` cap 1, `gemma-local` cap 2, `gemini-api`, `human`). A `human` brain's call just awaits UI input, so "AI plays" and "you play" share one path.
- **Seat / Agent**, a role with its **own private context, system prompt, temp** (`dm`, `narrator`, `player-1`…). Two seats on the same brain do **not** share a session, no model is ever asked to puppet multiple characters in one chat.
- **Binding**, which seat rides which brain. Adding a player = new seat + binding; the engine is unchanged.

**4. Parallelism is the point.** Hard ceiling: 1 Qwen + 2 Gemma in flight. Squeeze it via pipelining, not lockstep: Qwen pre-builds the next area while the current beat plays; the DM resolves monsters across the 2 Gemma slots while the player is idle; multiple player brains think at once. A per-brain **scheduler** (semaphore + priority queue) enforces budgets and lets player turns outrank speculative work. The UI shows this as live **agent lanes**.

**5. Voice from the Void.** The human's input is **not a seat**, it's injected as a high-priority in-world phenomenon into the DM's context. Hard guardrails (in the DM prompt *and* enforced as advisory-only in the engine): treat as a disembodied voice, never acknowledge anything meta/OOC, may startle/influence, **never** halts play, breaks character, or acts as a control command. Even "everyone dies" becomes dread, not an engine call.

**6. Honor the topic.** Fantasy-coded words (dungeon, monster) pull every world toward medieval fantasy. The prompts avoid them and append a shared `genreRule` (in `engine/game.go`) telling the models to match the topic's setting and tone and stay grounded when it is mundane. A locker-room topic yields a bully and a burst pipe, not goblins.

## Turn loop (as built)

Init: topic → Qwen worldgen (grammar-constrained JSON) → engine builds the validated `GameState` (location, player, adversaries; engine fills ids/level/hp) → broadcast → Qwen streams the opening scene → the autonomous play loop starts.

Each round:
1. Drain any Voice-from-the-Void utterances into in-world context for this round.
2. Player seat (Gemma) declares an intent.
3. Engine rolls a seeded d20, then the DM (Gemma) adjudicates intent + roll into `Adjudication` deltas.
4. Engine applies deltas (clamps, death, XP and engine-owned level-ups), broadcasts state, emits a `mechanics` event (roll + changes).
5. Pipelined: Narrator (Qwen) streams the player beat while the DM (Gemma) adjudicates the monsters concurrently; then monsters are applied and narrated.
6. `finalizeIfEnded` ends the game on player death (defeat) or last adversary down (victory); the Narrator streams a fitted closing passage.

Event types on the wire (see `internal/transport/events.go`): `hello`, `agent_status`, `action`, `mechanics`, `void`, `narration`, `state`, `log`, `error`.

## Layout

```
cmd/crossplay/         entrypoint (HTTP + WS, serves web/dist if built)
internal/transport/    WebSocket hub + event protocol (events.go = the wire types)
internal/engine/       turn loop, ledger, seeded dice, invariants, worldgen, narration, monsters, void
internal/schema/       GameState / Delta / Adjudication types, source for Go->TS codegen
internal/agents/       Brain interface, priority scheduler, OpenAI-compatible adapter
web/src/               React SPA: App (lanes/stage/void), useCrossplay (WS hook), protocol.ts, gen/schema.ts
```

The ledger types live in `internal/schema` and are the single source of truth: `make gen-types` regenerates `web/src/gen/schema.ts` from them (tygo). `web/src/protocol.ts` re-exports those and adds the wire-envelope types. After changing any `internal/schema` type, run `make gen-types`.

## Build status (milestones), thin vertical slice complete

- **M0 ✅** Scaffold: Go WS hub + event protocol, React shell, end-to-end round-trip.
- **M1 ✅** Brain interface + per-brain priority scheduler + qwen-local/gemma-local adapters (streaming, ctx-cancel, JSON-schema).
- **M2 ✅** Worldgen from topic → validated GameState → rendered scene + HP bars; Go→TS codegen (tygo).
- **M3 ✅** Autonomous turn loop: player intent → seeded d20 → DM adjudication (deltas) → engine applies → Qwen narrates.
- **M4 ✅** DM-driven monsters as parallel focused calls (2 Gemma slots).
- **M5 ✅** Voice from the Void: queued utterances injected as in-world context into the next round; advisory-only (never a delta).
- **M6 ✅** Agent-lane polish (round counter, game-over overlay) + pipelining: the player-beat narration (Qwen) runs concurrently with monster adjudication (Gemma) so both lanes light at once.

Since the slice (post-M6):
- **Game feel:** player cards show class, level, an XP bar, and a one-line description; engine-owned XP and leveling (DM awards XP, engine crosses thresholds); per-beat `mechanics` chip (d20 roll + applied changes); monster actions colored distinctly from the player.
- **Real endings:** victory (last adversary down) and defeat (player falls), each with a fitted closing passage from the Narrator and a victory/defeat overlay.
- **Topic fidelity:** the `genreRule` keeps worlds true to the topic instead of drifting to medieval fantasy.
- **Resilience + tests:** narrator streaming retries once on an early connection drop (GPU contention); first engine unit tests cover damage clamp/death, heal cap, XP level-up, finalize victory/defeat, item non-negativity.

The game plays itself autonomously (player is a Gemma seat; human spectates + whispers). Round cadence is bounded by Qwen narration (serial). See [ROADMAP.md](ROADMAP.md) for where it goes next (location transitions, speculative pre-build, human and multi-player seats, graphics and image generation, replay, persistence).
