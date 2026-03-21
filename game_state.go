package main

// ── State snapshot types ──────────────────────────────────────────────────────

// publicPlayer is the player view sent to overlay, operator and other players.
type publicPlayer struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	Points        int    `json:"points"`
	Status        string `json:"status"`
	BuzzerEnabled bool   `json:"buzzerEnabled"`
	Connected     bool   `json:"connected"`
}

// gmPlayer extends publicPlayer with the secret role (GM-only).
type gmPlayer struct {
	publicPlayer
	Role string `json:"role"`
}

func (g *GameState) buildPublicPlayers() map[string]publicPlayer {
	m := make(map[string]publicPlayer, len(g.players))
	for id, p := range g.players {
		m[id] = publicPlayer{
			ID:            p.ID,
			Name:          p.Name,
			Image:         p.Image,
			Points:        p.Points,
			Status:        p.Status,
			BuzzerEnabled: p.BuzzerEnabled,
			Connected:     p.Connected,
		}
	}
	return m
}

func (g *GameState) buildGMPlayers() map[string]gmPlayer {
	m := make(map[string]gmPlayer, len(g.players))
	for id, p := range g.players {
		m[id] = gmPlayer{
			publicPlayer: publicPlayer{
				ID:            p.ID,
				Name:          p.Name,
				Image:         p.Image,
				Points:        p.Points,
				Status:        p.Status,
				BuzzerEnabled: p.BuzzerEnabled,
				Connected:     p.Connected,
			},
			Role: g.roles[p.ID],
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
	return map[string]interface{}{
		"type":          "gameState",
		"players":       g.buildPublicPlayers(),
		"buzzerLocked":  g.buzzerLocked,
		"buzzerWinner":  winner,
		"votingOpen":    g.votingOpen,
		"votesRevealed": g.votesRevealed,
		"revealedVotes": rv,
		"showAllRoles":  g.showAllRoles,
		"activeModules": g.activeModules,
		"template":      g.template,
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
		"buzzerLocked":  g.buzzerLocked,
		"buzzerWinner":  winner,
		"activeModules": g.activeModules,
		"showAllRoles":  g.showAllRoles,
		"revealedVotes": rv,
	}
}
