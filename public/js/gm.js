/* jshint browser:true */
'use strict';

(function () {
  let socket = null;
  let gameState = null;

  // ── DOM refs ──────────────────────────────────────────────────────────────
  const connDot       = document.getElementById('conn-dot');
  const connStatus    = document.getElementById('conn-status');
  const playersGrid   = document.getElementById('players-grid');
  const playerCount   = document.getElementById('player-count');
  const noPlayers     = document.getElementById('no-players');
  const templateGrid  = document.getElementById('template-grid');
  const qrPanel       = document.getElementById('qr-panel');
  const qrImg         = document.getElementById('qr-img');
  const joinUrlEl     = document.getElementById('join-url');
  const voteTally     = document.getElementById('vote-tally');
  const voteTallyCard = document.getElementById('vote-tally-card');

  // ── Socket ────────────────────────────────────────────────────────────────
  socket = io();
  socket.on('connect', () => {
    connDot.className = 'conn-dot online';
    connStatus.textContent = 'Connected as GM';
    socket.emit('gm:join');
  });
  socket.on('disconnect', () => {
    connDot.className = 'conn-dot offline';
    connStatus.textContent = 'Disconnected';
  });
  socket.on('gameState', (gs) => {
    gameState = gs;
    renderAll(gs);
  });

  // ── Render ────────────────────────────────────────────────────────────────
  function renderAll(gs) {
    renderModuleToggles(gs.activeModules);
    renderPlayers(gs.players, gs.votes, gs.activeModules);
    renderVoteTally(gs.votes, gs.players);
    renderRolePlayerSelect(gs.players);
    updateActiveTemplate(gs.template);
    playerCount.textContent = Object.keys(gs.players || {}).length;
    noPlayers.style.display = Object.keys(gs.players || {}).length ? 'none' : 'block';
    voteTallyCard.style.display = gs.votingOpen ? 'block' : 'none';
  }

  function renderPlayers(players, votes, modules) {
    if (!players) return;
    playersGrid.innerHTML = '';
    Object.values(players).forEach((p) => {
      const card = document.createElement('div');
      const st   = (p.status || 'ALIVE').toLowerCase();
      card.className = `player-card ${st}`;
      card.innerHTML = `
        <div class="player-card-top">
          <img class="player-card-avatar" src="${p.image || '/img/default-avatar.svg'}" alt=""/>
          <div>
            <div class="player-card-name">${escHtml(p.name)}</div>
            ${modules?.points !== false ? `<div class="player-card-pts">${p.points || 0} pts</div>` : ''}
          </div>
          <span class="badge badge-${st}" style="margin-left:auto">${st.toUpperCase()}</span>
        </div>
        ${modules?.roles !== false ? `<div class="player-card-role">🎭 ${escHtml(p.role || '—')}</div>` : ''}
        <div class="player-card-status">${p.connected ? '🟢 Online' : '🔴 Offline'} | Buzzer: ${p.buzzerEnabled ? '✅' : '❌'}</div>
        <div class="player-actions">
          ${modules?.points !== false ? `
            <button class="btn btn-sm btn-green" data-action="pts" data-id="${p.id}" data-delta="1">+1</button>
            <button class="btn btn-sm btn-green" data-action="pts" data-id="${p.id}" data-delta="5">+5</button>
            <button class="btn btn-sm btn-red"   data-action="pts" data-id="${p.id}" data-delta="-1">-1</button>
            <button class="btn btn-sm btn-red"   data-action="pts" data-id="${p.id}" data-delta="-5">-5</button>
          ` : ''}
          ${modules?.status !== false ? `
            <button class="btn btn-sm" data-action="status" data-id="${p.id}" data-status="ALIVE">Alive</button>
            <button class="btn btn-sm btn-red" data-action="status" data-id="${p.id}" data-status="DEAD">Dead</button>
            <button class="btn btn-sm" data-action="status" data-id="${p.id}" data-status="MUTE">Mute</button>
          ` : ''}
          ${modules?.buzzer !== false ? `
            <button class="btn btn-sm" data-action="buzzer" data-id="${p.id}" data-enabled="${p.buzzerEnabled ? 'false' : 'true'}">
              ${p.buzzerEnabled ? '🔕 Disable Buzz' : '🔔 Enable Buzz'}
            </button>
          ` : ''}
          <button class="btn btn-sm btn-red" data-action="remove" data-id="${p.id}">✕ Remove</button>
        </div>
      `;
      playersGrid.appendChild(card);
    });

    // Event delegation on grid
    playersGrid.onclick = (e) => {
      const btn = e.target.closest('[data-action]');
      if (!btn) return;
      const { action, id, delta, status, enabled } = btn.dataset;
      if (action === 'pts')    socket.emit('gm:setPoints', { playerId: id, delta: Number(delta) });
      if (action === 'status') socket.emit('gm:setStatus', { playerId: id, status });
      if (action === 'buzzer') socket.emit('gm:disableBuzzerForPlayer', { playerId: id, enabled: enabled === 'true' });
      if (action === 'remove') { if (confirm(`Remove ${btn.closest('.player-card').querySelector('.player-card-name').textContent}?`)) socket.emit('gm:removePlayer', { playerId: id }); }
    };
  }

  function renderVoteTally(votes, players) {
    if (!votes || !players) return;
    const tally = {};
    Object.values(votes).forEach((tid) => { tally[tid] = (tally[tid] || 0) + 1; });
    if (!Object.keys(tally).length) {
      voteTally.innerHTML = '<span style="color:var(--text-muted)">No votes yet</span>';
      return;
    }
    voteTally.innerHTML = Object.entries(tally)
      .sort((a, b) => b[1] - a[1])
      .map(([tid, cnt]) => `
        <div class="vote-row">
          <span>${escHtml(players[tid]?.name || tid)}</span>
          <strong>${cnt}</strong>
        </div>`)
      .join('');
  }

  function renderRolePlayerSelect(players) {
    const sel = document.getElementById('role-player-select');
    const current = sel.value;
    sel.innerHTML = '<option value="">— Select Player —</option>';
    if (!players) return;
    Object.values(players).forEach((p) => {
      const opt = document.createElement('option');
      opt.value = p.id;
      opt.textContent = p.name;
      if (p.id === current) opt.selected = true;
      sel.appendChild(opt);
    });
  }

  function renderModuleToggles(modules) {
    if (!modules) return;
    document.querySelectorAll('[data-module]').forEach((cb) => {
      cb.checked = modules[cb.dataset.module] !== false;
    });
  }

  function updateActiveTemplate(templateName) {
    document.querySelectorAll('.template-btn').forEach((b) => {
      b.classList.toggle('active', b.dataset.tmpl === templateName);
    });
  }

  // ── Load templates ────────────────────────────────────────────────────────
  fetch('/api/templates')
    .then((r) => r.json())
    .then((templates) => {
      templateGrid.innerHTML = '';
      templates.forEach((t) => {
        const b = document.createElement('button');
        b.className = 'btn template-btn';
        // Use t.id (filename-based) so the server can locate the .json file.
        // Fall back to a slugified t.name for backwards-compat.
        b.dataset.tmpl = t.id || t.name.toLowerCase().replace(/\s+/g, '_');
        b.title = t.description || '';
        b.textContent = t.name;
        b.addEventListener('click', () => {
          socket.emit('gm:loadTemplate', { templateName: b.dataset.tmpl });
        });
        templateGrid.appendChild(b);
      });
      if (gameState) updateActiveTemplate(gameState.template);
    })
    .catch(() => {});

  // ── Module toggles ────────────────────────────────────────────────────────
  document.getElementById('module-toggles').addEventListener('change', () => {
    const modules = {};
    document.querySelectorAll('[data-module]').forEach((cb) => {
      modules[cb.dataset.module] = cb.checked;
    });
    socket.emit('gm:setModules', { modules });
  });

  // ── Role assignment ───────────────────────────────────────────────────────
  document.getElementById('assign-role-btn').addEventListener('click', () => {
    const pid  = document.getElementById('role-player-select').value;
    const role = document.getElementById('role-text-input').value.trim();
    if (!pid)  { toast('Select a player', 'error'); return; }
    if (!role) { toast('Enter a role', 'error'); return; }
    socket.emit('gm:assignRole', { playerId: pid, role });
    document.getElementById('role-text-input').value = '';
    toast(`Role assigned!`, 'success');
  });

  // ── QR Code ───────────────────────────────────────────────────────────────
  document.getElementById('qr-btn').addEventListener('click', () => {
    fetch('/qr')
      .then((r) => r.json())
      .then((d) => {
        qrImg.src = d.qr;
        joinUrlEl.textContent = d.url;
        qrPanel.style.display = 'block';
      })
      .catch(() => toast('Could not generate QR', 'error'));
  });
  document.getElementById('qr-close-btn').addEventListener('click', () => { qrPanel.style.display = 'none'; });

  // ── Global actions ────────────────────────────────────────────────────────
  document.getElementById('enable-buzzers-btn').addEventListener('click',  () => socket.emit('gm:enableBuzzers'));
  document.getElementById('reset-buzzer-btn').addEventListener('click',   () => socket.emit('gm:resetBuzzer'));
  document.getElementById('open-vote-btn').addEventListener('click',      () => socket.emit('gm:openVoting'));
  document.getElementById('close-vote-btn').addEventListener('click',     () => socket.emit('gm:closeVoting'));
  document.getElementById('reveal-votes-btn').addEventListener('click',   () => socket.emit('gm:revealVotes'));
  document.getElementById('show-roles-btn').addEventListener('click',     () => socket.emit('gm:showAllRoles', { show: true }));
  document.getElementById('hide-roles-btn').addEventListener('click',     () => socket.emit('gm:showAllRoles', { show: false }));
  document.getElementById('reset-scores-btn').addEventListener('click', () => {
    if (confirm('Reset all scores to 0?')) socket.emit('gm:resetScores');
  });

  // ── Toast helper ──────────────────────────────────────────────────────────
  function toast(msg, type = '') {
    const c = document.getElementById('toast-container');
    const t = document.createElement('div');
    t.className = 'toast ' + type;
    t.textContent = msg;
    c.appendChild(t);
    setTimeout(() => t.remove(), 3000);
  }

  function escHtml(s) {
    return String(s).replace(/[<>"'&]/g, (c) =>
      ({ '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;', '&': '&amp;' }[c])
    );
  }
})();
