/* jshint browser:true */
'use strict';

(function () {
  // Wake Lock API
  let wakeLock = null;
  async function requestWakeLock() {
    try {
      if ('wakeLock' in navigator) {
        wakeLock = await navigator.wakeLock.request('screen');
      }
    } catch (_) { /* ignore */ }
  }
  requestWakeLock();
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') requestWakeLock();
  });

  const STORAGE_KEY = 'omnicast_player';
  const fileInput   = document.getElementById('file-input');
  const preview     = document.getElementById('avatar-preview');
  const nameInput   = document.getElementById('player-name');
  const joinBtn     = document.getElementById('join-btn');
  const statusMsg   = document.getElementById('status-msg');
  const reconnInfo  = document.getElementById('reconnect-info');

  let uploadedImageUrl = null;
  let savedProfile = null;

  // Load saved profile
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) savedProfile = JSON.parse(raw);
  } catch (_) { /* ignore */ }

  if (savedProfile) {
    nameInput.value = savedProfile.name || '';
    if (savedProfile.image) {
      preview.src = savedProfile.image;
      uploadedImageUrl = savedProfile.image;
    }
    reconnInfo.style.display = 'block';
  }

  // Preview selected image
  fileInput.addEventListener('change', async () => {
    const file = fileInput.files[0];
    if (!file) return;
    // Show local preview immediately
    const reader = new FileReader();
    reader.onload = (e) => { preview.src = e.target.result; };
    reader.readAsDataURL(file);

    // Upload to server
    statusMsg.textContent = 'Uploading photo…';
    const formData = new FormData();
    formData.append('image', file);
    try {
      const res  = await fetch('/upload', { method: 'POST', body: formData });
      const data = await res.json();
      if (data.url) {
        uploadedImageUrl = data.url;
        statusMsg.textContent = 'Photo uploaded ✓';
      } else {
        statusMsg.textContent = data.error || 'Upload failed';
      }
    } catch (e) {
      statusMsg.textContent = 'Upload error: ' + e.message;
    }
  });

  joinBtn.addEventListener('click', joinGame);
  nameInput.addEventListener('keydown', (e) => { if (e.key === 'Enter') joinGame(); });

  function joinGame() {
    const name = nameInput.value.trim();
    if (!name) { toast('Please enter your name', 'error'); nameInput.focus(); return; }

    joinBtn.disabled = true;
    statusMsg.textContent = 'Connecting…';

    const socket = io();
    socket.on('connect', () => {
      const payload = {
        name,
        imageUrl: uploadedImageUrl || (savedProfile?.image) || null,
        playerId: savedProfile?.id || null,
      };
      socket.emit('player:join', payload);
    });

    socket.on('joined', ({ id }) => {
      // Save profile locally
      const profile = { id, name, image: uploadedImageUrl || savedProfile?.image || null };
      localStorage.setItem(STORAGE_KEY, JSON.stringify(profile));
      // Redirect to player view with ID in hash
      window.location.href = `/player.html#${id}`;
    });

    socket.on('connect_error', () => {
      statusMsg.textContent = 'Connection failed. Check your network.';
      joinBtn.disabled = false;
    });
  }

  // ── Toast helper ──────────────────────────────────────────────────────────
  function toast(msg, type = '') {
    const c = document.getElementById('toast-container');
    const t = document.createElement('div');
    t.className = 'toast ' + type;
    t.textContent = msg;
    c.appendChild(t);
    setTimeout(() => t.remove(), 3000);
  }
})();
