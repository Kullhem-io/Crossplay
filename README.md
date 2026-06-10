# Crossplay

> Stop putting every model behind a help desk. Give them a room, a game, and each other.

Crossplay is an AI text RPG driven by a parallel multi-agent engine. Several local
LLMs each take a **seat** at the table, a Dungeon Master, a Narrator, and a party
of Players, and play an adventure together in a world you seed with a single
topic ("a flooded cathedral", "a Target on Black Friday", "the inside of a whale").
You can watch, whisper into the world, or pull up a chair and play a character
yourself.

This is **v2**, a from-scratch rewrite (Go backend + React/Vite SPA over one
WebSocket). The original single-file Node prototype lives on the
[`v1`](https://github.com/Kullhem-io/Crossplay/tree/v1) branch.

## Design in one breath

- **Mechanics first, prose second.** Outcomes are decided as validated data; narration is painted on top of facts that already happened.
- **Code owns the ledger; the DM owns the world.** The engine is the character sheet, inventory, dice, and map. The low-temp DM model is the all-seeing referee: an accountant, not a rules-lawyer. It can rule anything; the engine just makes sure the books balance.
- **Seat ≠ brain.** Players, DM, and Narrator are *seats*; models or APIs are *brains*; a *binding* connects them. Adding a player (a local model, the Gemini API, or a human at a keyboard) is just a new seat and a binding.
- **Parallelism is the point.** Party members declare intents simultaneously, and the Narrator streams one beat while the DM adjudicates the next. The agent lanes at the top of the UI light up as it happens.
- **Voice from the Void.** When you whisper, you are a disembodied voice inside the world. It can startle and influence, but it never breaks character and never halts play.

See [CLAUDE.md](CLAUDE.md) for the full architecture and [ROADMAP.md](ROADMAP.md) for what comes next.

## What works today

**The world honors your topic.** Qwen builds the opening scene, a party, and the
threats from whatever you type, and the prompts keep it grounded: a locker room
gives you a bully and a burst pipe, not goblins.

**The table plays itself.** Each party member is its own seat with its own
context, deciding in character and in parallel. The DM turns every attempt plus
a seeded d20 into ledger deltas; the engine applies them, enforces the
invariants (HP bounds, item counts, death, level-ups), and the Narrator
dramatizes what actually happened. Every seat sees the whole party and every
foe, so the referee can heal a teammate, foes spread their aggro, and the prose
knows who is hurt and who is carrying what.

**It feels like a game.** HP bars shift green to amber to red and the cards
flash when someone takes a hit or a heal. Rolls show up as dice chips, with a
natural 20 glowing "crit!" and a natural 1 marked "fumble". Rounds are marked in
the transcript, XP fills a bar until the engine grants a level, items sit on the
character cards, and the DM can pass gear between party members mid-story. Every
run ends for real: a victory or defeat with its own closing passage.

**You can step in.** Whisper into the void to nudge the scene from outside, or
join the party as your own character and type your actions on your turn. The
narrator writes your arrival into the story, and if you walk away the AI covers
your character so the game never stalls.

## How a round plays out

1. Any void whispers are drained and injected as an in-world phenomenon.
2. Every living party member declares an intent at once, across the Gemma slots.
3. The engine rolls a d20 per action; the DM adjudicates each into deltas; the engine applies them in order so the second action sees the first one's result.
4. While Qwen narrates the party's beat, the DM is already adjudicating the foes in parallel; then their turn is applied and narrated.
5. If the party is down or the last threat falls, the Narrator closes the story.

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
produces `web/dist`, which the Go server serves automatically on :3001.

### Requires local LLM servers (OpenAI-compatible)

- **Qwen** on `127.0.0.1:8001` (serial, 1 concurrent), Narrator and world-building
- **Gemma** on `127.0.0.1:8004` with `--parallel 3`, DM (low temp) + the player party (high temp)
