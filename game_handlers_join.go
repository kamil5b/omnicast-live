package main

// handlePlayerJoin processes a player:join event.
// If the player already exists (reconnect), their points/status are preserved.
func (g *GameState) handlePlayerJoin(h *Hub, c *Client, m inMsg) {
	name := sanitize(m.str("name"))
	imageURL := m.str("imageUrl")
	reqID := m.str("playerId")

	g.mu.Lock()
	id := reqID
	if id == "" {
		id = newUUID()
	}
	existing := g.players[id]
	pts := 0
	status := "ALIVE"
	buzzerEnabled := true
	image := imageURL
	if existing != nil {
		pts = existing.Points
		status = existing.Status
		buzzerEnabled = existing.BuzzerEnabled
		if image == "" {
			image = existing.Image
		}
	}
	if image == "" {
		image = "/img/default-avatar.svg"
	}
	if name == "" {
		name = "Player " + id[:4]
	}
	g.players[id] = &player{
		ID:            id,
		Name:          name,
		Image:         image,
		Points:        pts,
		Status:        status,
		BuzzerEnabled: buzzerEnabled,
		Connected:     true,
	}
	g.mu.Unlock()

	c.role = "player"
	c.playerID = id
	h.deliver(c, map[string]interface{}{"type": "joined", "id": id, "role": "player"})
	g.broadcastPublicState(h)
}

func (g *GameState) handleGMJoin(h *Hub, c *Client) {
	c.role = "gm"
	g.mu.Lock()
	state := g.gmState()
	g.mu.Unlock()
	h.deliver(c, map[string]interface{}{"type": "joined", "role": "gm"})
	h.deliver(c, state)
}

func (g *GameState) handleOperatorJoin(h *Hub, c *Client) {
	c.role = "operator"
	g.mu.Lock()
	state := g.publicState()
	g.mu.Unlock()
	h.deliver(c, map[string]interface{}{"type": "joined", "role": "operator"})
	h.deliver(c, state)
}

func (g *GameState) handleOverlayJoin(h *Hub, c *Client) {
	c.role = "overlay"
	g.mu.Lock()
	state := g.publicState()
	g.mu.Unlock()
	h.deliver(c, state)
}
