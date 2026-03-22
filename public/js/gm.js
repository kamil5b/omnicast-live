/* jshint browser:true */
"use strict";

(function () {
    let socket = null;
    let gameState = null;

    // ── DOM refs ──────────────────────────────────────────────────────────────
    const connDot = document.getElementById("conn-dot");
    const connStatus = document.getElementById("conn-status");
    const playersGrid = document.getElementById("players-grid");
    const playerCount = document.getElementById("player-count");
    const noPlayers = document.getElementById("no-players");
    const templateGrid = document.getElementById("template-grid");
    const qrPanel = document.getElementById("qr-panel");
    const qrImg = document.getElementById("qr-img");
    const joinUrlEl = document.getElementById("join-url");
    const voteTally = document.getElementById("vote-tally");
    const voteTallyCard = document.getElementById("vote-tally-card");
    const roleDefList = document.getElementById("role-def-list");
    const roleDefEmpty = document.getElementById("role-def-empty");
    const roleChecklistWrap = document.getElementById("role-checklist-wrap");

    // Message modal
    const msgModal = document.getElementById("msg-modal");
    const msgModalName = document.getElementById("msg-modal-player-name");
    const msgModalText = document.getElementById("msg-modal-text");
    let msgTargetId = null;

    // ── Local role definitions state ──────────────────────────────────────────
    // Kept in sync from gameState.roleDefinitions; also editable locally before push.
    let localRoleDefs = []; // [{ name, max }]

    // ── Socket ────────────────────────────────────────────────────────────────
    socket = io();

    socket.on("connect", () => {
        connDot.className = "conn-dot online";
        connStatus.textContent = "Connected as GM";
        socket.emit("gm:join");
    });

    socket.on("disconnect", () => {
        connDot.className = "conn-dot offline";
        connStatus.textContent = "Disconnected";
    });

    socket.on("gameState", (gs) => {
        gameState = gs;
        renderAll(gs);
    });

    // ── Render ────────────────────────────────────────────────────────────────
    function renderAll(gs) {
        renderModuleToggles(gs.activeModules);
        renderPlayers(
            gs.players,
            gs.votes,
            gs.activeModules,
            gs.revealedRoles || {},
        );
        renderVoteTally(gs.votes, gs.players);
        renderRolePlayerSelect(gs.players);
        renderRoleDefinitions(
            gs.roleDefinitions || [],
            gs.players || {},
            gs.roles || {},
        );
        renderRoleChecklist(
            gs.players || {},
            gs.roleDefinitions || [],
            gs.roles || {},
        );
        updateActiveTemplate(gs.template);
        const count = Object.keys(gs.players || {}).length;
        playerCount.textContent = count;
        noPlayers.style.display = count ? "none" : "block";
        voteTallyCard.style.display = gs.votingOpen ? "block" : "none";
    }

    // ── Players grid ──────────────────────────────────────────────────────────
    function renderPlayers(players, votes, modules, revealedRoles) {
        if (!players) return;
        playersGrid.innerHTML = "";

        Object.values(players).forEach((p) => {
            const card = document.createElement("div");
            const st = (p.status || "ALIVE").toLowerCase();
            card.className = `player-card ${st}`;

            const isRoleRevealed = !!(revealedRoles && revealedRoles[p.id]);
            const playerUrl = buildPlayerUrl(p.id);

            card.innerHTML = `
        <div class="player-card-top">
          <img class="player-card-avatar" src="${p.image || "/img/default-avatar.svg"}" alt=""/>
          <div style="flex:1;min-width:0">
            <div class="player-card-name">${escHtml(p.name)}</div>
            ${modules?.points !== false ? `<div class="player-card-pts">${p.points || 0} pts</div>` : ""}
          </div>
          <span class="badge badge-${st}" style="margin-left:auto;flex-shrink:0">${st.toUpperCase()}</span>
        </div>
        ${modules?.roles !== false ? `<div class="player-card-role">🎭 ${escHtml(p.role || "—")}</div>` : ""}
        <div class="player-card-status">${p.connected ? "🟢 Online" : "🔴 Offline"} | Buzzer: ${p.buzzerEnabled ? "✅" : "❌"}</div>
        <div class="player-link-row">
          <span style="color:var(--text-muted);flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${escHtml(playerUrl)}">${escHtml(playerUrl)}</span>
          <button class="btn btn-sm" data-action="copylink" data-id="${p.id}" title="Copy player link">📋</button>
        </div>
        <div class="player-actions">
          ${
              modules?.points !== false
                  ? `
            <button class="btn btn-sm btn-green" data-action="pts" data-id="${p.id}" data-delta="1">+1</button>
            <button class="btn btn-sm btn-green" data-action="pts" data-id="${p.id}" data-delta="5">+5</button>
            <button class="btn btn-sm btn-red"   data-action="pts" data-id="${p.id}" data-delta="-1">-1</button>
            <button class="btn btn-sm btn-red"   data-action="pts" data-id="${p.id}" data-delta="-5">-5</button>
          `
                  : ""
          }
          ${
              modules?.status !== false
                  ? `
            <button class="btn btn-sm" data-action="status" data-id="${p.id}" data-status="ALIVE">Alive</button>
            <button class="btn btn-sm btn-red" data-action="status" data-id="${p.id}" data-status="DEAD">Dead</button>
            <button class="btn btn-sm" data-action="status" data-id="${p.id}" data-status="MUTE">Mute</button>
          `
                  : ""
          }
          ${
              modules?.buzzer !== false
                  ? `
            <button class="btn btn-sm" data-action="buzzer" data-id="${p.id}" data-enabled="${p.buzzerEnabled ? "false" : "true"}">
              ${p.buzzerEnabled ? "🔕 Disable Buzz" : "🔔 Enable Buzz"}
            </button>
          `
                  : ""
          }
          ${
              modules?.roles !== false
                  ? `
            <button class="btn btn-sm ${isRoleRevealed ? "btn-orange" : ""}" data-action="revealrole" data-id="${p.id}" data-reveal="${isRoleRevealed ? "false" : "true"}" title="${isRoleRevealed ? "Hide role from overlay" : "Reveal role to overlay"}">
              ${isRoleRevealed ? "🙈 Hide from Overlay" : "👁 Show on Overlay"}
            </button>
          `
                  : ""
          }
          <button class="btn btn-sm btn-blue" data-action="message" data-id="${p.id}" data-name="${escHtml(p.name)}">💬 Message</button>
          <button class="btn btn-sm btn-red" data-action="remove" data-id="${p.id}">✕ Remove</button>
        </div>
      `;
            playersGrid.appendChild(card);
        });

        playersGrid.onclick = (e) => {
            const btn = e.target.closest("[data-action]");
            if (!btn) return;
            const { action, id, delta, status, enabled, reveal, name } =
                btn.dataset;

            if (action === "pts")
                socket.emit("gm:setPoints", {
                    playerId: id,
                    delta: Number(delta),
                });

            if (action === "status")
                socket.emit("gm:setStatus", { playerId: id, status });

            if (action === "buzzer")
                socket.emit("gm:disableBuzzerForPlayer", {
                    playerId: id,
                    enabled: enabled === "true",
                });

            if (action === "revealrole")
                socket.emit("gm:revealRoleForPlayer", {
                    playerId: id,
                    reveal: reveal === "true",
                });

            if (action === "message") openMessageModal(id, name);

            if (action === "copylink") {
                const url = buildPlayerUrl(id);
                navigator.clipboard
                    .writeText(url)
                    .then(() => toast("Player link copied!", "success"))
                    .catch(() => {
                        // fallback
                        const ta = document.createElement("textarea");
                        ta.value = url;
                        ta.style.position = "fixed";
                        ta.style.opacity = "0";
                        document.body.appendChild(ta);
                        ta.select();
                        document.execCommand("copy");
                        ta.remove();
                        toast("Player link copied!", "success");
                    });
            }

            if (action === "remove") {
                const cardName =
                    btn
                        .closest(".player-card")
                        ?.querySelector(".player-card-name")?.textContent || id;
                if (confirm(`Remove ${cardName}?`))
                    socket.emit("gm:removePlayer", { playerId: id });
            }
        };
    }

    function buildPlayerUrl(playerId) {
        return `${location.protocol}//${location.host}/player#${playerId}`;
    }

    // ── Vote tally ────────────────────────────────────────────────────────────
    function renderVoteTally(votes, players) {
        if (!votes || !players) return;
        const tally = {};
        Object.values(votes).forEach((tid) => {
            tally[tid] = (tally[tid] || 0) + 1;
        });
        if (!Object.keys(tally).length) {
            voteTally.innerHTML =
                '<span style="color:var(--text-muted)">No votes yet</span>';
            return;
        }
        voteTally.innerHTML = Object.entries(tally)
            .sort((a, b) => b[1] - a[1])
            .map(
                ([tid, cnt]) => `
        <div class="vote-row">
          <span>${escHtml(players[tid]?.name || tid)}</span>
          <strong>${cnt}</strong>
        </div>`,
            )
            .join("");
    }

    // ── Legacy single-assign player select ───────────────────────────────────
    function renderRolePlayerSelect(players) {
        const sel = document.getElementById("role-player-select");
        const current = sel.value;
        sel.innerHTML = '<option value="">— Select Player —</option>';
        if (!players) return;
        Object.values(players).forEach((p) => {
            const opt = document.createElement("option");
            opt.value = p.id;
            opt.textContent = p.name;
            if (p.id === current) opt.selected = true;
            sel.appendChild(opt);
        });
    }

    // ── Role Definitions panel ────────────────────────────────────────────────
    function renderRoleDefinitions(defs, players, assignedRoles) {
        localRoleDefs = defs.slice();

        // Count current assignments per role
        const counts = {};
        Object.values(assignedRoles || {}).forEach((r) => {
            if (r) counts[r] = (counts[r] || 0) + 1;
        });
        const totalPlayers = Object.keys(players || {}).length;

        if (!defs.length) {
            roleDefEmpty.style.display = "";
            roleDefList.innerHTML = "";
            roleDefList.appendChild(roleDefEmpty);
            return;
        }

        roleDefEmpty.style.display = "none";
        roleDefList.innerHTML = "";

        defs.forEach((def, idx) => {
            const row = document.createElement("div");
            row.className = "role-def-row";
            const maxLabel = def.max > 0 ? `max ${def.max}` : "unlimited";
            const assigned = counts[def.name] || 0;
            row.innerHTML = `
        <span class="role-def-name">${escHtml(def.name)}</span>
        <span class="role-def-max">${maxLabel}</span>
        <span class="role-def-count">${assigned}/${def.max > 0 ? def.max : totalPlayers} assigned</span>
        <button class="btn btn-sm btn-red" data-defidx="${idx}" data-defaction="remove" style="padding:.2rem .4rem;font-size:.7rem">✕</button>
      `;
            roleDefList.appendChild(row);
        });

        roleDefList.onclick = (e) => {
            const btn = e.target.closest("[data-defaction]");
            if (!btn) return;
            if (btn.dataset.defaction === "remove") {
                const idx = Number(btn.dataset.defidx);
                localRoleDefs.splice(idx, 1);
                pushRoleDefinitions();
            }
        };
    }

    function pushRoleDefinitions() {
        socket.emit("gm:setRoleDefinitions", { roles: localRoleDefs });
    }

    // ── Checklist assignment ──────────────────────────────────────────────────
    function renderRoleChecklist(players, defs, assignedRoles) {
        if (!Object.keys(players).length || !defs.length) {
            roleChecklistWrap.innerHTML =
                '<span style="font-size:.8rem;color:var(--text-muted)">' +
                (Object.keys(players).length === 0
                    ? "No players yet."
                    : "No role definitions. Add roles above.") +
                "</span>";
            return;
        }

        // Count assignments per role
        const counts = {};
        Object.values(assignedRoles || {}).forEach((r) => {
            if (r) counts[r] = (counts[r] || 0) + 1;
        });

        roleChecklistWrap.innerHTML = "";

        Object.values(players).forEach((p) => {
            const playerRole = assignedRoles[p.id] || "";

            const col = document.createElement("div");
            col.className = "role-checklist-player";
            col.innerHTML = `
        <div class="role-checklist-player-name">${escHtml(p.name)}</div>
        <div class="role-checklist-roles" data-player-id="${p.id}"></div>
      `;

            const rolesContainer = col.querySelector(".role-checklist-roles");

            defs.forEach((def) => {
                const isChecked = playerRole === def.name;
                // At max if: max > 0, count >= max, AND this player doesn't already have this role
                const atMax =
                    def.max > 0 &&
                    (counts[def.name] || 0) >= def.max &&
                    !isChecked;

                const item = document.createElement("label");
                item.className = "role-check-item" + (atMax ? " at-max" : "");
                item.innerHTML = `
          <input type="radio"
            name="role-${p.id}"
            value="${escHtml(def.name)}"
            ${isChecked ? "checked" : ""}
            ${atMax ? "disabled" : ""}
            data-player-id="${p.id}"
            data-role="${escHtml(def.name)}"
          />
          ${escHtml(def.name)}
          ${def.max > 0 ? `<span style="margin-left:auto;font-size:.7rem;color:var(--text-muted)">${counts[def.name] || 0}/${def.max}</span>` : ""}
        `;
                rolesContainer.appendChild(item);
            });

            // "None" option to unassign
            const noneItem = document.createElement("label");
            noneItem.className = "role-check-item";
            noneItem.innerHTML = `
        <input type="radio"
          name="role-${p.id}"
          value=""
          ${playerRole === "" ? "checked" : ""}
          data-player-id="${p.id}"
          data-role=""
        />
        <em style="color:var(--text-muted)">None</em>
      `;
            rolesContainer.appendChild(noneItem);

            roleChecklistWrap.appendChild(col);
        });

        // Event delegation — one listener on the container
        roleChecklistWrap.onchange = (e) => {
            const input = e.target.closest('input[type="radio"]');
            if (!input) return;
            const pid = input.dataset.playerId;
            const role = input.dataset.role;
            if (role === "") {
                // Unassign: emit checklist with assign=false for current role
                const currentRole = assignedRoles[pid] || "";
                if (currentRole) {
                    socket.emit("gm:assignRoleChecklist", {
                        playerId: pid,
                        role: currentRole,
                        assign: false,
                    });
                }
            } else {
                socket.emit("gm:assignRoleChecklist", {
                    playerId: pid,
                    role,
                    assign: true,
                });
            }
        };
    }

    // ── Module toggles ────────────────────────────────────────────────────────
    function renderModuleToggles(modules) {
        if (!modules) return;
        document.querySelectorAll("[data-module]").forEach((cb) => {
            cb.checked = modules[cb.dataset.module] !== false;
        });
    }

    function updateActiveTemplate(templateName) {
        document.querySelectorAll(".template-btn").forEach((b) => {
            b.classList.toggle("active", b.dataset.tmpl === templateName);
        });
    }

    // ── GM Message modal ──────────────────────────────────────────────────────
    function openMessageModal(playerId, playerName) {
        msgTargetId = playerId;
        msgModalName.textContent = playerName || playerId;
        msgModalText.value = "";
        msgModal.classList.add("open");
        msgModalText.focus();
    }

    document
        .getElementById("msg-modal-cancel")
        .addEventListener("click", () => {
            msgModal.classList.remove("open");
            msgTargetId = null;
        });

    document.getElementById("msg-modal-send").addEventListener("click", () => {
        const text = msgModalText.value.trim();
        if (!text) {
            toast("Message cannot be empty", "error");
            return;
        }
        if (!msgTargetId) return;
        socket.emit("gm:messagePlayer", { playerId: msgTargetId, text });
        toast("Message sent!", "success");
        msgModal.classList.remove("open");
        msgTargetId = null;
    });

    // Allow Ctrl+Enter to send
    msgModalText.addEventListener("keydown", (e) => {
        if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
            document.getElementById("msg-modal-send").click();
        }
    });

    // Close on backdrop click
    msgModal.addEventListener("click", (e) => {
        if (e.target === msgModal) {
            msgModal.classList.remove("open");
            msgTargetId = null;
        }
    });

    // ── Load templates ────────────────────────────────────────────────────────
    fetch("/api/templates")
        .then((r) => r.json())
        .then((templates) => {
            templateGrid.innerHTML = "";
            templates.forEach((t) => {
                const b = document.createElement("button");
                b.className = "btn template-btn";
                b.dataset.tmpl =
                    t.id || t.name.toLowerCase().replace(/\s+/g, "_");
                b.title = t.description || "";
                b.textContent = t.name;
                b.addEventListener("click", () => {
                    socket.emit("gm:loadTemplate", {
                        templateName: b.dataset.tmpl,
                    });
                });
                templateGrid.appendChild(b);
            });
            if (gameState) updateActiveTemplate(gameState.template);
        })
        .catch(() => {});

    // ── Module toggles ────────────────────────────────────────────────────────
    document.getElementById("module-toggles").addEventListener("change", () => {
        const modules = {};
        document.querySelectorAll("[data-module]").forEach((cb) => {
            modules[cb.dataset.module] = cb.checked;
        });
        socket.emit("gm:setModules", { modules });
    });

    // ── Role definition: add ──────────────────────────────────────────────────
    document
        .getElementById("role-def-add-btn")
        .addEventListener("click", () => {
            const nameInput = document.getElementById("role-def-name");
            const maxInput = document.getElementById("role-def-max");
            const name = nameInput.value.trim();
            if (!name) {
                toast("Enter a role name", "error");
                return;
            }
            const max = Math.max(0, parseInt(maxInput.value, 10) || 0);

            // Prevent duplicate names
            if (
                localRoleDefs.some(
                    (d) => d.name.toLowerCase() === name.toLowerCase(),
                )
            ) {
                toast("Role already exists", "error");
                return;
            }

            localRoleDefs.push({ name, max });
            pushRoleDefinitions();
            nameInput.value = "";
            maxInput.value = "";
        });

    document
        .getElementById("role-def-name")
        .addEventListener("keydown", (e) => {
            if (e.key === "Enter")
                document.getElementById("role-def-add-btn").click();
        });

    // ── Randomize roles ───────────────────────────────────────────────────────
    document
        .getElementById("randomize-roles-btn")
        .addEventListener("click", () => {
            socket.emit("gm:randomizeRoles", { resetFirst: false });
            toast("Roles randomized!", "success");
        });

    document
        .getElementById("randomize-reset-btn")
        .addEventListener("click", () => {
            if (
                !confirm(
                    "This will clear all current role assignments and re-randomize. Continue?",
                )
            )
                return;
            socket.emit("gm:randomizeRoles", { resetFirst: true });
            toast("Roles reset and randomized!", "success");
        });

    document.getElementById("reset-roles-btn").addEventListener("click", () => {
        if (!confirm("Clear all role assignments?")) return;
        socket.emit("gm:resetRoles");
        toast("All roles cleared", "success");
    });

    // ── Role assign tabs ──────────────────────────────────────────────────────
    document.querySelectorAll(".role-assign-tab").forEach((tab) => {
        tab.addEventListener("click", () => {
            document
                .querySelectorAll(".role-assign-tab")
                .forEach((t) => t.classList.remove("active"));
            document
                .querySelectorAll(".role-assign-panel")
                .forEach((p) => p.classList.remove("active"));
            tab.classList.add("active");
            document
                .getElementById("tab-" + tab.dataset.tab)
                .classList.add("active");
        });
    });

    // ── Legacy single-assign ──────────────────────────────────────────────────
    document.getElementById("assign-role-btn").addEventListener("click", () => {
        const pid = document.getElementById("role-player-select").value;
        const role = document.getElementById("role-text-input").value.trim();
        if (!pid) {
            toast("Select a player", "error");
            return;
        }
        if (!role) {
            toast("Enter a role", "error");
            return;
        }
        socket.emit("gm:assignRole", { playerId: pid, role });
        document.getElementById("role-text-input").value = "";
        toast("Role assigned!", "success");
    });

    // ── QR Code ───────────────────────────────────────────────────────────────
    document.getElementById("qr-btn").addEventListener("click", () => {
        fetch("/qr")
            .then((r) => r.json())
            .then((d) => {
                qrImg.src = d.qr;
                joinUrlEl.textContent = d.url;
                qrPanel.style.display = "block";
            })
            .catch(() => toast("Could not generate QR", "error"));
    });

    document.getElementById("qr-close-btn").addEventListener("click", () => {
        qrPanel.style.display = "none";
    });

    // ── Global actions ────────────────────────────────────────────────────────
    document
        .getElementById("enable-buzzers-btn")
        .addEventListener("click", () => socket.emit("gm:enableBuzzers"));
    document
        .getElementById("reset-buzzer-btn")
        .addEventListener("click", () => socket.emit("gm:resetBuzzer"));
    document
        .getElementById("open-vote-btn")
        .addEventListener("click", () => socket.emit("gm:openVoting"));
    document
        .getElementById("close-vote-btn")
        .addEventListener("click", () => socket.emit("gm:closeVoting"));
    document
        .getElementById("reveal-votes-btn")
        .addEventListener("click", () => socket.emit("gm:revealVotes"));
    document
        .getElementById("hide-votes-btn")
        .addEventListener("click", () => socket.emit("gm:hideVotes"));
    document
        .getElementById("show-roles-btn")
        .addEventListener("click", () =>
            socket.emit("gm:showAllRoles", { show: true }),
        );
    document
        .getElementById("hide-roles-btn")
        .addEventListener("click", () =>
            socket.emit("gm:showAllRoles", { show: false }),
        );
    document
        .getElementById("reset-scores-btn")
        .addEventListener("click", () => {
            if (confirm("Reset all scores to 0?"))
                socket.emit("gm:resetScores");
        });

    // ── Helpers ───────────────────────────────────────────────────────────────
    function toast(msg, type = "") {
        const c = document.getElementById("toast-container");
        const t = document.createElement("div");
        t.className = "toast " + type;
        t.textContent = msg;
        c.appendChild(t);
        setTimeout(() => t.remove(), 3000);
    }

    function escHtml(s) {
        return String(s).replace(
            /[<>"'&]/g,
            (c) =>
                ({
                    "<": "&lt;",
                    ">": "&gt;",
                    '"': "&quot;",
                    "'": "&#39;",
                    "&": "&amp;",
                })[c],
        );
    }
})();
