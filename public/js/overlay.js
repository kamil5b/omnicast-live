/* jshint browser:true */
'use strict';

(function () {
  const grid          = document.getElementById('portraits-grid');
  const buzzerBanner  = document.getElementById('buzzer-banner');
  const buzzerWinName = document.getElementById('buzzer-winner-name');
  const votePanel     = document.getElementById('vote-panel');
  const voteResults   = document.getElementById('vote-results');

  let prevState = {};      // id -> { points }
  let revealedRoles = {};  // id -> roleText

  const socket = io();

  socket.on('connect', () => socket.emit('overlay:join'));

  socket.on('gameState', (gs) => {
    renderPortraits(gs);
    renderBuzzer(gs);
    renderVotes(gs);
    prevState = buildPrevState(gs.players);
  });

  socket.on('buzzer:winner', ({ playerId, playerName }) => {
    buzzerWinName.textContent = playerName;
    buzzerBanner.style.display = 'block';
    // Auto-hide after 5 s
    clearTimeout(buzzerBanner._timer);
    buzzerBanner._timer = setTimeout(() => { buzzerBanner.style.display = 'none'; }, 5000);
    // Highlight winner portrait
    document.querySelectorAll('.portrait').forEach((el) => {
      el.classList.toggle('winner', el.dataset.id === playerId);
    });
  });

  socket.on('buzzer:reset', () => {
    buzzerBanner.style.display = 'none';
    document.querySelectorAll('.portrait').forEach((el) => el.classList.remove('winner'));
  });

  socket.on('rolesRevealed', (roleData) => {
    revealedRoles = roleData;
    // Update role labels on portraits
    Object.entries(roleData).forEach(([id, role]) => {
      const roleEl = document.getElementById(`role-${id}`);
      if (roleEl) { roleEl.textContent = role; roleEl.style.display = 'block'; }
    });
  });

  // ── Render portraits ──────────────────────────────────────────────────────
  function renderPortraits(gs) {
    const players = Object.values(gs.players || {});
    const count   = players.filter((p) => p.status !== 'DEAD' || true).length; // show all
    const size    = calcPortraitSize(count);

    // Update or create portrait elements
    const existing = new Set(Array.from(grid.children).map((el) => el.dataset.id));
    const current  = new Set(players.map((p) => p.id));

    // Remove departed players
    existing.forEach((id) => {
      if (!current.has(id)) {
        const el = document.getElementById(`portrait-${id}`);
        if (el) el.remove();
      }
    });

    // Add / update
    players.forEach((p) => {
      let el = document.getElementById(`portrait-${p.id}`);
      if (!el) {
        el = createPortrait(p, size, gs);
        grid.appendChild(el);
      } else {
        updatePortrait(el, p, size, gs);
      }
    });
  }

  function calcPortraitSize(count) {
    if (count <= 4)  return 160;
    if (count <= 8)  return 120;
    if (count <= 12) return 96;
    if (count <= 16) return 78;
    return 64;
  }

  function createPortrait(p, size, gs) {
    const el  = document.createElement('div');
    el.id     = `portrait-${p.id}`;
    el.dataset.id = p.id;
    el.className  = `portrait ${(p.status || 'ALIVE').toLowerCase()}`;
    el.style.width = `${size}px`;

    el.innerHTML = `
      <div class="portrait-img-wrap" style="width:${size}px;height:${size}px">
        <img id="img-${p.id}" src="${p.image || '/img/default-avatar.svg'}" width="${size}" height="${size}" alt=""/>
        ${p.status === 'DEAD' ? `<span class="portrait-status-badge badge-dead">💀 DEAD</span>` : ''}
        ${p.status === 'MUTE' ? `<span class="portrait-status-badge badge-mute">🔇 MUTE</span>` : ''}
        <div id="pts-delta-anchor-${p.id}" style="position:absolute;top:0;left:50%;width:0"></div>
      </div>
      <div class="portrait-name" style="max-width:${size}px">${escHtml(p.name)}</div>
      ${gs.activeModules?.points !== false ? `<div class="portrait-pts">${p.points || 0} pts</div>` : ''}
      <div class="portrait-role" id="role-${p.id}" style="max-width:${size}px;display:${gs.showAllRoles && revealedRoles[p.id] ? 'block' : 'none'}">
        ${escHtml(revealedRoles[p.id] || '')}
      </div>
    `;
    return el;
  }

  function updatePortrait(el, p, size, gs) {
    const st = (p.status || 'ALIVE').toLowerCase();
    el.className = `portrait ${st}`;

    // Image
    const img = document.getElementById(`img-${p.id}`);
    if (img && img.src !== p.image && p.image) {
      img.src = p.image;
      img.width  = size;
      img.height = size;
    }

    // Points delta animation
    const prevPts = prevState[p.id]?.points ?? p.points;
    const delta   = (p.points || 0) - prevPts;
    if (delta !== 0) {
      const anchor = document.getElementById(`pts-delta-anchor-${p.id}`);
      if (anchor) {
        const span = document.createElement('span');
        span.className = `pts-delta ${delta > 0 ? 'pos' : 'neg'}`;
        span.textContent = (delta > 0 ? '+' : '') + delta;
        anchor.appendChild(span);
        setTimeout(() => span.remove(), 1900);
      }
    }

    // Update pts display
    const ptsEl = el.querySelector('.portrait-pts');
    if (ptsEl) ptsEl.textContent = `${p.points || 0} pts`;

    // Role
    const roleEl = document.getElementById(`role-${p.id}`);
    if (roleEl) {
      const roleText = revealedRoles[p.id] || '';
      const show = gs.showAllRoles && roleText;
      roleEl.style.display = show ? 'block' : 'none';
      roleEl.textContent = escHtml(roleText);
    }
  }

  // ── Render buzzer state ───────────────────────────────────────────────────
  function renderBuzzer(gs) {
    if (!gs.buzzerLocked || !gs.buzzerWinner) {
      buzzerBanner.style.display = 'none';
    }
  }

  // ── Render vote results ───────────────────────────────────────────────────
  function renderVotes(gs) {
    if (!gs.revealedVotes || !gs.votesRevealed) {
      votePanel.style.display = 'none';
      return;
    }
    votePanel.style.display = 'block';
    const max = gs.revealedVotes[0]?.votes || 1;
    voteResults.innerHTML = gs.revealedVotes.map((r) => `
      <div class="vote-result-item">
        <span style="min-width:90px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escHtml(r.playerName)}</span>
        <div class="vote-bar-wrap"><div class="vote-bar-fill" style="width:${Math.round((r.votes/max)*100)}%"></div></div>
        <span style="min-width:20px;text-align:right">${r.votes}</span>
      </div>
    `).join('');
  }

  // ── Helpers ───────────────────────────────────────────────────────────────
  function buildPrevState(players) {
    const result = {};
    Object.values(players || {}).forEach((p) => { result[p.id] = { points: p.points }; });
    return result;
  }

  function escHtml(s) {
    return String(s).replace(/[<>"'&]/g, (c) =>
      ({ '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;', '&': '&amp;' }[c])
    );
  }
})();
