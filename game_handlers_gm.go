package main

import "sort"

func (g *GameState) handleSetPoints(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	pid := m.str("playerId")
	delta := m.intVal("delta")
	g.mu.Lock()
	p := g.players[pid]
	if p == nil || !g.activeModules.Points {
		g.mu.Unlock()
		return
	}
	p.Points += delta
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleSetStatus(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	status := m.str("status")
	if status != "ALIVE" && status != "DEAD" && status != "MUTE" {
		return
	}
	pid := m.str("playerId")
	g.mu.Lock()
	p := g.players[pid]
	if p == nil || !g.activeModules.Status {
		g.mu.Unlock()
		return
	}
	p.Status = status
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleAssignRole(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	pid := m.str("playerId")
	role := sanitize(m.str("role"))
	g.mu.Lock()
	p := g.players[pid]
	if p == nil || !g.activeModules.Roles {
		g.mu.Unlock()
		return
	}
	g.roles[pid] = role
	playerSnap := g.privateState(p)
	playerSnap["role"] = role // ensure the newly assigned role is present
	gmSnap := g.gmState()
	g.mu.Unlock()

	if pc := h.findPlayer(pid); pc != nil {
		h.deliver(pc, playerSnap)
	}
	h.forEachRole("gm", func(gc *Client) { h.deliver(gc, gmSnap) })
}

// handleResetBuzzer handles both "gm:enableBuzzers" and "gm:resetBuzzer".
// eventType is the notification pushed to every connected client afterwards.
func (g *GameState) handleResetBuzzer(h *Hub, c *Client, eventType string) {
	if c.role != "gm" {
		return
	}
	g.mu.Lock()
	g.buzzerLocked = false
	g.buzzerWinner = ""
	g.mu.Unlock()
	g.broadcastPublicState(h)
	h.broadcastAll(map[string]interface{}{"type": eventType})
}

func (g *GameState) handleDisableBuzzerForPlayer(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	pid := m.str("playerId")
	enabled := m.boolVal("enabled")
	g.mu.Lock()
	if p := g.players[pid]; p != nil {
		p.BuzzerEnabled = enabled
	}
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleOpenVoting(h *Hub, c *Client) {
	if c.role != "gm" {
		return
	}
	g.mu.Lock()
	if !g.activeModules.Voting {
		g.mu.Unlock()
		return
	}
	g.votingOpen = true
	g.votes = make(map[string]string)
	g.votesRevealed = false
	g.revealedVotes = nil
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleCloseVoting(h *Hub, c *Client) {
	if c.role != "gm" {
		return
	}
	g.mu.Lock()
	g.votingOpen = false
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleRevealVotes(h *Hub, c *Client) {
	if c.role != "gm" {
		return
	}
	g.mu.Lock()
	g.votesRevealed = true
	tally := make(map[string]int, len(g.votes))
	for _, tid := range g.votes {
		tally[tid]++
	}
	results := make([]VoteResult, 0, len(tally))
	for tid, cnt := range tally {
		name := tid
		if p := g.players[tid]; p != nil {
			name = p.Name
		}
		results = append(results, VoteResult{PlayerID: tid, PlayerName: name, Votes: cnt})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Votes > results[j].Votes })
	g.revealedVotes = results
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleRemovePlayer(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	pid := m.str("playerId")
	g.mu.Lock()
	delete(g.players, pid)
	delete(g.roles, pid)
	delete(g.votes, pid)
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleLoadTemplate(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	name := m.str("templateName")
	tmpl := loadTemplateFile(name)
	if tmpl == nil {
		return
	}
	g.mu.Lock()
	if tmpl.Modules != nil {
		mergeModules(&g.activeModules, tmpl.Modules)
	}
	g.template = name
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

func (g *GameState) handleSetModules(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	g.mu.Lock()
	mergeModulesFromMap(&g.activeModules, m["modules"])
	g.mu.Unlock()
	g.broadcastPublicState(h)
}
