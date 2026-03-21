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

	type pending struct {
		playerID string
		state    map[string]interface{}
	}
	privates := make([]pending, 0, len(g.players))
	for _, p := range g.players {
		privates = append(privates, pending{playerID: p.ID, state: g.privateState(p)})
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

// setShowAllRoles toggles the showAllRoles flag and, when enabled, pushes
// every player's role to the overlay. Used by both GM and operator.
func (g *GameState) setShowAllRoles(h *Hub, show bool) {
	g.mu.Lock()
	if !g.activeModules.Roles {
		g.mu.Unlock()
		return
	}
	g.showAllRoles = show
	var rolesMsg map[string]interface{}
	if show {
		rolesMsg = map[string]interface{}{"type": "rolesRevealed"}
		for _, p := range g.players {
			role := g.roles[p.ID]
			if role == "" {
				role = "—"
			}
			rolesMsg[p.ID] = role
		}
	}
	g.mu.Unlock()

	if show {
		h.broadcastRole("overlay", rolesMsg)
	}
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
