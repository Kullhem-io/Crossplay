# Roadmap

Ideas for where Crossplay goes next, roughly grouped. Nothing here is committed
or scheduled; it is a running list we pull from. The thin vertical slice (M0 to
M6) and a first game-feel pass are already done and on `main`.

## Presentation and life

- **Graphics and minimal animation.** Pure front end, no GPU cost. React to the
  events we already emit: HP bars that lunge and flash on a hit, a dice chip that
  actually rolls, floating damage numbers, an entity card that desaturates and
  slumps on death, prose that fades in as it streams, a small shake on a crit.
  Likely a light motion layer (Framer Motion) plus CSS for the cheap parts. Could
  also tint the scene to the world's mood.
- **Image generation.** Architecturally this is just another brain the engine
  calls, run as an async job (like Qwen's speculative work), never on the per-beat
  path. Good moments: a scene image at worldgen and at each location change,
  entity portraits the first time they appear, a splash on victory or defeat.
  Constraint: a local image model (SDXL, Flux) competes for the GPU with Qwen
  (GPU 0 and 1) and Gemma (GPU 2), so generate only at scene boundaries, or use
  an external image API to avoid contention. Could get its own "Artist" lane in
  the UI.

## Engine and play

- **Location transitions.** Let a cleared room lead into the next area instead of
  ending the game on victory. This is the prerequisite for real exploration.
- **Speculative pre-build.** Once transitions exist, have Qwen generate the next
  area in the background while the current beat plays, so arriving is instant.
- **Human as a player.** The `human` brain whose call awaits UI input is already
  the planned path; wire a seat to it so a person can take a character.
- **Multiple player seats.** More than one player brain (local models, an API
  like Gemini, or a human), each its own context. Combat asks them in parallel.
- **Replay and persistence.** Store the RNG seed and the action log so a game can
  be replayed deterministically, and save or resume sessions.

## Quality

- **Ending prompt tuning.** Defeat passages are the hardest for the model to land
  without melodrama; tune that prompt.
- **More regression tests** around adjudication and the turn loop as it grows.
- **Worldgen latency.** Grammar-constrained worldgen on the 27B can be slow; cap
  tokens, stream it, or move worldgen to Gemma.
