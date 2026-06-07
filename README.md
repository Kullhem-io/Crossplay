# Crossplay

> Stop putting every model behind a help desk. Give them a room, a game, and each other.

Crossplay is an AI text RPG driven by a parallel multi-agent engine. Several local
LLMs each take a **seat** at the table — a Dungeon Master, a Narrator, and one or
more Players — and play an adventure together in a world you seed with a single
topic ("a flooded cathedral", "a Target on Black Friday").

This is **v2**, a from-scratch rewrite (Go backend + React/Vite SPA). The original
single-file Node prototype lives on the [`v1`](https://github.com/Kullhem-io/Crossplay/tree/v1) branch.

## Design in one breath

- **Mechanics first, prose second.** Outcomes are decided as validated data; narration is painted on top.
- **Code owns the ledger; the DM owns the world.** The engine is the character sheet, inventory, map, and dice; the low-temp DM model is the all-seeing referee. Accountant, not rules-lawyer.
- **Seat ≠ brain.** Players/DM/Narrator are *seats*; models or APIs are *brains*; a *binding* connects them. Adding a player (local model, Gemini API, or a human) is just a new seat + binding.
- **Voice from the Void.** When you speak, you're a disembodied voice in the world — it can startle and influence, but never breaks character or halts play.

See [CLAUDE.md](CLAUDE.md) for the full architecture.

## Run it (dev)

Backend (Go):

```bash
go run ./cmd/crossplay        # serves on :3001 (PORT to override)
```

Front end (Vite, separate terminal):

```bash
cd web && npm install && npm run dev   # proxies /ws + /healthz to :3001
```

Then open the Vite dev URL. For a single-process build, `cd web && npm run build`
produces `web/dist`, which the Go server serves automatically.

### Requires local LLM servers (OpenAI-compatible)

- **Qwen** on `127.0.0.1:8001` (serial, 1 concurrent) — Narrator / world-building
- **Gemma** on `127.0.0.1:8004` with `--parallel 2` — DM (low temp) + Player (high temp)
