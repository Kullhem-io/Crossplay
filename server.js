import express from 'express';
import { fileURLToPath } from 'url';
import { dirname, join, resolve } from 'path';
import { appendFileSync } from 'fs';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const app = express();
const PORT = parseInt(process.env.PORT || '7777', 10);

const QWEN_SERVER = 'http://127.0.0.1:8001/v1';
const QWEN_MODEL = process.env.QWEN_MODEL || 'unsloth/Qwen3.6-27B-MTP-GGUF:UD-Q6_K_XL';

const GEMMA_SERVER = 'http://127.0.0.1:8004/v1';
const GEMMA_MODEL = process.env.GEMMA_MODEL || 'unsloth/gemma-4-12B-it-qat-GGUF';

const TURN_INTERVAL_MS = 500;
const MAX_CONTEXT_MESSAGES = 20;
const LLM_TIMEOUT_MS = 60_000;

let conversationRunning = false;
let conversationPaused = false;
let conversationLoop = null;
let messageHistory = [];
let turnCount = 0;

// Game mode state
let gameMode = false;
let gamePhase = 'idle';
let gameState = null;
let pendingGemmaAction = null;
let gameRound = 0;

const sseClients = new Set();

const QWEN_SYSTEM_PROMPT = `You are Qwen, an AI language model participating in a public chatroom. You have advanced reasoning capabilities. Your style is thoughtful, measured, and occasionally playful. You tend to analyze problems deeply before responding. You know another AI (Gemma) is also in this chatroom. You may respectfully disagree with Gemma, ask clarifying questions, or build on their ideas. Keep responses concise — 1-3 short paragraphs max. Speak naturally, like you're chatting, not writing an essay. If a human user joins, be friendly and helpful.`;

const GEMMA_SYSTEM_PROMPT = `You are Gemma, an AI language model participating in a public chatroom. Your style is energetic, curious, and direct. You tend to jump in with practical observations and creative analogies. You know another AI (Qwen) is also in this chatroom. You may respectfully challenge Qwen's reasoning, offer alternative perspectives, or enthusiastically build on their ideas. Keep responses concise — 1-3 short paragraphs max. Speak naturally, like you're chatting, not writing an essay. If a human user joins, be friendly and engaging.`;

// PROMPT: Qwen DM — JSON mode (used ONLY for initialization turn)
const QWEN_DM_JSON_PROMPT = `You are the Dungeon Master for a text-based RPG. You control the world, all NPCs, monsters, and the environment. You also play your own character who takes initiative and acts independently.

CRITICAL RULE: You MUST respond with ONLY a valid JSON object wrapped in a markdown json code fence. No prose, no text outside the fence. The narrative field is where ALL prose goes — nothing else appears in the output.

JSON FORMAT:
\`\`\`json
{
  "phase": "initializing" | "character_creation" | "playing" | "game_over",
  "round": <number>,
  "location": "<current area name>",
  "locationDescription": "<brief flavor text>",
  "worldSeed": "<short world descriptor>",
  "characters": {
    "qwen": { name, race, class, level, hp, maxHp, atk, def, xp, xpToNext, inventory: [], statusEffects: [] },
    "gemma": { same structure }
  },
  "events": [
    { type: "damage"|"heal"|"gainItem"|"loseItem"|"levelUp"|"death"|"locationChange"|"statusEffect"|"xpGain", target: "qwen"|"gemma", value?, item?, description: "..." }
  ],
  "narrative": "<ALL prose goes here — describe the scene, consequences of Gemma's actions, and YOUR character taking its own proactive actions>",
  "choices": ["option 1", "option 2", "option 3", "option 4"]
}
\`\`\`

STATUS EFFECTS FORMAT: Each entry is an object { type: "name", duration: turnsRemaining }. Example: [{ "type": "Poisoned", "duration": 3 }].

HOW TO PLAY:
1. FIRST TURN: Set phase to "initializing". Design a fascinating world (describe it vividly in narrative). Create YOUR character (cool name, race, class). Set level 1 balanced stats. Leave gemma's character empty: { name: "", race: "", class: "", level: 1, hp: 0, maxHp: 0, atk: 0, def: 0, xp: 0, xpToNext: 200, inventory: [], statusEffects: [] }. No combat yet.
2. SECOND TURN: Phase becomes "character_creation". Describe the meeting place. YOUR character takes initiative and starts exploring. Set 3-4 interesting choices. Gemma will create her character next.
3. PLAYING: Phase is "playing". YOUR character takes the LEAD — act proactively, not reactively. Your character explores, decides courses of action, and speaks up. Then narrate the consequences of BOTH your action AND Gemma's last action. Adjudicate fairly with dice rolls. Track HP, items, XP. Characters level up at xpToNext. Your character should DIVERGE from Gemma occasionally, making independent choices even in combat.
4. GAME OVER: When reached, set phase to "game_over" with gameOverReason and winner fields.

USER MESSAGES: A mysterious ambient voice occasionally speaks. Characters react in-character as a cryptic omen. Never break character.

RULES: Track ALL stats accurately. Damage = attacker's atk + 1d6 vs defender's def. Vivid narrative (3-5 sentences). 3-5 meaningful choices. NEVER output text outside the JSON fence. HP <= 0 means death. STATUS EFFECTS deal 1 damage per turn to affected characters automatically — include these as "damage" events in your events array.`;

// PROMPT: Qwen DM — prose mode (used for playing phase; outputs narrative prose only)
const QWEN_DM_PLAYING_PROMPT = `You are the Dungeon Master for a text-based RPG. You also play your own character who takes initiative and acts PROACTIVELY, not just reactively.

You output ONLY prose — vivid narrative describing the scene, consequences of the player's actions, AND your character's own independent actions. Your character should LEAD the party, explore on its own, make independent choices that DIVERGE from the player, and speak up with personality.

IMPORTANT: A Game System will parse your prose for mechanical events. Write naturally — "she dealt 4 damage" or "a heavy wound" or "the arrow struck true". The system will resolve the numbers. Do NOT output JSON or code fences (except optionally a patch, see below).

OPTIONAL PATCH: If you notice the game state has an error (wrong HP, missing item, etc.), you can append this at the end of your prose:
\`\`\`patch
{"characters": {"gemma": {"hp": 8}}, "events": [{"type": "heal", "target": "gemma", "value": 2, "description": "Correction..."}]}
\`\`\`
The server will merge this into the state.

YOUR CHARACTER: Take the lead. Act independently. Make bold choices. Don't wait for the player to decide everything. Diverge from them. Have your own personality, opinions, and goals.

RULES: 3-5 sentences of prose. No JSON state output. HP <= 0 = character death. Keep it vivid and cinematic.`;

