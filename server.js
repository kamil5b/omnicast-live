'use strict';

const express = require('express');
const http = require('http');
const { Server } = require('socket.io');
const multer = require('multer');
const rateLimit = require('express-rate-limit');
const path = require('path');
const fs = require('fs');
const os = require('os');
const QRCode = require('qrcode');
const { v4: uuidv4 } = require('uuid');

const app = express();
const server = http.createServer(app);
const io = new Server(server, { cors: { origin: '*' } });

const PORT = process.env.PORT || 3000;
const UPLOADS_DIR = path.join(__dirname, 'uploads');
const TEMPLATES_DIR = path.join(__dirname, 'templates');

// Ensure directories exist
[UPLOADS_DIR, TEMPLATES_DIR].forEach((dir) => {
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true });
});

// Multer storage for player images
const storage = multer.diskStorage({
  destination: UPLOADS_DIR,
  filename: (_req, file, cb) => {
    const ext = path.extname(file.originalname).toLowerCase() || '.jpg';
    cb(null, `${uuidv4()}${ext}`);
  },
});

const fileFilter = (_req, file, cb) => {
  if (file.mimetype.startsWith('image/')) cb(null, true);
  else cb(new Error('Only image files are allowed'), false);
};

const upload = multer({
  storage,
  fileFilter,
  limits: { fileSize: 5 * 1024 * 1024 }, // 5 MB
});

// ── Rate limiters ─────────────────────────────────────────────────────────────
// General page/API limiter: 200 requests per minute per IP
const generalLimiter = rateLimit({ windowMs: 60 * 1000, max: 200, standardHeaders: true, legacyHeaders: false });
// Upload limiter: 30 uploads per minute per IP
const uploadLimiter = rateLimit({ windowMs: 60 * 1000, max: 30,  standardHeaders: true, legacyHeaders: false });

// ── Static assets ──────────────────────────────────────────────────────────────
app.use(express.static(path.join(__dirname, 'public')));
app.use('/uploads', express.static(UPLOADS_DIR));
app.use(express.json());

// ── Routes ────────────────────────────────────────────────────────────────────
app.get('/', generalLimiter, (_req, res) =>
  res.sendFile(path.join(__dirname, 'public', 'index.html'))
);
app.get('/gm', generalLimiter, (_req, res) =>
  res.sendFile(path.join(__dirname, 'public', 'gm.html'))
);
app.get('/operator', generalLimiter, (_req, res) =>
  res.sendFile(path.join(__dirname, 'public', 'operator.html'))
);
app.get('/overlay', generalLimiter, (_req, res) =>
  res.sendFile(path.join(__dirname, 'public', 'overlay.html'))
);

