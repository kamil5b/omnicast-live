package main

func (g *GameState) handleBuzz(h *Hub, c *Client) {
	if c.role != "player" {
		return
	}
	g.mu.Lock()
	p := g.players[c.playerID]
	if p == nil || !p.BuzzerEnabled || g.buzzerLocked || !g.activeModules.Buzzer {
		g.mu.Unlock()
		return
	}
	g.buzzerLocked = true
	g.buzzerWinner = c.playerID
	pName := p.Name
	g.mu.Unlock()

	g.broadcastPublicState(h)
	h.broadcastAll(map[string]interface{}{
		"type":       "buzzer:winner",
		"playerId":   c.playerID,
		"playerName": pName,
	})
}

func (g *GameState) handleVote(h *Hub, c *Client, m inMsg) {
	if c.role != "player" {
		return
	}
	targetID := m.str("targetId")
	g.mu.Lock()
	if !g.votingOpen || !g.activeModules.Voting || g.players[targetID] == nil {
		g.mu.Unlock()
		return
	}
	g.votes[c.playerID] = targetID
	gmSnap := g.gmState()
	g.mu.Unlock()

	h.forEachRole("gm", func(gc *Client) { h.deliver(gc, gmSnap) })
	h.deliver(c, map[string]interface{}{"type": "vote:confirmed", "targetId": targetID})
}

// onDisconnect marks a player as offline when their connection drops.
func (g *GameState) onDisconnect(h *Hub, c *Client) {
	if c.role != "player" || c.playerID == "" {
		return
	}
	g.mu.Lock()
	if p := g.players[c.playerID]; p != nil {
		p.Connected = false
	}
	g.mu.Unlock()
	g.broadcastPublicState(h)
}
