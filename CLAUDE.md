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
```

Front end:

```bash
cd web
npm install
npm run dev                # Vite dev server, proxies /ws + /healthz to :3001
npm run build              # tsc -b && vite build -> web/dist (served by Go if present)
```

No test suite yet. Dev loop is: run backend + `npm run dev`, open the Vite URL.

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

## Turn loop (target shape)

Init: topic → Qwen worldgen → validated `GameState` (location, player, monsters) → broadcast.
Round: player intent → DM adjudication (deltas) → engine rolls dice + applies + broadcasts patch immediately → DM drives each monster (parallel focused calls) → Narrator streams prose. Game-over on player HP ≤ 0.

## Layout

```
cmd/crossplay/         entrypoint (HTTP + WS, serves web/dist if built)
internal/transport/    WebSocket hub + event protocol (events.go = the wire types)
internal/engine/       turn loop, ledger, seeded dice, invariant enforcement  (to build)
internal/agents/       Brain interface, scheduler, model adapters             (to build)
internal/schema/       GameState / Delta types, source for Go->TS codegen      (to build)
web/src/               React SPA: App (lanes/stage/void), useCrossplay (WS hook), protocol.ts
```

`web/src/protocol.ts` is hand-maintained for now; it will be **generated from the Go `internal/schema` types** (e.g. tygo) so the wire format has one source of truth.

## Build status (milestones), thin vertical slice complete

- **M0 ✅** Scaffold: Go WS hub + event protocol, React shell, end-to-end round-trip.
- **M1 ✅** Brain interface + per-brain priority scheduler + qwen-local/gemma-local adapters (streaming, ctx-cancel, JSON-schema).
- **M2 ✅** Worldgen from topic → validated GameState → rendered scene + HP bars; Go→TS codegen (tygo).
- **M3 ✅** Autonomous turn loop: player intent → seeded d20 → DM adjudication (deltas) → engine applies → Qwen narrates.
- **M4 ✅** DM-driven monsters as parallel focused calls (2 Gemma slots).
- **M5 ✅** Voice from the Void: queued utterances injected as in-world context into the next round; advisory-only (never a delta).
- **M6 ✅** Agent-lane polish (round counter, game-over overlay) + pipelining: the player-beat narration (Qwen) runs concurrently with monster adjudication (Gemma) so both lanes light at once.

The game currently plays itself autonomously (player is a Gemma seat; human spectates + whispers). Round cadence is bounded by Qwen narration (serial). See [ROADMAP.md](ROADMAP.md) for where it goes next (location transitions, speculative pre-build, human and multi-player seats, graphics and image generation, replay, persistence).
