/* jshint browser:true */
"use strict";

(function () {
    const connDot = document.getElementById("conn-dot");
    const connStatus = document.getElementById("conn-status");
    const playersGrid = document.getElementById("players-grid");
    const scoreList = document.getElementById("score-list");
    const noPlayers = document.getElementById("no-players");
    const gameStatusEl = document.getElementById("game-status");

    const socket = io();

    socket.on("connect", () => {
        connDot.className = "conn-dot online";
        connStatus.textContent = "Connected as Operator";
        socket.emit("operator:join");
    });
    socket.on("disconnect", () => {
        connDot.className = "conn-dot offline";
        connStatus.textContent = "Disconnected";
    });

    socket.on("gameState", (gs) => renderAll(gs));

    // ── Render ────────────────────────────────────────────────────────────────
    function renderAll(gs) {
        const players = Object.values(gs.players || {});
        noPlayers.style.display = players.length ? "none" : "block";

        renderScoreboard(players, gs.activeModules);
        renderPlayers(players);

        const parts = [];
        if (gs.buzzerLocked)
            parts.push(
                `Buzzer locked — winner: ${gs.players[gs.buzzerWinner]?.name || "?"}`,
            );
        if (gs.votingOpen) parts.push("Voting OPEN");
        if (gs.showAllRoles) parts.push("Roles REVEALED on OBS");
        gameStatusEl.textContent = parts.join(" · ") || "Standby";
    }

    function renderScoreboard(players, modules) {
        if (!modules?.points) {
            scoreList.innerHTML =
                '<span style="color:var(--text-muted);font-size:.85rem">Points module disabled</span>';
            return;
        }
        const sorted = [...players].sort(
            (a, b) => (b.points || 0) - (a.points || 0),
        );
        scoreList.innerHTML = sorted
            .map(
                (p, i) => `
      <div class="score-item">
        <span class="score-rank">${i + 1}</span>
        <img src="${p.image || "/img/default-avatar.svg"}" alt=""/>
        <span style="flex:1;font-weight:600">${escHtml(p.name)}</span>
        <span style="font-weight:700;color:var(--accent)">${p.points || 0}</span>
      </div>
    `,
            )
            .join("");
    }

    function renderPlayers(players) {
        playersGrid.innerHTML = "";
        players.forEach((p) => {
            const card = document.createElement("div");
            const st = (p.status || "ALIVE").toLowerCase();
            card.className = `player-card ${st}`;
            card.innerHTML = `
        <div class="player-card-top">
          <img class="player-card-avatar" id="av-${p.id}" src="${p.image || "/img/default-avatar.svg"}" alt=""/>
          <div>
            <div class="player-card-name">${escHtml(p.name)}</div>
            <span class="badge badge-${st}">${st.toUpperCase()}</span>
          </div>
        </div>
        <div class="override-form">
          <label>Image URL</label>
          <input type="text" id="url-${p.id}" placeholder="https://…/art.jpg" value=""/>
          <button class="btn btn-sm btn-blue" data-action="url" data-id="${p.id}">Apply URL</button>
          <label>Upload File</label>
          <div class="file-upload-row">
            <input type="file" id="file-${p.id}" accept="image/*"/>
            <button class="btn btn-sm btn-blue" data-action="file" data-id="${p.id}">Upload</button>
          </div>
        </div>
      `;
            playersGrid.appendChild(card);
        });
    }

    // ── Player grid actions (registered once at init) ─────────────────────────
    playersGrid.addEventListener("click", async (e) => {
        const btn = e.target.closest("[data-action]");
        if (!btn) return;
        const { action, id } = btn.dataset;

        if (action === "url") {
            const url = document.getElementById(`url-${id}`).value.trim();
            if (!url) {
                toast("Enter a URL", "error");
                return;
            }
            socket.emit("operator:overrideImage", {
                playerId: id,
                imageUrl: url,
            });
            toast("Image updated", "success");
        }

        if (action === "file") {
            const fileInput = document.getElementById(`file-${id}`);
            const file = fileInput.files[0];
            if (!file) {
                toast("Select a file first", "error");
                return;
            }
            const fd = new FormData();
            fd.append("image", file);
            try {
                const res = await fetch("/upload", {
                    method: "POST",
                    body: fd,
                });
                const data = await res.json();
                if (data.filename) {
                    socket.emit("operator:overrideImageUpload", {
                        playerId: id,
                        filename: data.filename,
                    });
                    toast("Image updated", "success");
                } else {
                    toast(data.error || "Upload failed", "error");
                }
            } catch (err) {
                toast("Upload error: " + err.message, "error");
            }
        }
    });

    // ── Global actions ────────────────────────────────────────────────────────
    document
        .getElementById("show-roles-btn")
        .addEventListener("click", () =>
            socket.emit("operator:showAllRoles", { show: true }),
        );
    document
        .getElementById("hide-roles-btn")
        .addEventListener("click", () =>
            socket.emit("operator:showAllRoles", { show: false }),
        );
    document
        .getElementById("reset-scores-btn")
        .addEventListener("click", () => {
            if (confirm("Reset all scores to 0?"))
                socket.emit("operator:resetScores");
        });

    // ── Helpers ───────────────────────────────────────────────────────────────
    // toast and escHtml are provided globally by ws-client.js
})();
