package main

import (
	"encoding/json"
	"math/rand"
	"sort"
)

// playerSnapshot pairs a player ID with its pre-built private state payload.
// Used to batch private-state deliveries after releasing the game lock.
type playerSnapshot struct {
	playerID string
	state    map[string]interface{}
}

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
	gmSnap := g.gmState()
	g.mu.Unlock()

	if pc := h.findPlayer(pid); pc != nil {
		h.deliver(pc, playerSnap)
	}
	h.forEachRole("gm", func(gc *Client) { h.deliver(gc, gmSnap) })
}

// handleRevealRoleForPlayer reveals (or hides) a single player's role to that player only.
func (g *GameState) handleRevealRoleForPlayer(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	pid := m.str("playerId")
	reveal := m.boolVal("reveal")
	g.mu.Lock()
	p := g.players[pid]
	if p == nil || !g.activeModules.Roles {
		g.mu.Unlock()
		return
	}
	g.revealedRoles[pid] = reveal
	playerSnap := g.privateState(p)
	gmSnap := g.gmState()
	g.mu.Unlock()

	if pc := h.findPlayer(pid); pc != nil {
		h.deliver(pc, playerSnap)
	}
	h.forEachRole("gm", func(gc *Client) { h.deliver(gc, gmSnap) })
}

// handleSetRoleDefinitions replaces the GM's role definition list.
func (g *GameState) handleSetRoleDefinitions(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	var defs []RoleDefinition
	if raw, ok := m["roles"]; ok {
		json.Unmarshal(raw, &defs)
	}
	// sanitize names
	for i := range defs {
		defs[i].Name = sanitize(defs[i].Name)
		if defs[i].Max < 0 {
			defs[i].Max = 0
		}
	}
	g.mu.Lock()
	g.roleDefinitions = defs
	gmSnap := g.gmState()
	g.mu.Unlock()
	h.forEachRole("gm", func(gc *Client) { h.deliver(gc, gmSnap) })
}

// handleAssignRoleChecklist assigns or unassigns a role to a player via checklist.
// It enforces the 1-player-1-role constraint and respects max count per role.
func (g *GameState) handleAssignRoleChecklist(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	pid := m.str("playerId")
	role := sanitize(m.str("role"))
	assign := m.boolVal("assign") // true = assign, false = unassign

	g.mu.Lock()
	p := g.players[pid]
	if p == nil || !g.activeModules.Roles {
		g.mu.Unlock()
		return
	}

	if assign {
		// Enforce max count per role definition
		maxCount := 0
		for _, def := range g.roleDefinitions {
			if def.Name == role {
				maxCount = def.Max
				break
			}
		}
		if maxCount > 0 {
			count := 0
			for _, r := range g.roles {
				if r == role {
					count++
				}
			}
			if count >= maxCount {
				g.mu.Unlock()
				return
			}
		}
		g.roles[pid] = role
	} else {
		if g.roles[pid] == role {
			delete(g.roles, pid)
		}
	}

	// Collect snapshots for all players since role counts changed, plus the GM.
	privates := make([]playerSnapshot, 0, len(g.players))
	for _, pl := range g.players {
		privates = append(privates, playerSnapshot{playerID: pl.ID, state: g.privateState(pl)})
	}
	gmSnap := g.gmState()
	g.mu.Unlock()

	for _, pp := range privates {
		if pc := h.findPlayer(pp.playerID); pc != nil {
			h.deliver(pc, pp.state)
		}
	}
	h.forEachRole("gm", func(gc *Client) { h.deliver(gc, gmSnap) })
}

// handleRandomizeRoles randomly assigns roles to players according to role definitions.
// It respects the max count per role and the 1-player-1-role constraint.
// Only unassigned players are considered unless resetFirst is true.
func (g *GameState) handleRandomizeRoles(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	resetFirst := m.boolVal("resetFirst")

	g.mu.Lock()
	if !g.activeModules.Roles || len(g.roleDefinitions) == 0 {
		g.mu.Unlock()
		return
	}

	if resetFirst {
		g.roles = make(map[string]string)
	}

	// Collect players without a role assigned
	unassigned := make([]string, 0)
	for id := range g.players {
		if g.roles[id] == "" {
			unassigned = append(unassigned, id)
		}
	}
	rand.Shuffle(len(unassigned), func(i, j int) { unassigned[i], unassigned[j] = unassigned[j], unassigned[i] })

	// Build a pool of role slots respecting max counts
	pool := make([]string, 0)
	for _, def := range g.roleDefinitions {
		limit := def.Max
		if limit <= 0 {
			limit = len(unassigned) // effectively unlimited — fill remaining
		}
		// Count existing assignments for this role
		existing := 0
		for _, r := range g.roles {
			if r == def.Name {
				existing++
			}
		}
		slots := limit - existing
		for i := 0; i < slots && len(pool) < len(unassigned); i++ {
			pool = append(pool, def.Name)
		}
	}
	// Pad with empty string if pool is shorter than unassigned
	for len(pool) < len(unassigned) {
		pool = append(pool, "")
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })

	for i, pid := range unassigned {
		if i < len(pool) && pool[i] != "" {
			g.roles[pid] = pool[i]
		}
	}

	privates := make([]playerSnapshot, 0, len(g.players))
	for _, pl := range g.players {
		privates = append(privates, playerSnapshot{playerID: pl.ID, state: g.privateState(pl)})
	}
	gmSnap := g.gmState()
	g.mu.Unlock()

	for _, pp := range privates {
		if pc := h.findPlayer(pp.playerID); pc != nil {
			h.deliver(pc, pp.state)
		}
	}
	h.forEachRole("gm", func(gc *Client) { h.deliver(gc, gmSnap) })
}

// handleResetRoles clears all role assignments (but keeps role definitions).
func (g *GameState) handleResetRoles(h *Hub, c *Client) {
	if c.role != "gm" {
		return
	}
	g.mu.Lock()
	g.roles = make(map[string]string)
	g.revealedRoles = make(map[string]bool)

	privates := make([]playerSnapshot, 0, len(g.players))
	for _, pl := range g.players {
		privates = append(privates, playerSnapshot{playerID: pl.ID, state: g.privateState(pl)})
	}
	gmSnap := g.gmState()
	g.mu.Unlock()

	for _, pp := range privates {
		if pc := h.findPlayer(pp.playerID); pc != nil {
			h.deliver(pc, pp.state)
		}
	}
	h.forEachRole("gm", func(gc *Client) { h.deliver(gc, gmSnap) })
}

// handleGMMessage sends a direct message from the GM to a specific player.
func (g *GameState) handleGMMessage(h *Hub, c *Client, m inMsg) {
	if c.role != "gm" {
		return
	}
	pid := m.str("playerId")
	text := sanitize(m.str("text"))
	if text == "" || pid == "" {
		return
	}

	g.mu.Lock()
	p := g.players[pid]
	g.mu.Unlock()

	if p == nil {
		return
	}

	pc := h.findPlayer(pid)
	if pc == nil {
		return
	}
	h.deliver(pc, map[string]interface{}{
		"type": "gm:message",
		"text": text,
	})
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

func (g *GameState) handleHideVotes(h *Hub, c *Client) {
	if c.role != "gm" {
		return
	}
	g.mu.Lock()
	g.votesRevealed = false
	g.revealedVotes = nil
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
	delete(g.revealedRoles, pid)
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
