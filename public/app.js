var SSE_URL = '/api/events';
var messageList = document.getElementById('messageList');
var statusDot = document.getElementById('statusDot');
var statusText = document.getElementById('statusText');
var turnCounter = document.getElementById('turnCounter');
var startPanel = document.getElementById('startPanel');
var controlPanel = document.getElementById('controlPanel');
var topicInput = document.getElementById('topicInput');
var startBtn = document.getElementById('startBtn');
var pauseBtn = document.getElementById('pauseBtn');
var resumeBtn = document.getElementById('resumeBtn');
var stopBtn = document.getElementById('stopBtn');
var userMessage = document.getElementById('userMessage');
var sendBtn = document.getElementById('sendBtn');
var gamePanels = document.getElementById('gamePanels');
var locationBar = document.getElementById('locationBar');
var gameOverOverlay = document.getElementById('gameOverOverlay');
var newGameBtn = document.getElementById('newGameBtn');

var sse = null;
var isRunning = false;
var isPaused = false;
var autoScroll = true;
var turnCount = 0;
var currentMode = 'chat';
var currentGameState = null;

var topicPlaceholderChat = 'Enter a topic to discuss...';
var topicPlaceholderGame = 'Set the scene for your adventure (e.g., "A haunted dungeon beneath a dying castle")...';

function connectSSE() {
  if (sse) sse.close();
  sse = new EventSource(SSE_URL);

  sse.onmessage = function (e) {
    var data = JSON.parse(e.data);
    switch (data.type) {
      case 'message':
        if (data.role === 'dm') {
          appendMessage('dm', data.content, data.turn, data.timestamp, data.dmName);
        } else {
          appendMessage(data.role, data.content, data.turn, data.timestamp);
        }
        if (data.turn) turnCount = data.turn;
        updateTurnCounter();
        break;
      case 'history':
        appendMessage(data.message.role, data.message.content, null, data.message.timestamp);
        break;
      case 'status':
        updateStatus(data.state, data.mode);
        break;
      case 'stats':
        if (data.turns) turnCount = data.turns;
        updateTurnCounter();
        break;
      case 'gameState':
        currentGameState = data.state;
        updateCharacterPanels();
        updateLocationBar();
        break;
      case 'gameEvent':
        appendGameEvent(data.event);
        break;
      case 'gameOver':
        showGameOver(data.reason, data.winner);
        break;
    }
  };

  sse.onerror = function () {
    console.warn('[crossplay] SSE disconnected, reconnecting...');
    updateStatus('disconnected');
  };
}

function appendMessage(role, content, turn, timestamp, customLabel) {
  var div = document.createElement('div');
  div.className = 'message ' + role;

  var labels = { qwen: 'Qwen', gemma: 'Gemma', user: 'You', narrator: 'Narrator', dm: 'Narrator' };
  var sender = customLabel || labels[role] || role;
  var ts = timestamp ? new Date(timestamp).toLocaleTimeString() : '';

  var senderSpan = document.createElement('span');
  senderSpan.className = 'sender';
  senderSpan.textContent = (labels[role] || role) + (turn ? ' (turn ' + turn + ')' : '');

  var contentSpan = document.createElement('span');
  contentSpan.className = 'content';
  contentSpan.textContent = content;

  div.appendChild(senderSpan);
  div.appendChild(contentSpan);

  if (ts) {
    var tsSpan = document.createElement('span');
    tsSpan.className = 'timestamp';
    tsSpan.textContent = ts;
    div.appendChild(tsSpan);
  }

  messageList.appendChild(div);
  if (autoScroll) messageList.scrollTop = messageList.scrollHeight;
}

function appendGameEvent(event) {
  if (!event || !event.description) return;
  var div = document.createElement('div');
  div.className = 'message event';

  var typeLabels = { damage: '[Damage]', heal: '[Heal]', gainItem: '[Item]', loseItem: '[Item Lost]', levelUp: '[Level Up!]', death: '[Death]', locationChange: '[Location]', statusEffect: '[Effect]', xpGain: '[XP]' };
  var label = typeLabels[event.type] || '[Event]';

  var senderSpan = document.createElement('span');
  senderSpan.className = 'sender';
  senderSpan.textContent = label;

  var contentSpan = document.createElement('span');
  contentSpan.className = 'content';
  contentSpan.textContent = event.description;

  div.appendChild(senderSpan);
  div.appendChild(contentSpan);
  messageList.appendChild(div);
  if (autoScroll) messageList.scrollTop = messageList.scrollHeight;
}