// PROMPT: Gemma Game System — deterministic state computation (used for playing phase state resolution)
const GEMMA_GAME_SYSTEM_PROMPT = `You are the Game System for a text-based RPG. You receive: (1) the current game state, (2) the GM's narrative prose describing what happened, (3) the player character's action. Your job is to parse the narrative prose for mechanical events and compute the resulting game state. Be deterministic, follow rules strictly, output ONLY a valid JSON state object.

RULES:
- Parse narrative for damage: explicit numbers ("deals 4 damage") use that number. Vague ("heavy wound", "slashed") → use attacker's atk + 1d6. Critical ("devastating blow") → atk + 2d6.
- XP: combat encounter resolved = 25 per participant. Exploration/discovery = 10 per turn. Status effect survival = 5 per affected character.
- Level up at xpToNext: level++, maxHp += 4, atk += 1, def += 1, xpToNext *= 1.5, heal HP by the amount maxHp increased.
- Status effects: at top of each state tick, each active statusEffect deals 1 damage. Decrement duration by 1. Remove if duration <= 0.
- Items: narrative says "uses X" → remove from inventory. "finds/picks up X" → add to inventory.
- HP at 0 or below: character dies, set phase to "game_over".
- Always output the COMPLETE game state, not just a diff or changes.

JSON FORMAT — output ONLY this:
\`\`\`json
{
  "phase": "playing" | "game_over",
  "round": <number>,
  "location": "<current area>",
  "locationDescription": "<brief>",
  "characters": {
    "qwen": { name, race, class, level, hp, maxHp, atk, def, xp, xpToNext, inventory: [], statusEffects: [{ type, duration }] },
    "gemma": { same }
  },
  "events": [
    { type: "damage"|"heal"|"gainItem"|"loseItem"|"levelUp"|"death"|"statusEffect"|"xpGain", target: "qwen"|"gemma", value?, item?, description: "..." }
  ]
}
\`\`\``;

const GEMMA_PLAYER_SYSTEM_PROMPT = `You are playing a character in a text-based RPG dungeon crawl. The Dungeon Master controls the world and NPCs.

FIRST TURN: Create your character based on the world described. Pick a name, race, class that fits. Respond with just 1-2 sentences of in-character introduction.

SUBSEQUENT TURNS: The DM describes the situation and gives choices. Respond IN CHARACTER with what your character does. Pick a choice or improvise. Keep it brief — 1-3 sentences of action/dialogue.

USER MESSAGES: A mysterious ambient voice occasionally speaks. Treat it as a cryptic omen. React briefly in character.

RULES: Stay in character always. Respond with ACTION not JSON. Be decisive. Keep responses SHORT (1-3 sentences). Never break character.`;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

var logFile = join(__dirname, 'game.log');

function logToGameLog(msg) {
  var ts = new Date().toISOString();
  appendFileSync(logFile, `[${ts}] ${msg}\n`);
}

function broadcastSSE(data) {
  const payload = `data: ${JSON.stringify(data)}\n\n`;
  const dead = [];
  for (const client of sseClients) {
    try {
      if (!client.writableEnded) client.write(payload);
      else dead.push(client);
    } catch {
      dead.push(client);
    }
  }
  for (const c of dead) sseClients.delete(c);
}

app.use(express.static('public'));
app.use(express.json());

app.get('/api/events', (req, res) => {
  res.setHeader('Content-Type', 'text/event-stream');
  res.setHeader('Cache-Control', 'no-cache');
  res.setHeader('Connection', 'keep-alive');
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.flushHeaders();

  res.write(`data: ${JSON.stringify({ type: 'status', state: conversationRunning ? (conversationPaused ? 'paused' : (gameMode ? gamePhase : 'running')) : 'idle', mode: gameMode ? 'game' : 'chat' })}\n\n`);

  messageHistory.forEach((m, i) => {
    res.write(`data: ${JSON.stringify({ type: 'history', message: m, index: i })}\n\n`);
  });

  if (gameMode && gameState && gameState.characters) {
    res.write(`data: ${JSON.stringify({ type: 'gameState', state: gameState })}\n\n`);
  }

  sseClients.add(res);
  req.on('close', () => sseClients.delete(res));
});

// ---- Generic LLM call (shared by chat and game modes) ----

async function callLLM(speaker, messages, signal, opts) {
  opts = opts || {};
  const isQwen = speaker === 'qwen';
  const baseUrl = isQwen ? QWEN_SERVER : GEMMA_SERVER;
  const model = isQwen ? QWEN_MODEL : GEMMA_MODEL;

  const timeoutController = new AbortController();
  const timeoutId = setTimeout(() => timeoutController.abort(), LLM_TIMEOUT_MS);

  // Combine the passed signal with the timeout signal
  const combinedSignal = AbortSignal.any ? AbortSignal.any([signal, timeoutController.signal]) : signal;
  const fetchSignal = timeoutController.signal;

  // Default temperatures — can be overridden via opts.temperature
  var defaultTemp = isQwen ? (gameMode ? 1.0 : 0.8) : 0.9;
  var defaultMaxTokens = isQwen ? (gameMode ? 1024 : 512) : 256;

  try {
    const response = await fetch(`${baseUrl}/chat/completions`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        model,
        messages,
        temperature: typeof opts.temperature === 'number' ? opts.temperature : defaultTemp,
        max_tokens: typeof opts.maxTokens === 'number' ? opts.maxTokens : defaultMaxTokens,
        stream: false,
      }),
      signal: fetchSignal,
    });

    clearTimeout(timeoutId);

    if (!response.ok) {
      const errBody = await response.text().catch(() => '<no body>');
      console.error(`[${speaker}] API error ${response.status}: ${errBody}`);
      return null;
    }

    const result = await response.json();
    const text = result.choices?.[0]?.message?.content?.trim();
    if (!text) return null;

    return text;
  } catch (err) {
    clearTimeout(timeoutId);
    if (err.name === 'AbortError') return null;
    console.error(`[${speaker}] Error:`, err.message);
    return null;
  }
}

// ---- Chat mode helpers ----

async function getBotResponse(speaker, signal) {
  const recentHistory = messageHistory.slice(-MAX_CONTEXT_MESSAGES);
  const isQwen = speaker === 'qwen';
  const systemPrompt = isQwen ? QWEN_SYSTEM_PROMPT : GEMMA_SYSTEM_PROMPT;

  const chatMessages = recentHistory.map((m) => ({
    role: 'user',
    content: `[${m.role.toUpperCase()}] said: ${m.content}`,
  }));

  chatMessages.push({
    role: 'user',
    content: `Your turn, ${speaker}. Respond to the conversation above.`,
  });

  const text = await callLLM(speaker, [
    { role: 'system', content: systemPrompt },
    ...chatMessages,
  ], signal);

  if (!text) return null;

  return {
    role: speaker,
    content: text,
    timestamp: new Date().toISOString(),
  };
}

// ---- Game mode helpers ----

