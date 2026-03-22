/**
 * ws-client.js — Socket.IO-compatible WebSocket wrapper for OmniCast.
 * Provides window.io() that returns a socket object with the same API
 * surface used by the OmniCast client scripts (on/emit/volatile.emit).
 *
 * Protocol: every message is a JSON object with a required "type" field.
 *   Send:    { type: "player:join", name: "Alice", ... }
 *   Receive: { type: "gameState", players: {...}, ... }
 * The "type" field is stripped before handing data to on() handlers,
 * matching the Socket.IO arg-style: socket.on("gameState", (data) => …)
 */
(function (global) {
    "use strict";

    function createSocket() {
        var handlers = {};
        var ws = null;
        var reconnectTimer = null;
        var reconnectDelay = 1500;

        var socket = {
            connected: false,

            /** Emit an event (message) to the server. */
            emit: function (type, data) {
                if (!ws || ws.readyState !== WebSocket.OPEN) return;
                var payload = Object.assign({}, data || {}, { type: type });
                ws.send(JSON.stringify(payload));
            },

            /** Register a handler for an inbound event type. */
            on: function (type, fn) {
                if (!handlers[type]) handlers[type] = [];
                handlers[type].push(fn);
            },

            /** Best-effort emit (plain WS has no volatile semantics; same as emit). */
            volatile: {
                emit: function (type, data) {
                    socket.emit(type, data);
                },
            },
        };

        function dispatch(type, data) {
            var fns = handlers[type] || [];
            for (var i = 0; i < fns.length; i++) {
                try {
                    fns[i](data);
                } catch (e) {
                    console.error("[ws-client]", e);
                }
            }
        }

        function connect() {
            var proto = location.protocol === "https:" ? "wss:" : "ws:";
            ws = new WebSocket(proto + "//" + location.host + "/ws");

            ws.addEventListener("open", function () {
                socket.connected = true;
                reconnectDelay = 1500;
                clearTimeout(reconnectTimer);
                dispatch("connect", {});
            });

            ws.addEventListener("close", function () {
                socket.connected = false;
                dispatch("disconnect", {});
                reconnectTimer = setTimeout(connect, reconnectDelay);
                reconnectDelay = Math.min(reconnectDelay * 1.5, 10000);
            });

            ws.addEventListener("error", function () {
                dispatch("connect_error", {});
            });

            ws.addEventListener("message", function (event) {
                // A single WebSocket frame may carry multiple newline-delimited
                // JSON objects (batched by the writePump).
                var lines = event.data.split("\n");
                for (var i = 0; i < lines.length; i++) {
                    var line = lines[i].trim();
                    if (!line) continue;
                    try {
                        var msg = JSON.parse(line);
                        var type = msg.type;
                        if (!type) continue;
                        // Build data object without the type field
                        var data = {};
                        for (var key in msg) {
                            if (
                                Object.prototype.hasOwnProperty.call(
                                    msg,
                                    key,
                                ) &&
                                key !== "type"
                            ) {
                                data[key] = msg[key];
                            }
                        }
                        dispatch(type, data);
                    } catch (e) {
                        console.warn("[ws-client] bad message", line, e);
                    }
                }
            });
        }

        connect();
        return socket;
    }

    /** Mimics Socket.IO's io() factory. */
    global.io = function () {
        return createSocket();
    };

    // ── Shared UI helpers ─────────────────────────────────────────────────────

    /**
     * Display a transient toast notification.
     * @param {string} msg  - Text to show.
     * @param {string} type - Optional CSS modifier: "success" | "error" | "info"
     */
    global.toast = function (msg, type) {
        var container = document.getElementById("toast-container");
        if (!container) return;
        var el = document.createElement("div");
        el.className = "toast" + (type ? " " + type : "");
        el.textContent = msg;
        container.appendChild(el);
        setTimeout(function () {
            el.remove();
        }, 3000);
    };

    /**
     * Escape HTML special characters to prevent XSS when injecting into innerHTML.
     * @param {*} s - Value to escape (coerced to string).
     * @returns {string}
     */
    global.escHtml = function (s) {
        return String(s).replace(/[<>"'&]/g, function (c) {
            return {
                "<": "&lt;",
                ">": "&gt;",
                '"': "&quot;",
                "'": "&#39;",
                "&": "&amp;",
            }[c];
        });
    };
})(window);
