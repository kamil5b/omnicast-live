package main

func (g *GameState) handleOverrideImage(h *Hub, c *Client, m inMsg) {
	if c.role != "operator" {
		return
	}
	pid := m.str("playerId")
	g.mu.Lock()
	if p := g.players[pid]; p != nil {
		p.Image = m.str("imageUrl")
	}
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleOverrideImageUpload(h *Hub, c *Client, m inMsg) {
	if c.role != "operator" {
		return
	}
	pid := m.str("playerId")
	g.mu.Lock()
	if p := g.players[pid]; p != nil {
		p.Image = "/uploads/" + m.str("filename")
	}
	g.mu.Unlock()
	g.broadcastPublicState(h)
}