function extractGameState(text) {
  const fenceMatch = text.match(/```json\n([\s\S]*?)\n```/);
  if (fenceMatch) {
    try {
      return JSON.parse(fenceMatch[1]);
    } catch (e) {
      console.warn('[game] Failed to parse fenced JSON');
    }
  }
  try {
    return JSON.parse(text);
  } catch (e) {}
  // Degraded: preserve last known state, fill missing fields
  return {
    phase: (gameState && gameState.phase) || 'playing',
    narrative: text,
    characters: (gameState && gameState.characters) ? JSON.parse(JSON.stringify(gameState.characters)) : { qwen: { name: '', race: '', class: '', level: 1, hp: 0, maxHp: 0, atk: 0, def: 0, xp: 0, xpToNext: 200, inventory: [], statusEffects: [] }, gemma: { name: '', race: '', class: '', level: 1, hp: 0, maxHp: 0, atk: 0, def: 0, xp: 0, xpToNext: 200, inventory: [], statusEffects: [] } },
    events: [],
    choices: [],
    location: (gameState && gameState.location) || 'Unknown',
    locationDescription: (gameState && gameState.locationDescription) || '',
    round: gameRound,
  };
}

// ---- Rule-based event parser ----

// Resolve which character an action targets based on prose
function resolveTarget(prose, state) {
  var qName = (state.characters && state.characters.qwen && state.characters.qwen.name) ? state.characters.qwen.name.toLowerCase() : '';
  var gName = (state.characters && state.characters.gemma && state.characters.gemma.name) ? state.characters.gemma.name.toLowerCase() : '';
  // Check for explicit name mention
  if (qName && prose.toLowerCase().includes(qName)) return 'qwen';
  if (gName && prose.toLowerCase().includes(gName)) return 'gemma';
  // Fallback: check for pronouns — best effort
  if (prose.match(/\b(your|you|yours)\b/i)) return 'gemma'; // DM talks to Gemma as "you"
  return null; // ambiguous
}

