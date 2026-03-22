package main

// ── State snapshot types ──────────────────────────────────────────────────────

// gmPlayer is the player shape sent exclusively to GM connections.
// It extends the public player fields with the secret role text.
type gmPlayer struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	Points        int    `json:"points"`
	Status        string `json:"status"`
	BuzzerEnabled bool   `json:"buzzerEnabled"`
	Connected     bool   `json:"connected"`
	Role          string `json:"role"`
}

func (g *GameState) buildPublicPlayers() map[string]*player {
	m := make(map[string]*player, len(g.players))
	for id, p := range g.players {
		m[id] = p
	}
	return m
}

func (g *GameState) buildGMPlayers() map[string]gmPlayer {
	m := make(map[string]gmPlayer, len(g.players))
	for id, p := range g.players {
		m[id] = gmPlayer{
			ID:            p.ID,
			Name:          p.Name,
			Image:         p.Image,
			Points:        p.Points,
			Status:        p.Status,
			BuzzerEnabled: p.BuzzerEnabled,
			Connected:     p.Connected,
			Role:          g.roles[p.ID],
		}
	}
	return m
}

// publicState returns the game state for overlay and operator views.
// Must be called with g.mu held.
func (g *GameState) publicState() map[string]interface{} {
	var winner interface{}
	if g.buzzerWinner != "" {
		winner = g.buzzerWinner
	}
	var rv interface{}
	if g.revealedVotes != nil {
		rv = g.revealedVotes
	}
	// Build the per-player overlay reveal map: playerID → roleText (only for
	// players whose role has been individually revealed to the overlay).
	overlayRevealed := make(map[string]string, len(g.revealedRoles))
	for pid, revealed := range g.revealedRoles {
		if revealed {
			overlayRevealed[pid] = g.roles[pid]
		}
	}
	return map[string]interface{}{
		"type":            "gameState",
		"players":         g.buildPublicPlayers(),
		"buzzerLocked":    g.buzzerLocked,
		"buzzerWinner":    winner,
		"votingOpen":      g.votingOpen,
		"votesRevealed":   g.votesRevealed,
		"revealedVotes":   rv,
		"showAllRoles":    g.showAllRoles,
		"activeModules":   g.activeModules,
		"template":        g.template,
		"overlayRevealed": overlayRevealed,
	}
}

// gmState returns the full game state with roles and votes (GM-only).
// Must be called with g.mu held.
func (g *GameState) gmState() map[string]interface{} {
	base := g.publicState()
	base["players"] = g.buildGMPlayers()
	votes := make(map[string]string, len(g.votes))
	for k, v := range g.votes {
		votes[k] = v
	}
	base["votes"] = votes
	// Explicit roles map (playerID → roleText) for GM checklist rendering.
	rolesMap := make(map[string]string, len(g.roles))
	for k, v := range g.roles {
		rolesMap[k] = v
	}
	base["roles"] = rolesMap
	// Role definitions and per-player reveal state (GM only).
	defs := make([]RoleDefinition, len(g.roleDefinitions))
	copy(defs, g.roleDefinitions)
	base["roleDefinitions"] = defs
	revealedRoles := make(map[string]bool, len(g.revealedRoles))
	for k, v := range g.revealedRoles {
		revealedRoles[k] = v
	}
	base["revealedRoles"] = revealedRoles
	return base
}

// privateState returns a player's own private state (role, buzzer, voting info).
// Must be called with g.mu held.
func (g *GameState) privateState(p *player) map[string]interface{} {
	var winner interface{}
	if g.buzzerWinner != "" {
		winner = g.buzzerWinner
	}
	var rv interface{}
	if g.revealedVotes != nil {
		rv = g.revealedVotes
	}

	// Build the list of other players for voting (exclude self).
	votingPlayers := make([]map[string]interface{}, 0)
	if g.votingOpen {
		for _, other := range g.players {
			if other.ID == p.ID {
				continue
			}
			votingPlayers = append(votingPlayers, map[string]interface{}{
				"id":    other.ID,
				"name":  other.Name,
				"image": other.Image,
			})
		}
	}

	return map[string]interface{}{
		"type":          "playerState",
		"id":            p.ID,
		"name":          p.Name,
		"image":         p.Image,
		"points":        p.Points,
		"status":        p.Status,
		"buzzerEnabled": p.BuzzerEnabled,
		"role":          g.roles[p.ID],
		"votingOpen":    g.votingOpen,
		"votingPlayers": votingPlayers,
		"buzzerLocked":  g.buzzerLocked,
		"buzzerWinner":  winner,
		"activeModules": g.activeModules,
		"showAllRoles":  g.showAllRoles,
		"revealedVotes": rv,
	}
}