// QR code for local network IP
app.get('/qr', generalLimiter, async (req, res) => {
  try {
    const ip = getLocalIP();
    const url = `http://${ip}:${PORT}`;
    const qrDataURL = await QRCode.toDataURL(url);
    res.json({ url, qr: qrDataURL });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Upload player image
app.post('/upload', uploadLimiter, upload.single('image'), (req, res) => {
  if (!req.file) return res.status(400).json({ error: 'No image provided' });
  res.json({ filename: req.file.filename, url: `/uploads/${req.file.filename}` });
});

// Template API
app.get('/api/templates', generalLimiter, (_req, res) => {
  try {
    const files = fs
      .readdirSync(TEMPLATES_DIR)
      .filter((f) => f.endsWith('.json'));
    const templates = files.map((f) => {
      const data = JSON.parse(fs.readFileSync(path.join(TEMPLATES_DIR, f), 'utf8'));
      return { name: f.replace('.json', ''), ...data };
    });
    res.json(templates);
  } catch {
    res.json([]);
  }
});

app.post('/api/templates', generalLimiter, (req, res) => {
  const { name, modules, description } = req.body;
  if (!name) return res.status(400).json({ error: 'Template name required' });
  const safeName = name.replace(/[^a-zA-Z0-9_-]/g, '_');
  const filePath = path.join(TEMPLATES_DIR, `${safeName}.json`);
  fs.writeFileSync(filePath, JSON.stringify({ name, description: description || '', modules }, null, 2));
  res.json({ success: true, name: safeName });
});

// ── Game State ────────────────────────────────────────────────────────────────
const gameState = {
  players: {},   // id -> PlayerRecord
  roles: {},     // GM-managed: id -> roleText (never broadcast to other players)
  buzzerLocked: false,
  buzzerWinner: null,
  votingOpen: false,
  votes: {},     // voterId -> targetId (private to GM)
  votesRevealed: false,
  revealedVotes: null,
  showAllRoles: false,
  activeModules: {
    buzzer: true,
    points: true,
    roles: true,
    voting: true,
    status: true,
  },
  template: 'custom',
};

/** Return the public game state (no private role data) */
function publicState() {
  const players = {};
  Object.values(gameState.players).forEach((p) => {
    players[p.id] = {
      id: p.id,
      name: p.name,
      image: p.image,
      points: p.points,
      status: p.status,
      buzzerEnabled: p.buzzerEnabled,
      connected: p.connected,
    };
  });
  return {
    players,
    buzzerLocked: gameState.buzzerLocked,
    buzzerWinner: gameState.buzzerWinner,
    votingOpen: gameState.votingOpen,
    votesRevealed: gameState.votesRevealed,
    revealedVotes: gameState.revealedVotes,
    showAllRoles: gameState.showAllRoles,
    activeModules: gameState.activeModules,
    template: gameState.template,
  };
}

/** Return GM-only state (includes roles and votes) */
function gmState() {
  const base = publicState();
  const playersWithRoles = {};
  Object.values(gameState.players).forEach((p) => {
    playersWithRoles[p.id] = {
      ...base.players[p.id],
      role: gameState.roles[p.id] || '',
    };
  });
  return {
    ...base,
    players: playersWithRoles,
    votes: gameState.votes,
  };
}

// ── Role helpers ──────────────────────────────────────────────────────────────
function broadcastPublicState() {
  io.to('overlay').emit('gameState', publicState());
  io.to('operator').emit('gameState', publicState());
  const gmSockets = io.sockets.adapter.rooms.get('gm');
  if (gmSockets) {
    gmSockets.forEach((sid) => {
      io.to(sid).emit('gameState', gmState());
    });
  }
  // Send each player their own private state
  Object.values(gameState.players).forEach((player) => {
    if (player.socketId && player.connected) {
      io.to(player.socketId).emit('playerState', {
        id: player.id,
        name: player.name,
        image: player.image,
        points: player.points,
        status: player.status,
        buzzerEnabled: player.buzzerEnabled,
        role: gameState.roles[player.id] || '',
        votingOpen: gameState.votingOpen,
        buzzerLocked: gameState.buzzerLocked,
        buzzerWinner: gameState.buzzerWinner,
        activeModules: gameState.activeModules,
        showAllRoles: gameState.showAllRoles,
        revealedVotes: gameState.revealedVotes,
      });
    }
  });
}

// ── Socket.io ─────────────────────────────────────────────────────────────────
io.on('connection', (socket) => {
  // ── Join as player ──
  socket.on('player:join', ({ name, imageUrl, playerId }) => {
    const id = playerId || uuidv4();
    const existing = gameState.players[id];
    gameState.players[id] = {
      id,
      name: sanitize(name) || `Player ${Object.keys(gameState.players).length + 1}`,
      image: imageUrl || existing?.image || '/img/default-avatar.svg',
      points: existing?.points ?? 0,
      status: existing?.status ?? 'ALIVE',
      buzzerEnabled: existing?.buzzerEnabled ?? true,
      socketId: socket.id,
      connected: true,
    };
    socket.data.role = 'player';
    socket.data.playerId = id;
    socket.join('players');
    socket.emit('joined', { id, role: 'player' });
    broadcastPublicState();
  });

  // ── Join as GM ──
  socket.on('gm:join', () => {
    socket.data.role = 'gm';
    socket.join('gm');
    socket.emit('joined', { role: 'gm' });
    socket.emit('gameState', gmState());
  });

  // ── Join as Operator ──
  socket.on('operator:join', () => {
    socket.data.role = 'operator';
    socket.join('operator');
    socket.emit('joined', { role: 'operator' });
    socket.emit('gameState', publicState());
  });

  // ── Join as Overlay ──
  socket.on('overlay:join', () => {
    socket.data.role = 'overlay';
    socket.join('overlay');
    socket.emit('gameState', publicState());
  });

  // ── Buzzer ──
  socket.on('player:buzz', () => {
    if (socket.data.role !== 'player') return;
    const pid = socket.data.playerId;
    const player = gameState.players[pid];
    if (!player || !player.buzzerEnabled || gameState.buzzerLocked) return;
    if (!gameState.activeModules.buzzer) return;
    gameState.buzzerLocked = true;
    gameState.buzzerWinner = pid;
    broadcastPublicState();
    io.emit('buzzer:winner', { playerId: pid, playerName: player.name });
  });

  // ── Voting ──
  socket.on('player:vote', ({ targetId }) => {
    if (socket.data.role !== 'player') return;
    const pid = socket.data.playerId;
    if (!gameState.votingOpen) return;
    if (!gameState.activeModules.voting) return;
    if (!gameState.players[targetId]) return;
    gameState.votes[pid] = targetId;
    // Notify GM only
    const gmSockets = io.sockets.adapter.rooms.get('gm');
    if (gmSockets) {
      gmSockets.forEach((sid) => io.to(sid).emit('gameState', gmState()));
    }
    socket.emit('vote:confirmed', { targetId });
  });

  // ──────────── GM Events ──────────────────────────────────────────────────
  socket.on('gm:setPoints', ({ playerId, delta }) => {
    if (socket.data.role !== 'gm') return;
    if (!gameState.players[playerId]) return;
    if (!gameState.activeModules.points) return;
    gameState.players[playerId].points = (gameState.players[playerId].points || 0) + Number(delta);
    broadcastPublicState();
  });

  socket.on('gm:setStatus', ({ playerId, status }) => {
    if (socket.data.role !== 'gm') return;
    if (!gameState.players[playerId]) return;
    if (!gameState.activeModules.status) return;
    const valid = ['ALIVE', 'DEAD', 'MUTE'];
    if (!valid.includes(status)) return;
    gameState.players[playerId].status = status;
    broadcastPublicState();
  });

  socket.on('gm:assignRole', ({ playerId, role }) => {
    if (socket.data.role !== 'gm') return;
    if (!gameState.players[playerId]) return;
    if (!gameState.activeModules.roles) return;
    gameState.roles[playerId] = sanitize(role);
    // Send role ONLY to that specific player
    const player = gameState.players[playerId];
    if (player.socketId && player.connected) {
      io.to(player.socketId).emit('playerState', {
        id: player.id,
        name: player.name,
        image: player.image,
        points: player.points,
        status: player.status,
        buzzerEnabled: player.buzzerEnabled,
        role: gameState.roles[player.id] || '',
        votingOpen: gameState.votingOpen,
        buzzerLocked: gameState.buzzerLocked,
        buzzerWinner: gameState.buzzerWinner,
        activeModules: gameState.activeModules,
        showAllRoles: gameState.showAllRoles,
        revealedVotes: gameState.revealedVotes,
      });
    }
    const gmSockets = io.sockets.adapter.rooms.get('gm');
    if (gmSockets) {
      gmSockets.forEach((sid) => io.to(sid).emit('gameState', gmState()));
    }
  });

  socket.on('gm:enableBuzzers', () => {
    if (socket.data.role !== 'gm') return;
    gameState.buzzerLocked = false;
    gameState.buzzerWinner = null;
    broadcastPublicState();
    io.emit('buzzer:enabled');
  });

  socket.on('gm:resetBuzzer', () => {
    if (socket.data.role !== 'gm') return;
    gameState.buzzerLocked = false;
    gameState.buzzerWinner = null;
    broadcastPublicState();
    io.emit('buzzer:reset');
  });

  socket.on('gm:disableBuzzerForPlayer', ({ playerId, enabled }) => {
    if (socket.data.role !== 'gm') return;
    if (!gameState.players[playerId]) return;
    gameState.players[playerId].buzzerEnabled = !!enabled;
    broadcastPublicState();
  });

  socket.on('gm:openVoting', () => {
    if (socket.data.role !== 'gm') return;
    if (!gameState.activeModules.voting) return;
    gameState.votingOpen = true;
    gameState.votes = {};
    gameState.votesRevealed = false;
    gameState.revealedVotes = null;
    broadcastPublicState();
  });

  socket.on('gm:closeVoting', () => {
    if (socket.data.role !== 'gm') return;
    gameState.votingOpen = false;
    broadcastPublicState();
  });

  socket.on('gm:revealVotes', () => {
    if (socket.data.role !== 'gm') return;
    gameState.votesRevealed = true;
    // Tally votes
    const tally = {};
    Object.values(gameState.votes).forEach((targetId) => {
      tally[targetId] = (tally[targetId] || 0) + 1;
    });
    const players = gameState.players;
    gameState.revealedVotes = Object.entries(tally)
      .map(([id, count]) => ({
        playerId: id,
        playerName: players[id]?.name || id,
        votes: count,
      }))
      .sort((a, b) => b.votes - a.votes);
    broadcastPublicState();
  });

  socket.on('gm:showAllRoles', ({ show }) => {
    if (socket.data.role !== 'gm') return;
    if (!gameState.activeModules.roles) return;
    gameState.showAllRoles = !!show;
    if (show) {
      // Build role data for overlay
      const roleData = {};
      Object.values(gameState.players).forEach((p) => {
        roleData[p.id] = gameState.roles[p.id] || '—';
      });
      io.to('overlay').emit('rolesRevealed', roleData);
    }
    broadcastPublicState();
  });

  socket.on('gm:removePlayer', ({ playerId }) => {
    if (socket.data.role !== 'gm') return;
    delete gameState.players[playerId];
    delete gameState.roles[playerId];
    delete gameState.votes[playerId];
    broadcastPublicState();
  });

  socket.on('gm:resetScores', () => {
    if (socket.data.role !== 'gm') return;
    Object.values(gameState.players).forEach((p) => { p.points = 0; });
    broadcastPublicState();
  });

  socket.on('gm:loadTemplate', ({ templateName }) => {
    if (socket.data.role !== 'gm') return;
    const filePath = path.join(TEMPLATES_DIR, `${templateName}.json`);
    if (!fs.existsSync(filePath)) return;
    const tmpl = JSON.parse(fs.readFileSync(filePath, 'utf8'));
    if (tmpl.modules) {
      gameState.activeModules = { ...gameState.activeModules, ...tmpl.modules };
    }
    gameState.template = templateName;
    broadcastPublicState();
  });

  socket.on('gm:setModules', ({ modules }) => {
    if (socket.data.role !== 'gm') return;
    gameState.activeModules = { ...gameState.activeModules, ...modules };
    broadcastPublicState();
  });

  // ──────────── Operator Events ────────────────────────────────────────────
  socket.on('operator:overrideImage', ({ playerId, imageUrl }) => {
    if (socket.data.role !== 'operator') return;
    if (!gameState.players[playerId]) return;
    gameState.players[playerId].image = imageUrl;
    broadcastPublicState();
  });

  socket.on('operator:overrideImageUpload', ({ playerId, filename }) => {
    if (socket.data.role !== 'operator') return;
    if (!gameState.players[playerId]) return;
    gameState.players[playerId].image = `/uploads/${filename}`;
    broadcastPublicState();
  });

  socket.on('operator:showAllRoles', ({ show }) => {
    if (socket.data.role !== 'operator') return;
    if (!gameState.activeModules.roles) return;
    gameState.showAllRoles = !!show;
    if (show) {
      const roleData = {};
      Object.values(gameState.players).forEach((p) => {
        roleData[p.id] = gameState.roles[p.id] || '—';
      });
      io.to('overlay').emit('rolesRevealed', roleData);
    }
    broadcastPublicState();
  });

  socket.on('operator:resetScores', () => {
    if (socket.data.role !== 'operator') return;
    Object.values(gameState.players).forEach((p) => { p.points = 0; });
    broadcastPublicState();
  });

  // ── Disconnect ──
  socket.on('disconnect', () => {
    if (socket.data.role === 'player' && socket.data.playerId) {
      const player = gameState.players[socket.data.playerId];
      if (player) {
        player.connected = false;
        player.socketId = null;
      }
      broadcastPublicState();
    }
  });
});

// ── Helpers ───────────────────────────────────────────────────────────────────
function getLocalIP() {
  const interfaces = os.networkInterfaces();
  for (const iface of Object.values(interfaces)) {
    for (const alias of iface) {
      if (alias.family === 'IPv4' && !alias.internal) return alias.address;
    }
  }
  return 'localhost';
}

function sanitize(str) {
  if (typeof str !== 'string') return '';
  return str.replace(/[<>"'&]/g, (c) => ({ '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;', '&': '&amp;' }[c]));
}

// ── Start ─────────────────────────────────────────────────────────────────────
server.listen(PORT, '0.0.0.0', () => {
  const ip = getLocalIP();
  console.log(`\n🎮 OmniCast Live Engine running`);
  console.log(`   Local:   http://localhost:${PORT}`);
  console.log(`   Network: http://${ip}:${PORT}`);
  console.log(`\n   GM:       http://${ip}:${PORT}/gm`);
  console.log(`   Operator: http://${ip}:${PORT}/operator`);
  console.log(`   Overlay:  http://${ip}:${PORT}/overlay`);
  console.log(`   Players:  http://${ip}:${PORT}  (share QR code)\n`);
});

module.exports = { app, server, gameState };