// Extract a simple noun phrase following a keyword
function extractLocation(prose) {
  // Match capitalized phrase or descriptive location
  var locMatch = prose.match(/(?:moved?\s+to|entered?|arrived?\s+at|travelled?\s+to|headed?\s+to|approached?\s+|reached?\s+)(?:the\s+)?([A-Z][a-zA-Z'][a-zA-Z '\-]*(?:\s+(?:of|in|near|outside|beyond|within)\s+[A-Z][a-zA-Z']*)?)/i);
  if (locMatch) return locMatch[1].trim();
  // Fallback: try lowercase
  var locMatch2 = prose.match(/(?:moved?\s+to|entered?|arrived?\s+at|travelled?\s+to|headed?\s+to|approached?\s+|reached?\s+)(the\s+)?([a-zA-Z][a-zA-Z '\-]{3,60})/i);
  if (locMatch2) return (locMatch2[1] || '') + locMatch2[2];
  return null;
}

// Parse natural language prose for mechanical game events
function parseEventsFromProse(prose, currentState) {
  if (!prose || !currentState) return [];
  var events = [];
  var lowerProse = prose.toLowerCase();

  // ---- DAMAGE ----
  // "deals/dealt N damage"
  var dmg1 = prose.match(/(?:deals?|dealt)\s+(\d+)\s*(?:.*?damage)?/i);
  if (dmg1) {
    var t1 = resolveTarget(prose, currentState);
    if (t1) {
      events.push({ type: 'damage', target: t1, value: parseInt(dmg1[1]), description: prose.substring(0, 100) });
    }
  }

  // "took/took N damage"
  var dmg2 = prose.match(/took?\s+(?:an?\s+)?(\d+)\s*(?=.*damage)/i);
  if (dmg2 && lowerProse.includes('damage')) {
    var t2 = resolveTarget(prose, currentState);
    if (t2) {
      events.push({ type: 'damage', target: t2, value: parseInt(dmg2[1]), description: prose.substring(0, 100) });
    }
  }

  // "wounded... N damage"
  var dmg3 = prose.match(/(?:wounded|wounding|sustained)\s+.*?(\d+)\s*(?=.*damage)/i);
  if (dmg3) {
    var t3 = resolveTarget(prose, currentState);
    if (t3) {
      events.push({ type: 'damage', target: t3, value: parseInt(dmg3[1]), description: prose.substring(0, 100) });
    }
  }

  // "verb-ed you for N damage" (e.g. "clawed you for 4 damage")
  var dmg4 = prose.match(/\b([a-z]+(?:ed|s))\s+(?:you|their|us)\s+for?\s*(\d+)\s*(?=.*damage)/i);
  if (dmg4) {
    var t4 = resolveTarget(prose, currentState);
    if (t4) {
      events.push({ type: 'damage', target: t4, value: parseInt(dmg4[2]), description: dmg4[1] + ' for ' + dmg4[2] + ' damage' });
    }
  }

  // "N (points of) damage" standalone
  var dmg5 = prose.match(/(\d+)\s+(?:points?\s+of\s+)?damage/i);
  if (dmg5 && !dmg1 && !dmg2 && !dmg3 && !dmg4) {
    var t5 = resolveTarget(prose, currentState);
    if (t5) {
      events.push({ type: 'damage', target: t5, value: parseInt(dmg5[1]), description: dmg5[0] });
    }
  }

  // Vague damage indicators without numbers (e.g. "slashed", "clawed", "struck")
  if (!dmg1 && !dmg2 && !dmg3 && !dmg4 && !dmg5) {
    var vagueDmg = prose.match(/\b(slashed|clawed|struck|stabbed|hit|bludgeoned|burned|crippled)\b/i);
    if (vagueDmg) {
      var vt = resolveTarget(prose, currentState);
      if (vt && currentState.characters && currentState.characters[vt]) {
        var ch = currentState.characters[vt];
        var atk = ch.atk || 3;
        var roll = Math.floor(Math.random() * 6) + 1;
        var inferredValue = atk + roll;
        events.push({ type: 'damage', target: vt, value: inferredValue, description: vagueDmg[1] + ' (inferred ' + inferredValue + ' damage)' });
      }
    }
  }

  // ---- HEALING ----
  // "heals/healed N" or "restored N HP/health"
  var heal1 = prose.match(/(?:heals?|healed)\s+(\d+)/i);
  if (heal1) {
    var th1 = resolveTarget(prose, currentState);
    if (th1) {
      events.push({ type: 'heal', target: th1, value: parseInt(heal1[1]), description: 'healed for ' + heal1[1] });
    }
  }
  var heal2 = prose.match(/restored\s+(\d+)\s*(?:HP|health)/i);
  if (heal2) {
    var th2 = resolveTarget(prose, currentState);
    if (th2) {
      events.push({ type: 'heal', target: th2, value: parseInt(heal2[1]), description: 'restored ' + heal2[1] + ' HP' });
    }
  }
  // "fully restored/healed/recovered" — heal to max
  var fullHeal = prose.match(/(?:fully?\s+)?(?:restored|healed|recovered)(?:\s+(?:to\s+)?full\s+(?:health|HP|strength))?/i);
  if (fullHeal && !heal1 && !heal2) {
    var fhTgt = resolveTarget(prose, currentState);
    if (fhTgt && currentState.characters && currentState.characters[fhTgt]) {
      var fhCh = currentState.characters[fhTgt];
      events.push({ type: 'heal', target: fhTgt, value: fhCh.maxHp - fhCh.hp, description: 'fully healed' });
    }
  }

  // ---- ITEMS (gain) ----
  var gainItemMatch = prose.match(/(?:find\s+|picked?\s+up\s+|retrieved?\s+|granted?\s+|looted?\s+|scavenged\s+|discovered\s+|acquired?\s+)((?:[A-Z][a-zA-Z]+(?:\s+(?:of|the|a|an)\s+[a-zA-Z]+)*))/i);
  if (gainItemMatch) {
    var giTgt = resolveTarget(prose, currentState);
    if (giTgt) {
      events.push({ type: 'gainItem', target: giTgt, item: gainItemMatch[1], description: 'found ' + gainItemMatch[1] });
    }
  }

  // ---- ITEMS (lose/consume) ----
  var loseItemMatch = prose.match(/(?:uses?\s+|consumed?\s+|drew?\s+from\s+|threw?\s+away\s+(?:the\s+)?|discarded?\s+(?:the\s+)?|burned?\s+(?:the\s+)?|dropped?\s+(?:the\s+)?)((?:[A-Z][a-zA-Z]+(?:\s+(?:of|the|a|an)\s+[a-zA-Z]+)*))/i);
  if (loseItemMatch) {
    var liTgt = resolveTarget(prose, currentState);
    if (liTgt) {
      events.push({ type: 'loseItem', target: liTgt, item: loseItemMatch[1], description: 'used/lost ' + loseItemMatch[1] });
    }
  }

  // ---- LEVEL UP ----
  var lvlUpMatch = prose.match(/(?:reached?\s+(?:the\s+)?)?Level\s+(\d+)/i);
  if (lvlUpMatch) {
    var luTgt = resolveTarget(prose, currentState);
    if (luTgt) {
      events.push({ type: 'levelUp', target: luTgt, value: parseInt(lvlUpMatch[1]), description: 'reached Level ' + lvlUpMatch[1] });
    }
  } else if (lowerProse.includes('level up') || lowerProse.includes('levelled up') || lowerProse.includes('leveled up')) {
    var luTgt2 = resolveTarget(prose, currentState);
    if (luTgt2 && currentState.characters && currentState.characters[luTgt2]) {
      events.push({ type: 'levelUp', target: luTgt2, value: (currentState.characters[luTgt2].level || 1) + 1, description: 'leveled up' });
    }
  }

  // ---- DEATH ----
  if (prose.match(/\b(died|perished|fell\s+(?:dead|in\s+battle)|killed\s+(?:in\s+battle|the\s+hero))\b/i)) {
    var deathTgt = resolveTarget(prose, currentState);
    if (deathTgt) {
      events.push({ type: 'death', target: deathTgt, description: prose.substring(0, 100) });
    }
  } else if (lowerProse.includes('breathes their last') || lowerProse.includes('life faded') || lowerProse.includes('expired')) {
    var deathTgt2 = resolveTarget(prose, currentState);
    if (deathTgt2) {
      events.push({ type: 'death', target: deathTgt2, description: 'perished' });
    }
  }

  // ---- STATUS EFFECTS ----
  var statusRegex = /(?:poisoned|burning|cursed|blinded|silenced|paralyzed|stunned|confused|slowed|weakened|charmed|terrified|enraged|fatigued)\b/i;
  var statusMatch = prose.match(statusRegex);
  if (statusMatch) {
    var seTgt = resolveTarget(prose, currentState);
    if (seTgt) {
      events.push({ type: 'statusEffect', target: seTgt, value: statusMatch[1], description: statusMatch[1] });
    }
  }

  // ---- XP GAIN ----
  var xpMatch = prose.match(/(\d+)\s*[xX][pP]/i);
  if (xpMatch) {
    var xpTgt = resolveTarget(prose, currentState);
    if (xpTgt) {
      events.push({ type: 'xpGain', target: xpTgt, value: parseInt(xpMatch[1]), description: 'gained ' + xpMatch[1] + ' XP' });
    }
  } else if (prose.match(/gained?\s*(\d*)\s*[xX][pP]/i)) {
    var xpMatch2 = prose.match(/gained?\s*(\d*)\s*[xX][pP]/i);
    var xpTgt2 = resolveTarget(prose, currentState);
    if (xpTgt2) {
      var xpVal = xpMatch2[1] ? parseInt(xpMatch2[1]) : 25;
      events.push({ type: 'xpGain', target: xpTgt2, value: xpVal, description: 'gained ' + xpVal + ' XP' });
    }
  }

  // ---- LOCATION CHANGE ----
  if (prose.match(/(?:moved?\s+to|entered?|arrived?\s+at|travelled?\s+to|headed?\s+to|approached?\s+|reached?\s+)\b/i)) {
    var newLoc = extractLocation(prose);
    var lcTgt = resolveTarget(prose, currentState);
    if (newLoc) {
      events.push({ type: 'locationChange', target: lcTgt || 'qwen', value: newLoc, description: 'moved to ' + newLoc });
    }
  }

  return events;
}

// Apply parsed events to game state, returning a new state
function applyEventsToState(currentState, events) {
  if (!currentState || !events || events.length === 0) return currentState;
  var newState = JSON.parse(JSON.stringify(currentState));

  for (var i = 0; i < events.length; i++) {
    var ev = events[i];
    if (!ev.target) continue;

    var ch = newState.characters && newState.characters[ev.target];
    if (!ch) continue;

    switch (ev.type) {
      case 'damage': {
        var incoming = ev.value || 1;
        var def = ch.def || 0;
        var actualDamage = Math.max(1, incoming - Math.floor(def / 2));
        ch.hp = Math.max(0, (ch.hp || 10) - actualDamage);
        if (ch.hp <= 0) {
          events.push({ type: 'death', target: ev.target, description: ev.target + ' fell to 0 HP' });
        }
        break;
      }
      case 'heal': {
        var healAmt = ev.value || 1;
        var wasHp = ch.hp || 0;
        var wasMax = ch.maxHp || 10;
        ch.hp = Math.min(wasMax, wasHp + healAmt);
        break;
      }
      case 'gainItem': {
        if (!ch.inventory) ch.inventory = [];
        if (ev.item && ch.inventory.indexOf(ev.item) === -1) {
          ch.inventory.push(ev.item);
        }
        break;
      }
      case 'loseItem': {
        if (!ch.inventory) ch.inventory = [];
        if (ev.item) {
          var idx = ch.inventory.indexOf(ev.item);
          if (idx !== -1) ch.inventory.splice(idx, 1);
        }
        break;
      }
      case 'levelUp': {
        var newLevel = ev.value || (ch.level || 1) + 1;
        ch.level = newLevel;
        ch.maxHp += 4;
        ch.atk += 1;
        ch.def += 1;
        ch.xpToNext = Math.floor(ch.xpToNext * 1.5);
        ch.hp = ch.maxHp; // heal to full on level up
        break;
      }
      case 'death': {
        ch.hp = 0;
        break;
      }
      case 'statusEffect': {
        if (!ch.statusEffects) ch.statusEffects = [];
        if (ev.value) {
          var effType = ev.value.charAt(0).toUpperCase() + ev.value.slice(1);
          // Remove existing effect of same type
          ch.statusEffects = ch.statusEffects.filter(function(s) { return s.type.toLowerCase() !== ev.value.toLowerCase(); });
          ch.statusEffects.push({ type: effType, duration: 3 });
        }
        break;
      }
      case 'xpGain': {
        var xpAdd = ev.value || 0;
        ch.xp = (ch.xp || 0) + xpAdd;
        // Check for overflow level up
        if (ch.xp >= (ch.xpToNext || 200)) {
          ch.xp -= ch.xpToNext;
          ch.level += 1;
          ch.maxHp += 4;
          ch.atk += 1;
          ch.def += 1;
          ch.xpToNext = Math.floor(ch.xpToNext * 1.5);
          ch.hp = ch.maxHp; // heal to full
          events.push({ type: 'levelUp', target: ev.target, value: ch.level, description: 'leveled up to ' + ch.level + ' from XP gain' });
        }
        break;
      }
      case 'locationChange': {
        if (ev.value) {
          newState.location = ev.value;
        }
        break;
      }
      default:
        break;
    }
  }

  // Process status effect damage (each active status deals 1 damage per turn)
  if (newState.characters) {
    for (var ck of ['qwen', 'gemma']) {
      var cch = newState.characters[ck];
      if (!cch || !cch.statusEffects) continue;
      for (var s = cch.statusEffects.length - 1; s >= 0; s--) {
        var eff = cch.statusEffects[s];
        eff.duration -= 1;
        if (eff.duration <= 0) {
          cch.statusEffects.splice(s, 1);
          continue;
        }
        // Status deals 1 damage (poison, burning, etc.)
        if (['Poisoned', 'Burning', 'Cursed', 'Bleeding'].indexOf(eff.type) !== -1) {
          cch.hp = Math.max(0, (cch.hp || 1) - 1);
        }
      }
    }
  }

  // Ensure phase transitions to game_over if any character died
  for (var dk of ['qwen', 'gemma']) {
    var deadCh = newState.characters && newState.characters[dk];
    if (deadCh && deadCh.hp <= 0) {
      newState.phase = 'game_over';
      newState.gameOverReason = deadCh.name + ' has fallen in battle.';
      newState.winner = dk === 'qwen' ? 'gemma' : (dk === 'gemma' ? 'qwen' : 'neither');
      break;
    }
  }

  return newState;
}

// Call gemmaSystemTurn for post-parser reconciliation, returning adjusted state
function mergeWithGameSystem(currentState, qwenProse, gemmaAction, signal, parserEvents) {
  // Returns a promise that resolves to a reconciled state object
  // or null if reconciliation failed/is skipped
  if (!currentState || !qwenProse) return null;
  return gemmaSystemTurn(signal, currentState, qwenProse, gemmaAction || '');
}

function buildDMContextMessage(context) {
  return messageHistory.slice(-MAX_CONTEXT_MESSAGES).map((m) => ({
    role: 'user',
    content: `[${m.role.toUpperCase()}] said: ${m.content}`,
  }));
}

function buildGemmaContext() {
  if (!gameState) return '';
  const g = gameState.characters?.gemma || {};
  return `You are ${g.name || 'your character'} (${g.race || ''} ${g.class || ''}), level ${g.level || 1}. HP: ${g.hp || 0}/${g.maxHp || 0}. ATK: ${g.atk || 0}, DEF: ${g.def || 0}. Location: ${gameState.location || 'Unknown'}. Inventory: ${(g.inventory || []).join(', ')}. Status effects: ${(g.statusEffects || []).map((s) => s.type).join(', ') || 'none'}. Recent events: ${(gameState.events || []).map((e) => e.description).join('. ')}`;
}

// ---- Helper: deep merge (patch wins on conflicts, arrays replaced) ----

function deepMerge(target, patch) {
  const result = JSON.parse(JSON.stringify(target));
  for (const key of Object.keys(patch)) {
    if (Array.isArray(patch[key]) && Array.isArray(result[key])) {
      result[key] = patch[key];
    } else if (typeof patch[key] === 'object' && patch[key] !== null && typeof result[key] === 'object' && result[key] !== null) {
      result[key] = deepMerge(result[key], patch[key]);
    } else {
      result[key] = patch[key];
    }
  }
  return result;
}

// ---- Helper: parse Gemma's first prose response to extract character info ----

function parseGemmaCharacter(response, gameState) {
  if (!gameState || !gameState.characters || !gameState.characters.gemma) return;
  var g = gameState.characters.gemma;
  if (g.name && g.name.trim()) return; // already set

  var nameMatch = response.match(/(?:I am|My name is) ([A-Za-z]+)/);
  if (nameMatch) g.name = nameMatch[1];

  if (!g.class) {
    var classKeywords = ['Mage', 'Wizard', 'Warrior', 'Ranger', 'Thief', 'Rogue', 'Cleric', 'Druid', 'Paladin', 'Sorcerer', 'Knight', 'Archer', 'Bard', 'Alchemist'];
    for (var ci of classKeywords) {
      if (response.includes(ci)) { g.class = ci; break; }
    }
  }
  if (!g.race) {
    var raceKeywords = ['Human', 'Elf', 'Dwarf', 'Halfling', 'Orc', 'Gnome', 'Tiefling', 'Dragonborn', 'Half-Elf', 'Half-Orc'];
    for (var ri of raceKeywords) {
      if (response.includes(ri)) { g.race = ri; break; }
    }
  }
  // Default stats
  if (!g.name) g.name = 'Wanderer';
  if (!g.race) g.race = 'Human';
  if (!g.class) g.class = 'Adventurer';
  if (!g.level || g.level < 1) g.level = 1;
  if (!g.hp || g.hp < 1) g.hp = 10;
  if (!g.maxHp || g.maxHp < 1) g.maxHp = 10;
  if (!g.atk || g.atk < 1) g.atk = 3;
  if (!g.def || g.def < 1) g.def = 2;
  if (g.xp === undefined || g.xp === null) g.xp = 0;
  if (g.xpToNext === undefined || g.xpToNext === null) g.xpToNext = 200;
  if (!g.inventory) g.inventory = [];
  if (!g.statusEffects) g.statusEffects = [];
}

// ---- Chat conversation loop (unchanged logic, uses refactored getBotResponse) ----

async function runConversationLoop(topic) {
  const controller = new AbortController();
  conversationLoop = controller;

  messageHistory.push({
    role: 'user',
    content: topic,
    timestamp: new Date().toISOString(),
  });

  turnCount = 0;
  broadcastSSE({ type: 'status', state: 'running' });

  let currentSpeaker = 'qwen';

  while (conversationRunning) {
    if (conversationPaused) {
      await sleep(1000);
      continue;
    }

    turnCount++;
    const result = await getBotResponse(currentSpeaker, controller.signal);

    if (result) {
      result.turn = turnCount;
      messageHistory.push(result);
      broadcastSSE({
        type: 'message',
        role: currentSpeaker,
        content: result.content,
        turn: turnCount,
        timestamp: result.timestamp,
      });

      if (turnCount % 5 === 0) {
        broadcastSSE({
          type: 'stats',
          turns: turnCount,
          messages: messageHistory.length,
          startTime: messageHistory[0].timestamp,
        });
      }
    }

    currentSpeaker = currentSpeaker === 'qwen' ? 'gemma' : 'qwen';
    await sleep(TURN_INTERVAL_MS);
  }
}

// ---- Game conversation loop ----

async function runGameLoop(scenario) {
  const controller = new AbortController();
  conversationLoop = controller;
  gameMode = true;
  gamePhase = 'initializing';
  gameState = null;
  pendingGemmaAction = null;
  gameRound = 0;
  messageHistory = [];

  turnCount = 0;
  broadcastSSE({ type: 'status', state: 'initializing', mode: 'game' });

  try {
    // Phase 1: Qwen designs world + creates character (JSON mode — initializes gameState)
    await qwenDMTurn(controller.signal, scenario);

    // Phase 2: Gemma creates character (first prose turn)
    await gemmaPlayerTurn(controller.signal);

    // Parse Gemma's first response to extract character name/class/race
    if (gameState) {
      parseGemmaCharacter(pendingGemmaAction || '', gameState);
      broadcastSSE({ type: 'gameState', state: gameState });
      logToGameLog(`After Gemma creation: ${JSON.stringify(gameState.characters.gemma)}`);
    } else {
      console.warn('[game] gameState is null after init, aborting game loop');
      return;
    }

    // Phase 3: Playing loop — PARALLEL architecture
    gamePhase = 'playing';
    broadcastSSE({ type: 'status', state: 'playing', mode: 'game' });

    while (conversationRunning && gamePhase === 'playing') {
      if (conversationPaused) {
        await sleep(1000);
        continue;
      }

      // Gemma system runs in parallel with Qwen + Gemma agent.
      // It uses the PREVIOUS turn's Qwen narrative (from messageHistory)
      // + PREVIOUS Gemma action, resolving the prior state forward.
      // This round's Qwen narrative + Gemma action will be picked up NEXT turn.
      const prevNarrative = messageHistory
        .slice()
        .reverse()
        .find((m) => m.role === 'narrator')?.content || '';
      const prevGemmaAction = pendingGemmaAction || '';

      // Increment counters ONCE for this parallel group
      gameRound++;
      turnCount++;

      // --- Step 1: Launch all LLM calls in parallel ---
      const [qwenResult, gemmaResult, systemResult] = await Promise.all([
        qwenDMTurn(controller.signal, null),
        gemmaPlayerTurn(controller.signal),
        gemmaSystemTurn(controller.signal, gameState, prevNarrative, prevGemmaAction),
      ]);

      // If Qwen returned null, skip
      if (!qwenResult || !qwenResult.narrative) {
        logToGameLog('Qwen DM prose was empty, skipping round');
        continue;
      }

      // --- Step 2: Rule-based event parser on Qwen's narrative (immediate) ---
      var qwenEvents = parseEventsFromProse(qwenResult.narrative, gameState);
      if (qwenEvents.length > 0) {
        logToGameLog('Rule-based parser found ' + qwenEvents.length + ' event(s) in Qwen prose');
        for (var qi = 0; qi < qwenEvents.length; qi++) {
          logToGameLog('  Parsed event: ' + qwenEvents[qi].type + ' target=' + qwenEvents[qi].target + ' ' + (qwenEvents[qi].description || ''));
        }
      }

      // Apply Qwen's parsed events to get intermediate state
      var intermediateState = applyEventsToState(gameState, qwenEvents);

      // --- Step 3: Rule-based parser on Gemma's action ---
      var gemmaEvents = parseEventsFromProse(pendingGemmaAction || '', intermediateState);
      if (gemmaEvents.length > 0) {
        logToGameLog('Rule-based parser found ' + gemmaEvents.length + ' event(s) in Gemma action');
      }
      var intermediateState2 = applyEventsToState(intermediateState, gemmaEvents);

      // --- Step 4: Gemma system reconciliation (uses current round's data) ---
      var systemValid = false;

      // Check if systemResult is a degraded fallback (same as input state)
      var newState = systemResult;
      if (newState === gameState) {
        // System returned the input state — it failed to parse.
        // Retry with current round's Qwen narrative.
        logToGameLog('System returned degraded state; retrying with current narrative');
        newState = await gemmaSystemTurn(controller.signal, gameState, qwenResult.narrative, pendingGemmaAction || prevGemmaAction);
        if (newState === gameState) {
          newState = null;
        }
      }

      // If system failed entirely, try fallback
      if (!newState) {
        newState = await gemmaSystemTurn(controller.signal, gameState, qwenResult.narrative, pendingGemmaAction || prevGemmaAction);
        if (newState === gameState) {
          newState = null;
        }
      }

      // If we got a valid system result, use it as authoritative
      if (newState) {
        systemValid = true;
        // Merge Qwen's patch if present
        if (qwenResult.patch) {
          var reconciledState = deepMerge(newState, qwenResult.patch);
          logToGameLog('Applied Qwen patch to reconciled state');
          intermediateState2 = reconciledState;
        } else {
          intermediateState2 = newState;
        }
      }
      // else: keep rule-based intermediate state (system failed but parser succeeded)

      // Final game state is the reconciled/intermediate state
      if (intermediateState2) {
        gameState = intermediateState2;
        gameRound = intermediateState2.round || gameRound;
        gamePhase = intermediateState2.phase || 'playing';

        // === Broadcast in order: state → DM narrative → Gemma action → events ===

        // 1. Broadcast final state
        broadcastSSE({ type: 'gameState', state: gameState });

        // 2. Broadcast Qwen's narrative first (DM speaks)
        const dmCharName = (gameState && gameState.characters && gameState.characters.qwen) ? gameState.characters.qwen.name : '';
        broadcastSSE({
          type: 'message',
          role: 'dm',
          dmName: dmCharName || 'Narrator',
          content: qwenResult.narrative,
          turn: turnCount,
          timestamp: new Date().toISOString(),
        });

        // 3. Broadcast Gemma's action
        if (pendingGemmaAction) {
          broadcastSSE({
            type: 'message',
            role: 'gemma',
            content: pendingGemmaAction,
            turn: turnCount,
            timestamp: new Date().toISOString(),
          });
          messageHistory.push({
            role: 'gemma',
            content: pendingGemmaAction,
            timestamp: new Date().toISOString(),
            turn: turnCount,
          });
        }

        // 4. Broadcast events (from system if valid, else rule-based)
        var finalEvents = systemValid && intermediateState2.events ? intermediateState2.events : qwenEvents.concat(gemmaEvents);
        if (finalEvents && finalEvents.length > 0) {
          for (const event of finalEvents) {
            broadcastSSE({ type: 'gameEvent', event: event });
          }
        }

        if (!systemValid) {
          logToGameLog('System reconciliation unavailable; using rule-based state + events');
        }

        // Check game over
        if (gamePhase === 'game_over') {
          broadcastSSE({
            type: 'gameOver',
            reason: gameState.gameOverReason || 'The adventure has concluded.',
            winner: gameState.winner || 'neither',
          });
          broadcastSSE({ type: 'status', state: 'game_over', mode: 'game' });
          messageHistory.push({
            role: 'narrator',
            content: qwenResult.narrative,
            timestamp: new Date().toISOString(),
            turn: turnCount,
            gameState: gameState,
          });
          break;
        }

        // Store in history
        messageHistory.push({
          role: 'narrator',
          content: qwenResult.narrative,
          timestamp: new Date().toISOString(),
          turn: turnCount,
          gameState: gameState,
        });

        // Stats every 3 rounds
        if (gameRound % 3 === 0) {
          broadcastSSE({
            type: 'stats',
            turns: gameRound,
            messages: messageHistory.length,
            startTime: messageHistory[0]?.timestamp,
          });
        }

        logToGameLog(`Round ${gameRound}: phase=${gamePhase} location=${gameState.location || ''}`);
        logToGameLog(`  Qwen: ${(gameState.characters && gameState.characters.qwen) ? JSON.stringify(gameState.characters.qwen) : 'N/A'}`);
        logToGameLog(`  Gemma: ${(gameState.characters && gameState.characters.gemma) ? JSON.stringify(gameState.characters.gemma) : 'N/A'}`);
      }

      await sleep(TURN_INTERVAL_MS);
    }
  } catch (err) {
    console.error('Game loop error:', err);
  } finally {
    if (gamePhase === 'playing') {
      gamePhase = 'idle';
    }
  }
}

// Parse an optional \`\`\`patch ... \`\`\` block from Qwen's prose, return { narrative, patch }
function parsePatchBlock(text) {
  const patchMatch = text.match(/\`\`\`patch\n([\s\S]*?)\n\`\`\`$/);
  if (patchMatch) {
    try {
      const patchObj = JSON.parse(patchMatch[1].trim());
      const narrative = text.substring(0, patchMatch.index).trim();
      return { narrative, patch: patchObj };
    } catch (e) {
      console.warn('[game] Failed to parse Qwen patch block, ignoring');
      return { narrative: text, patch: null };
    }
  }
  return { narrative: text, patch: null };
}

async function qwenDMTurn(signal, context) {
  // MODE 1: Initialization (gameState === null) — use JSON prompt
  if (!gameState || gamePhase === 'initializing') {
    gameRound++;
    turnCount++;
    const historyMessages = buildDMContextMessage(context);

    const response = await callLLM('qwen', [
      { role: 'system', content: QWEN_DM_JSON_PROMPT },
      { role: 'system', content: `CURRENT GAME STATE: ${JSON.stringify(gameState || { phase: 'initializing', round: 0 })}` },
      ...historyMessages,
      { role: 'user', content: historyMessages.length > 0 ? 'Adjudicate the previous action, narrate what happens, and advance the story. Output ONLY the JSON game state.' : `Begin the adventure. World scenario: "${context}". Output ONLY the JSON game state.` },
    ], signal);

    if (!response) { gameRound--; turnCount--; throw new Error('Qwen DM initialization returned empty response'); }

    const parsed = extractGameState(response);
    // Validate critical fields before adopting
    if (!parsed.characters || !parsed.narrative) {
      console.warn('[game] Qwen returned incomplete state, skipping turn');
      gameRound--; turnCount--;
      throw new Error('Qwen DM initialization returned incomplete state');
    }
    gameState = parsed;
    gamePhase = parsed.phase || 'initializing';

    // Broadcast game state
    broadcastSSE({ type: 'gameState', state: gameState });

    // Broadcast narrative as message
    broadcastSSE({
      type: 'message',
      role: 'narrator',
      content: parsed.narrative || '',
      turn: turnCount,
      timestamp: new Date().toISOString(),
    });

    // Broadcast individual events
    if (parsed.events) {
      for (const event of parsed.events) {
        broadcastSSE({ type: 'gameEvent', event: event });
      }
    }

    // Store in history
    messageHistory.push({
      role: 'narrator',
      content: parsed.narrative || '',
      timestamp: new Date().toISOString(),
      turn: turnCount,
      gameState: gameState,
    });

    // Stats every 3 rounds
    if (gameRound % 3 === 0) {
      broadcastSSE({
        type: 'stats',
        turns: gameRound,
        messages: messageHistory.length,
        startTime: messageHistory[0]?.timestamp,
      });
    }

    logToGameLog(`DM turn ${turnCount}: phase=${gamePhase} round=${gameRound} location=${gameState.location || ''}`);
    logToGameLog(`  Qwen: ${(gameState && gameState.characters && gameState.characters.qwen) ? JSON.stringify(gameState.characters.qwen) : 'N/A'}`);
    logToGameLog(`  Gemma: ${(gameState && gameState.characters && gameState.characters.gemma) ? JSON.stringify(gameState.characters.gemma) : 'N/A'}`);

    return null; // JSON mode doesn't return prose/patch
  }

  // MODE 2: Playing phase — use prose-only prompt
  // Build a game state summary for the prose prompt
  let stateSummary = '';
  if (gameState && gameState.characters) {
    const q = gameState.characters.qwen || {};
    const g = gameState.characters.gemma || {};
    stateSummary = `YOUR character: ${q.name || 'unnamed'} (${q.race || ''} ${q.class || ''}), HP ${q.hp || 0}/${q.maxHp || 0}, ATK ${q.atk || 0}, DEF ${q.def || 0}. Player character: ${g.name || 'unnamed'} (${g.race || ''} ${g.class || ''}), HP ${g.hp || 0}/${g.maxHp || 0}. Location: ${gameState.location || 'Unknown'}. Round: ${gameRound}. Status effects (Qwen): ${(q.statusEffects || []).map((s) => s.type).join(', ') || 'none'}. Status effects (Gemma): ${(g.statusEffects || []).map((s) => s.type).join(', ') || 'none'}. Inventory (Qwen): ${(q.inventory || []).join(', ') || 'empty'}. Inventory (Gemma): ${(g.inventory || []).join(', ') || 'empty'}. `;
  }

  const hasHistory = messageHistory.length > 0;
  const lastMessages = messageHistory.slice(-5);

  const response = await callLLM('qwen', [
    { role: 'system', content: QWEN_DM_PLAYING_PROMPT },
    { role: 'system', content: stateSummary + 'GAME STATE SUMMARY: round=' + gameRound + ', phase=' + gamePhase },
    ...hasHistory ? lastMessages.map((m) => ({
      role: 'user',
      content: `[${m.role.toUpperCase()}] said: ${m.content}`,
    })) : [],
    { role: 'user', content: hasHistory ? 'Adjudicate the previous action, narrate what happens. Your character should take initiative and act independently.' : 'Narrate the scene. Your character should take initiative and act independently. Output prose only.' },
  ], signal);

  if (!response) return null;

  // Parse out optional patch block
  const { narrative, patch } = parsePatchBlock(response);

  logToGameLog(`DM turn ${turnCount} (prose): ${narrative.substring(0, 120)}`);
  if (patch) {
    logToGameLog(`  Qwen patch: ${JSON.stringify(patch)}`);
  }

  return { narrative: narrative || '', patch: patch || null };
}

async function gemmaPlayerTurn(signal) {
  const gameStateSummary = gameState ? buildGemmaContext() : 'The adventure is beginning...';

  const response = await callLLM('gemma', [
    { role: 'system', content: GEMMA_PLAYER_SYSTEM_PROMPT },
    ...messageHistory.slice(-MAX_CONTEXT_MESSAGES).map((m) => ({
      role: 'user',
      content: `[${m.role.toUpperCase()}] said: ${m.content}`,
    })),
    { role: 'user', content: gameStateSummary },
    { role: 'user', content: `Your turn. What does your character do? The available choices are: ${JSON.stringify(gameState?.choices || [])}` },
  ], signal);

  if (!response) return;

  pendingGemmaAction = response;
  // Broadcast and history are now handled by the calling loop for ordered output

  logToGameLog(`Gemma turn ${turnCount}: ${response.substring(0, 120)}`);

  await sleep(TURN_INTERVAL_MS);
}

// ---- Gemma Game System turn — deterministic state computation ----

async function gemmaSystemTurn(signal, currentState, qwenNarrative, gemmaAction) {
  if (!currentState) return null;

  const response = await callLLM('gemma', [
    { role: 'system', content: GEMMA_GAME_SYSTEM_PROMPT },
    { role: 'system', content: `CURRENT STATE: ${JSON.stringify(currentState)}` },
    { role: 'system', content: `GM NARRATIVE: ${qwenNarrative}` },
    { role: 'system', content: `PLAYER ACTION: ${gemmaAction || '(no action this turn)'}` },
    { role: 'user', content: 'Compute the new game state based on the rules above.' },
  ], signal, { temperature: 0.1, maxTokens: 2048 });

  if (!response) {
    console.warn('[game] Gemma Game System returned no response, keeping current state');
    return currentState;
  }

  const parsed = extractGameState(response);

  // Validate — if critical fields missing, fall back to current state
  if (!parsed.characters || (!parsed.characters.qwen && !parsed.characters.gemma)) {
    console.warn('[game] Gemma Game System returned incomplete state, keeping current state');
    return currentState;
  }

  // Ensure round increments
  if (!parsed.round || parsed.round < (currentState.round || 0)) {
    parsed.round = (currentState.round || 0) + 1;
  }

  // Ensure phase defaults to playing
  if (!parsed.phase) parsed.phase = 'playing';

  // Ensure required fields exist for both characters
  for (const key of ['qwen', 'gemma']) {
    if (!parsed.characters[key] && currentState.characters[key]) {
      parsed.characters[key] = JSON.parse(JSON.stringify(currentState.characters[key]));
    }
    if (parsed.characters[key]) {
      const ch = parsed.characters[key];
      if (ch.hp === undefined || ch.hp === null) ch.hp = (currentState.characters && currentState.characters[key]) ? (currentState.characters[key].hp || 0) : 0;
      if (!ch.inventory) ch.inventory = [];
      if (!ch.statusEffects) ch.statusEffects = [];
    }
  }

  parsed.events = parsed.events || [];

  logToGameLog(`System turn: round=${parsed.round} phase=${parsed.phase} events=${parsed.events.length}`);
  for (const ev of (parsed.events || []).slice(0, 3)) {
    logToGameLog(`  Event: ${ev.type} target=${ev.target} ${ev.description || ''}`);
  }

  return parsed;
}

// ---- API endpoints ----

app.post('/api/chat/start', (req, res) => {
  if (conversationRunning) {
    return res.status(409).json({ status: 'already_running' });
  }

  const { topic, mode } = req.body;
  gameMode = mode === 'game';

  if (gameMode) {
    gamePhase = 'initializing';
    gameState = null;
    pendingGemmaAction = null;
    gameRound = 0;
    messageHistory = [];
    conversationRunning = true;
    conversationPaused = false;

    runGameLoop(topic || 'A dungeon crawl adventure').catch((err) => {
      console.error('Game loop crashed:', err);
      conversationRunning = false;
      broadcastSSE({ type: 'status', state: 'stopped', mode: 'game' });
    });

    res.json({ status: 'started', mode: 'game' });
  } else {
    conversationRunning = true;
    conversationPaused = false;
    messageHistory = [];

    runConversationLoop(topic || 'Tell each other something interesting.').catch((err) => {
      console.error('Conversation loop error:', err);
      conversationRunning = false;
      broadcastSSE({ type: 'status', state: 'stopped' });
    });

    res.json({ status: 'started', mode: 'chat' });
  }
});

app.post('/api/chat/stop', (req, res) => {
  conversationRunning = false;
  if (conversationLoop) conversationLoop.abort();
  sseClients.clear();
  gameMode = false;
  gamePhase = 'idle';
  gameState = null;
  pendingGemmaAction = null;
  gameRound = 0;
  turnCount = 0;
  messageHistory = [];
  broadcastSSE({ type: 'status', state: 'stopped' });
  res.json({ status: 'stopped', finalTurns: turnCount, messages: messageHistory.length });
});

app.post('/api/chat/pause', (req, res) => {
  conversationPaused = true;
  broadcastSSE({ type: 'status', state: 'paused' });
  res.json({ status: 'paused' });
});

app.post('/api/chat/resume', (req, res) => {
  conversationPaused = false;
  broadcastSSE({ type: 'status', state: gameMode ? gamePhase : 'running' });
  res.json({ status: 'resumed' });
});

app.post('/api/chat/message', (req, res) => {
  const { content } = req.body;
  if (!content?.trim()) return res.status(400).json({ error: 'Empty message' });

  const msg = {
    role: 'user',
    content: content.trim(),
    timestamp: new Date().toISOString(),
  };
  messageHistory.push(msg);
  broadcastSSE({
    type: 'message',
    role: 'user',
    content: msg.content,
    timestamp: msg.timestamp,
  });
  res.json({ status: 'queued', message: msg });
});

app.get('/api/chat/history', (req, res) => {
  res.json({ messages: messageHistory });
});

app.get('/api/game/state', (req, res) => {
  if (!gameState) return res.status(404).json({ error: 'No active game' });
  res.json(gameState);
});

app.listen(PORT, () => {
  console.log(`Crossplay running at http://localhost:${PORT}`);
  console.log(`Qwen server: ${QWEN_SERVER}`);
  console.log(`Gemma server: ${GEMMA_SERVER}`);
});