function updateStatus(state, mode) {
  statusDot.className = 'dot';
  statusText.textContent = state.charAt(0).toUpperCase() + state.slice(1).replace('_', ' ');
  if (mode) currentMode = mode;

  switch (state) {
    case 'running':
    case 'playing':
    case 'initializing':
    case 'character_creation':
      statusDot.className = 'dot running';
      isRunning = true;
      isPaused = false;
      startPanel.classList.add('hidden');
      controlPanel.classList.remove('hidden');
      pauseBtn.classList.remove('hidden');
      resumeBtn.classList.add('hidden');
      enableUserInput(true);

      if (currentMode === 'game') {
        gamePanels.classList.remove('hidden');
        locationBar.classList.remove('hidden');
        userMessage.placeholder = 'Whisper to the void...';
        hideGameOver();
      } else {
        gamePanels.classList.add('hidden');
        locationBar.classList.add('hidden');
        userMessage.placeholder = 'Send a message to the agents...';
      }
      break;
    case 'paused':
      statusDot.className = 'dot paused';
      isPaused = true;
      pauseBtn.classList.add('hidden');
      resumeBtn.classList.remove('hidden');
      break;
    case 'stopped':
    case 'idle':
      statusDot.className = 'dot stopped';
      isRunning = false;
      isPaused = false;
      startPanel.classList.remove('hidden');
      controlPanel.classList.add('hidden');
      enableUserInput(false);
      gamePanels.classList.add('hidden');
      locationBar.classList.add('hidden');
      resetCharPanels();
      break;
    case 'game_over':
      statusDot.className = 'dot stopped';
      isRunning = false;
      isPaused = false;
      controlPanel.classList.add('hidden');
      enableUserInput(false);
      break;
    case 'disconnected':
      statusText.textContent = 'Disconnected';
      break;
  }
}

function updateTurnCounter() {
  turnCounter.textContent = turnCount > 0 ? turnCount + (turnCount === 1 ? ' turn' : ' turns') : '';
}

function enableUserInput(enabled) {
  userMessage.disabled = !enabled;
  sendBtn.disabled = !enabled;
  if (enabled) userMessage.focus();
}

function updateCharacterPanels() {
  if (!currentGameState) return;
  var chars = currentGameState.characters;
  if (!chars) return;

  updateCharPanel('qwen', chars.qwen || {});
  updateCharPanel('gemma', chars.gemma || {});
}

function updateCharPanel(prefix, char) {
  var hasChar = char && char.name && char.name.trim() !== '';
  var name = hasChar ? char.name : 'Awaiting character...';
  var title = hasChar ? (char.race || '') + ' ' + (char.class || '') : '—';

  document.getElementById(prefix + 'CharName').textContent = name;
  document.getElementById(prefix + 'CharClass').textContent = title;

  var maxHp = char.maxHp || 1;
  var pct = Math.max(0, Math.min(100, (char.hp / maxHp) * 100));
  document.getElementById(prefix + 'HpBar').style.width = pct + '%';
  document.getElementById(prefix + 'HpText').textContent = (char.hp || 0) + '/' + maxHp;

  document.getElementById(prefix + 'Level').textContent = char.level || 1;
  document.getElementById(prefix + 'Atk').textContent = char.atk || 0;
  document.getElementById(prefix + 'Def').textContent = char.def || 0;
  document.getElementById(prefix + 'Xp').textContent = (char.xp || 0) + '/' + (char.xpToNext || 200);

  var invEl = document.getElementById(prefix + 'Inventory');
  invEl.innerHTML = '';
  if (char.inventory && char.inventory.length) {
    char.inventory.forEach(function (item) {
      var s = document.createElement('span');
      s.textContent = typeof item === 'string' ? item : (item.name || JSON.stringify(item));
      invEl.appendChild(s);
    });
  }

  var statusEl = document.getElementById(prefix + 'StatusEffects');
  statusEl.innerHTML = '';
  if (char.statusEffects && char.statusEffects.length) {
    char.statusEffects.forEach(function (effect) {
      var s = document.createElement('span');
      s.className = 'debuff';
      var label = typeof effect === 'string' ? effect : (effect.type || 'Unknown');
      var dur = typeof effect === 'object' ? (effect.duration || '?') : '...';
      s.textContent = label + ' (' + dur + ')';
      statusEl.appendChild(s);
    });
  }
}

