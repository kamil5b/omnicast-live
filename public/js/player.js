/* jshint browser:true */
"use strict";

(function () {
    // ── Wake Lock ──────────────────────────────────────────────────────────────
    let wakeLock = null;
    async function requestWakeLock() {
        try {
            if ("wakeLock" in navigator)
                wakeLock = await navigator.wakeLock.request("screen");
        } catch (_) {
            /* ignore */
        }
    }
    requestWakeLock();
    document.addEventListener("visibilitychange", () => {
        if (document.visibilityState === "visible") requestWakeLock();
    });

    // ── State ──────────────────────────────────────────────────────────────────
    const STORAGE_KEY = "omnicast_player";
    let myId = null;
    let myState = null;
    let selectedVoteTarget = null;
    let hasVoted = false;
    let lastPoints = 0;
    let socket = null;
    let gmMsgTimer = null;

    // Load ID from hash or localStorage
    const hashId = window.location.hash.replace("#", "");
    let savedProfile = null;
    try {
        savedProfile = JSON.parse(localStorage.getItem(STORAGE_KEY) || "null");
    } catch (_) {}

    myId = hashId || savedProfile?.id || null;
    if (!myId) {
        window.location.href = "/";
        return;
    }

    // ── DOM Refs ──────────────────────────────────────────────────────────────
    const connDot = document.getElementById("conn-dot");
    const connStatus = document.getElementById("conn-status");
    const playerHeader = document.getElementById("player-header");
    const avatarEl = document.getElementById("player-avatar");
    const nameEl = document.getElementById("player-name");
    const ptsEl = document.getElementById("player-pts");
    const ptsChange = document.getElementById("pts-change");
    const statusBadge = document.getElementById("player-status-badge");
    const statusBanner = document.getElementById("status-banner");
    const roleCard = document.getElementById("role-card");
    const roleValue = document.getElementById("role-value");
    const buzzerSection = document.getElementById("buzzer-section");
    const buzzerBtn = document.getElementById("buzzer-btn");
    const buzzerStatus = document.getElementById("buzzer-status");
    const votingSection = document.getElementById("voting-section");
    const voteList = document.getElementById("vote-list");
    const voteConfirm = document.getElementById("vote-confirm-btn");
    const voteStatusEl = document.getElementById("vote-status");
    const voteReveal = document.getElementById("vote-reveal");
    const voteRevealList = document.getElementById("vote-reveal-list");
    const gmMessageOverlay = document.getElementById("gm-message-overlay");
    const gmMessageText = document.getElementById("gm-message-text");

    // ── Socket ────────────────────────────────────────────────────────────────
    socket = io();

    socket.on("connect", () => {
        connDot.className = "conn-dot online";
        connStatus.textContent = "Connected";
        socket.emit("player:join", {
            name: savedProfile?.name || "Player",
            imageUrl: savedProfile?.image || null,
            playerId: myId,
        });
    });

    socket.on("disconnect", () => {
        connDot.className = "conn-dot offline";
        connStatus.textContent = "Reconnecting…";
    });

    socket.on("joined", ({ id }) => {
        myId = id;
        playerHeader.classList.remove("hidden");
    });

    socket.on("playerState", (state) => {
        // Detect a fresh voting session opening before updating myState
        const wasVotingOpen = myState?.votingOpen || false;
        if (!wasVotingOpen && state.votingOpen) {
            hasVoted = false;
            selectedVoteTarget = null;
            voteStatusEl.textContent = "";
            voteConfirm.disabled = true;
        }
        myState = state;
        renderPlayerState(state);
    });

    // Buzzer events (volatile path)
    socket.on("buzzer:winner", ({ playerId, playerName }) => {
        if (playerId === myId) {
            buzzerBtn.textContent = "🎉 YOU BUZZED FIRST!";
            buzzerBtn.className = "buzzer-btn winner";
            toast("You buzzed in first!", "success");
        } else {
            buzzerBtn.textContent = `❌ ${playerName} buzzed first`;
            buzzerBtn.className = "buzzer-btn loser";
            buzzerBtn.disabled = true;
        }
    });

    socket.on("buzzer:reset", () => {
        buzzerBtn.textContent = "⚡ BUZZ IN!";
        buzzerBtn.className = "buzzer-btn";
        buzzerBtn.disabled = false;
        buzzerStatus.textContent = "";
        hasVoted = false;
    });

    socket.on("buzzer:enabled", () => {
        buzzerBtn.textContent = "⚡ BUZZ IN!";
        buzzerBtn.className = "buzzer-btn";
        buzzerBtn.disabled = false;
        buzzerStatus.textContent = "Buzzers enabled!";
        setTimeout(() => {
            buzzerStatus.textContent = "";
        }, 2000);
    });

    socket.on("vote:confirmed", () => {
        hasVoted = true;
        voteStatusEl.textContent = "✅ Vote submitted";
        voteConfirm.disabled = true;
    });

    // ── GM direct message ─────────────────────────────────────────────────────
    socket.on("gm:message", ({ text }) => {
        showGMMessage(text);
    });

    function showGMMessage(text) {
        if (!text) return;

        // Cancel any existing timer
        if (gmMsgTimer) {
            clearTimeout(gmMsgTimer);
            gmMsgTimer = null;
        }

        // Count words: split on whitespace, filter empty
        const wordCount = text
            .trim()
            .split(/\s+/)
            .filter((w) => w.length > 0).length;
        const displayMs = Math.max(3000, wordCount * 5 * 1000); // 5s per word, min 3s

        gmMessageText.textContent = text;
        gmMessageOverlay.classList.add("visible");

        gmMsgTimer = setTimeout(() => {
            gmMessageOverlay.classList.remove("visible");
            gmMsgTimer = null;
        }, displayMs);
    }

    // Tap overlay to dismiss early
    gmMessageOverlay.addEventListener("click", () => {
        if (gmMsgTimer) {
            clearTimeout(gmMsgTimer);
            gmMsgTimer = null;
        }
        gmMessageOverlay.classList.remove("visible");
    });

    // ── Render ────────────────────────────────────────────────────────────────
    function renderPlayerState(s) {
        // Basic info
        nameEl.textContent = s.name || "—";
        if (s.image) avatarEl.src = s.image;

        // Points animation
        const newPts = s.points || 0;
        if (lastPoints !== newPts) {
            const diff = newPts - lastPoints;
            ptsChange.textContent = (diff > 0 ? "+" : "") + diff;
            ptsChange.style.color =
                diff > 0 ? "var(--success)" : "var(--danger)";
            setTimeout(() => {
                ptsChange.textContent = "";
            }, 2000);
            lastPoints = newPts;
        }
        ptsEl.textContent = `${newPts} pts`;

        // Status badge
        const st = (s.status || "ALIVE").toUpperCase();
        statusBadge.className = `badge badge-${st.toLowerCase()}`;
        statusBadge.textContent = st;

        statusBanner.className = "status-banner";
        statusBanner.textContent = "";
        if (st === "DEAD") {
            statusBanner.textContent = "💀 You are DEAD — spectator mode";
            statusBanner.classList.add("dead");
        } else if (st === "MUTE") {
            statusBanner.textContent = "🔇 You are MUTED";
            statusBanner.classList.add("mute");
        }

        // Role card
        if (s.activeModules?.roles !== false) {
            roleCard.style.display = "block";
            roleValue.textContent = s.role || "(No role assigned)";
        } else {
            roleCard.style.display = "none";
        }

        // Buzzer section
        if (s.activeModules?.buzzer !== false) {
            buzzerSection.style.display = "block";
            if (!s.buzzerEnabled) {
                buzzerBtn.disabled = true;
                buzzerStatus.textContent = "Your buzzer is disabled";
            } else if (s.buzzerLocked && s.buzzerWinner !== myId) {
                buzzerBtn.disabled = true;
                buzzerStatus.textContent = "Locked — another player buzzed";
            } else if (!s.buzzerLocked) {
                buzzerBtn.disabled = false;
                buzzerBtn.className = "buzzer-btn";
                buzzerBtn.textContent = "⚡ BUZZ IN!";
            }
        } else {
            buzzerSection.style.display = "none";
        }

        // Voting section — always rebuilt from the player state's votingPlayers
        if (s.activeModules?.voting !== false && s.votingOpen) {
            votingSection.style.display = "block";
            buildVoteList(s.votingPlayers || []);
        } else {
            votingSection.style.display = "none";
        }

        // Vote results
        if (s.revealedVotes) {
            voteReveal.style.display = "block";
            renderVoteResults(s.revealedVotes);
        } else {
            voteReveal.style.display = "none";
        }
    }

    function renderVoteResults(results) {
        const max = results[0]?.votes || 1;
        voteRevealList.innerHTML = results
            .map(
                (r) => `
      <div class="vote-reveal-item">
        <span style="min-width:100px;font-weight:600">${escHtml(r.playerName)}</span>
        <div class="vote-bar"><div class="vote-bar-fill" style="width:${Math.round((r.votes / max) * 100)}%"></div></div>
        <span style="min-width:30px;text-align:right">${r.votes}</span>
      </div>
    `,
            )
            .join("");
    }

    // ── Voting UI ─────────────────────────────────────────────────────────────
    // votingPlayers is now an array of { id, name, image } sent in playerState.
    // No longer depends on a separate gameState broadcast.
    function buildVoteList(players) {
        if (!players) return;

        // Preserve previous selection across re-renders
        voteList.innerHTML = "";

        players.forEach((p) => {
            const div = document.createElement("div");
            div.className =
                "vote-target" +
                (p.id === selectedVoteTarget ? " selected" : "");
            div.dataset.id = p.id;
            div.innerHTML = `<img src="${p.image || "/img/default-avatar.svg"}" alt=""/><span class="vote-name">${escHtml(p.name)}</span>`;
            div.addEventListener("click", () => {
                if (hasVoted) return;
                selectedVoteTarget = p.id;
                document
                    .querySelectorAll(".vote-target")
                    .forEach((el) => el.classList.remove("selected"));
                div.classList.add("selected");
                voteConfirm.disabled = false;
            });
            voteList.appendChild(div);
        });

        // If no other players are available yet
        if (!players.length) {
            voteList.innerHTML =
                '<p style="color:var(--text-muted);font-size:.85rem;text-align:center">No other players to vote for.</p>';
            voteConfirm.disabled = true;
        }

        // Restore confirm button state
        if (hasVoted) {
            voteConfirm.disabled = true;
        } else if (
            selectedVoteTarget &&
            players.some((p) => p.id === selectedVoteTarget)
        ) {
            voteConfirm.disabled = false;
        }
    }

    voteConfirm.addEventListener("click", () => {
        if (!selectedVoteTarget || hasVoted) return;
        socket.emit("player:vote", { targetId: selectedVoteTarget });
    });

    // ── Privacy Shield (role reveal) ──────────────────────────────────────────
    let holdTimer = null;

    function startReveal(e) {
        e.preventDefault();
        holdTimer = setTimeout(() => {
            roleValue.classList.add("revealed");
        }, 0);
    }

    function endReveal() {
        roleValue.classList.remove("revealed");
        clearTimeout(holdTimer);
    }

    roleValue.addEventListener("mousedown", startReveal);
    roleValue.addEventListener("touchstart", startReveal, { passive: false });
    roleValue.addEventListener("mouseup", endReveal);
    roleValue.addEventListener("mouseleave", endReveal);
    roleValue.addEventListener("touchend", endReveal);
    roleValue.addEventListener("touchcancel", endReveal);

    // ── Buzzer press (volatile event) ─────────────────────────────────────────
    buzzerBtn.addEventListener("click", () => {
        if (!socket.connected) return;
        socket.volatile.emit("player:buzz");
        buzzerBtn.disabled = true;
        buzzerBtn.textContent = "⏳ Waiting…";
    });

    // toast and escHtml are provided globally by ws-client.js
})();
