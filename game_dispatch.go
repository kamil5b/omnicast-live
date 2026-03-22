package main

import "encoding/json"

// dispatch routes an inbound WebSocket message to the correct handler.
func (g *GameState) dispatch(h *Hub, c *Client, raw []byte) {
	var m inMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		return
	}

	switch m.str("type") {

	// ── Join ─────────────────────────────────────────────────────────────────
	case "player:join":
		g.handlePlayerJoin(h, c, m)
	case "gm:join":
		g.handleGMJoin(h, c)
	case "operator:join":
		g.handleOperatorJoin(h, c)
	case "overlay:join":
		g.handleOverlayJoin(h, c)

	// ── Player ───────────────────────────────────────────────────────────────
	case "player:buzz":
		g.handleBuzz(h, c)
	case "player:vote":
		g.handleVote(h, c, m)

	// ── GM ───────────────────────────────────────────────────────────────────
	case "gm:setPoints":
		g.handleSetPoints(h, c, m)
	case "gm:setStatus":
		g.handleSetStatus(h, c, m)
	case "gm:assignRole":
		g.handleAssignRole(h, c, m)
	case "gm:assignRoleChecklist":
		g.handleAssignRoleChecklist(h, c, m)
	case "gm:revealRoleForPlayer":
		g.handleRevealRoleForPlayer(h, c, m)
	case "gm:setRoleDefinitions":
		g.handleSetRoleDefinitions(h, c, m)
	case "gm:randomizeRoles":
		g.handleRandomizeRoles(h, c, m)
	case "gm:resetRoles":
		g.handleResetRoles(h, c)
	case "gm:messagePlayer":
		g.handleGMMessage(h, c, m)
	case "gm:enableBuzzers":
		g.handleResetBuzzer(h, c, "buzzer:enabled")
	case "gm:resetBuzzer":
		g.handleResetBuzzer(h, c, "buzzer:reset")
	case "gm:disableBuzzerForPlayer":
		g.handleDisableBuzzerForPlayer(h, c, m)
	case "gm:openVoting":
		g.handleOpenVoting(h, c)
	case "gm:closeVoting":
		g.handleCloseVoting(h, c)
	case "gm:revealVotes":
		g.handleRevealVotes(h, c)
	case "gm:showAllRoles":
		if c.role == "gm" {
			g.setShowAllRoles(h, m.boolVal("show"))
		}
	case "gm:removePlayer":
		g.handleRemovePlayer(h, c, m)
	case "gm:resetScores":
		if c.role == "gm" {
			g.resetAllScores(h)
		}
	case "gm:loadTemplate":
		g.handleLoadTemplate(h, c, m)
	case "gm:setModules":
		g.handleSetModules(h, c, m)

	// ── Operator ─────────────────────────────────────────────────────────────
	case "operator:overrideImage":
		g.handleOverrideImage(h, c, m)
	case "operator:overrideImageUpload":
		g.handleOverrideImageUpload(h, c, m)
	case "operator:showAllRoles":
		if c.role == "operator" {
			g.setShowAllRoles(h, m.boolVal("show"))
		}
	case "operator:resetScores":
		if c.role == "operator" {
			g.resetAllScores(h)
		}
	}
}