function resetCharPanels() {
  ['qwen', 'gemma'].forEach(function (prefix) {
    document.getElementById(prefix + 'CharName').textContent = '\u2014';
    document.getElementById(prefix + 'CharClass').textContent = '\u2014';
    document.getElementById(prefix + 'HpBar').style.width = '100%';
    document.getElementById(prefix + 'HpText').textContent = '0/0';
    document.getElementById(prefix + 'Level').textContent = '1';
    document.getElementById(prefix + 'Atk').textContent = '0';
    document.getElementById(prefix + 'Def').textContent = '0';
    document.getElementById(prefix + 'Xp').textContent = '0/200';
    document.getElementById(prefix + 'Inventory').innerHTML = '';
    document.getElementById(prefix + 'StatusEffects').innerHTML = '';
  });
}

function updateLocationBar() {
  if (!currentGameState) return;
  document.getElementById('locationName').textContent = currentGameState.location || '\u2014';
  document.getElementById('locationDesc').textContent = currentGameState.locationDescription || '';
}

function showGameOver(reason, winner) {
  document.getElementById('gameOverReason').textContent = reason || 'The chronicle is complete. Your journey has reached its end.';
  var winnerLabels = { qwen: 'Victory to Qwen!', gemma: 'Victory to Gemma!', both: 'Both heroes survive!', neither: 'The tale ends in shadow.' };
  document.getElementById('gameOverWinner').textContent = winnerLabels[winner] || 'The adventure is over.';
  gameOverOverlay.classList.remove('hidden');
}

function hideGameOver() {
  gameOverOverlay.classList.add('hidden');
}

function apiCall(path, body) {
  return fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body || {}),
  }).then(function (r) {
    if (!r.ok) throw new Error('API error: ' + r.status);
    return r.json();
  });
}

// Mode selector update placeholder
var radios = document.querySelectorAll('input[name="mode"]');
radios.forEach(function (radio) {
  radio.addEventListener('change', function () {
    topicInput.placeholder = radio.value === 'game' ? topicPlaceholderGame : topicPlaceholderChat;
    startBtn.textContent = radio.value === 'game' ? 'Begin Adventure' : 'Start Chat';
  });
});

function getSelectedMode() {
  return document.querySelector('input[name="mode"]:checked').value;
}

startBtn.addEventListener('click', function () {
  var mode = getSelectedMode();
  var topic = topicInput.value.trim() || (mode === 'game' ? 'A dungeon crawl adventure' : 'Tell each other something interesting.');
  apiCall('/api/chat/start', { topic: topic, mode: mode }).then(function () {
    topicInput.value = '';
  });
});

stopBtn.addEventListener('click', function () {
  apiCall('/api/chat/stop');
  hideGameOver();
});
pauseBtn.addEventListener('click', function () { apiCall('/api/chat/pause'); });
resumeBtn.addEventListener('click', function () { apiCall('/api/chat/resume'); });

newGameBtn.addEventListener('click', function () {
  apiCall('/api/chat/stop');
  hideGameOver();
});

sendBtn.addEventListener('click', sendUserMessage);
userMessage.addEventListener('keydown', function (e) {
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendUserMessage(); }
});
topicInput.addEventListener('keydown', function (e) {
  if (e.key === 'Enter') startBtn.click();
});

function sendUserMessage() {
  var content = userMessage.value.trim();
  if (!content) return;
  apiCall('/api/chat/message', { content: content });
  userMessage.value = '';
}

messageList.addEventListener('scroll', function () {
  var st = messageList.scrollTop;
  var sh = messageList.scrollHeight;
  var ch = messageList.clientHeight;
  autoScroll = sh - st - ch < 100;
});

connectSSE();
