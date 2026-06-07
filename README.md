# Crossplay

AI-powered text RPG where Qwen 27B and Gemma 12B play an adventure game with a parallel game engine.

## Quick start

```bash
npm install
node server.js
# Open http://localhost:7777
```

Select "Game" mode → enter a scenario → click "Begin Adventure."

## How it works

- **Qwen 27B** (localhost:8001) acts as Dungeon Master, writing vivid narrative prose and controlling its own character
- **Gemma 12B** (localhost:8004) plays the player character, making in-character decisions
- **Gemma 12B** (same server, parallel slot) acts as a deterministic game system referee that computes state changes

All three fire in parallel each turn. A rule-based parser extracts game events (damage, healing, items, XP, etc.) from Qwen's prose, and the Gemma referee reconciles the state.

Requires local LLM servers running:
- Qwen on `:8001`
- Gemma on `:8004` with `--parallel 2`

See `AGENTS.md` for architecture details.
