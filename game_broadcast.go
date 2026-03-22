package main

// broadcastPublicState fans the current game state to every audience:
//   - overlay/operator → public state
//   - each GM socket   → full state (roles + votes)
//   - each player      → their private state (includes own role)
//
// Snapshots all data under the game lock, then releases before sending so
// that g.mu is never held when h.mu is acquired.
func (g *GameState) broadcastPublicState(h *Hub) {
	g.mu.Lock()
	pub := g.publicState()
	gm := g.gmState()

	privates := make([]playerSnapshot, 0, len(g.players))
	for _, p := range g.players {
		privates = append(privates, playerSnapshot{playerID: p.ID, state: g.privateState(p)})
	}
	g.mu.Unlock()

	h.broadcastRole("overlay", pub)
	h.broadcastRole("operator", pub)
	h.forEachRole("gm", func(c *Client) { h.deliver(c, gm) })
	for _, pp := range privates {
		if c := h.findPlayer(pp.playerID); c != nil {
			h.deliver(c, pp.state)
		}
	}
}

// setShowAllRoles toggles the showAllRoles flag. When enabled, every player's
// role is written into revealedRoles so that overlayRevealed in publicState is
// consistent for the overlay on the next broadcast (and on reconnect).
// When disabled, only the per-player overrides that were set explicitly survive;
// the bulk entries added by this call are removed.
func (g *GameState) setShowAllRoles(h *Hub, show bool) {
	g.mu.Lock()
	if !g.activeModules.Roles {
		g.mu.Unlock()
		return
	}
	g.showAllRoles = show
	if show {
		for _, p := range g.players {
			g.revealedRoles[p.ID] = true
		}
	} else {
		// Clear the map entirely — per-player manual reveals are also cleared,
		// matching the expectation that "hide all" means nothing shows.
		g.revealedRoles = make(map[string]bool)
	}
	g.mu.Unlock()

	g.broadcastPublicState(h)
}

// resetAllScores zeroes every player's point total. Used by both GM and operator.
func (g *GameState) resetAllScores(h *Hub) {
	g.mu.Lock()
	for _, p := range g.players {
		p.Points = 0
	}
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleGMShowAllRoles(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	g.setShowAllRoles(h, m.boolVal("show"))
}

func (g *GameState) handleGMResetScores(h *Hub, c *Client) {
	if c.role != "gm" {
		return
	}
	g.resetAllScores(h)
}

func (g *GameState) handleOperatorShowAllRoles(h *Hub, c *Client, m inMsg) {
	if c.role != "operator" {
		return
	}
	g.setShowAllRoles(h, m.boolVal("show"))
}

func (g *GameState) handleOperatorResetScores(h *Hub, c *Client) {
	if c.role != "operator" {
		return
	}
	g.resetAllScores(h)
}
