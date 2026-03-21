package main

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Modules controls which game features are active.
type Modules struct {
	Buzzer bool `json:"buzzer"`
	Points bool `json:"points"`
	Roles  bool `json:"roles"`
	Voting bool `json:"voting"`
	Status bool `json:"status"`
}

// player holds per-player data.
// Status is one of "ALIVE", "DEAD", or "MUTE".
type player struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	Points        int    `json:"points"`
	Status        string `json:"status"`
	BuzzerEnabled bool   `json:"buzzerEnabled"`
	Connected     bool   `json:"connected"`
}

// VoteResult is one entry in the revealed-votes ranking.
type VoteResult struct {
	PlayerID   string `json:"playerId"`
	PlayerName string `json:"playerName"`
	Votes      int    `json:"votes"`
}

// GameState is the single authoritative game state.
// All mutations must hold g.mu. Callers of broadcastPublicState must NOT
// hold g.mu — the method acquires it internally.
// Lock ordering: g.mu must always be acquired before h.mu (Hub).
type GameState struct {
	mu sync.Mutex

	players       map[string]*player
	roles         map[string]string // playerID → roleText; never broadcast to other players
	buzzerLocked  bool
	buzzerWinner  string
	votingOpen    bool
	votes         map[string]string // voterID → targetID
	votesRevealed bool
	revealedVotes []VoteResult
	showAllRoles  bool
	activeModules Modules
	template      string
}

func newGameState() *GameState {
	return &GameState{
		players: make(map[string]*player),
		roles:   make(map[string]string),
		votes:   make(map[string]string),
		activeModules: Modules{
			Buzzer: true,
			Points: true,
			Roles:  true,
			Voting: true,
			Status: true,
		},
		template: "custom",
	}
}

// ── Inbound message helpers ───────────────────────────────────────────────────

// inMsg is the generic inbound WebSocket message (type + arbitrary fields).
type inMsg map[string]json.RawMessage

func (m inMsg) str(key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	json.Unmarshal(v, &s)
	return s
}

func (m inMsg) boolVal(key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	var b bool
	json.Unmarshal(v, &b)
	return b
}

func (m inMsg) intVal(key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	var i int
	json.Unmarshal(v, &i)
	return i
}

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

// publicState returns the game state suitable for overlay and operator views.
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

// gmState returns the game state with roles and votes (GM-only).
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

// privateState returns the per-player private state (role, voting, buzzer info).
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

// ── Broadcast ─────────────────────────────────────────────────────────────────

// broadcastPublicState fans the current game state out to every audience:
//   - overlay/operator → public state
//   - each GM socket   → full state (roles + votes)
//   - each player      → their private state (includes own role)
func (g *GameState) broadcastPublicState(h *Hub) {
	// Snapshot everything under the game lock, then release before sending.
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

	// Send without holding any lock.
	h.broadcastRole("overlay", pub)
	h.broadcastRole("operator", pub)
	h.forEachRole("gm", func(c *Client) { h.deliver(c, gm) })
	for _, pp := range privates {
		if c := h.findPlayer(pp.playerID); c != nil {
			h.deliver(c, pp.state)
		}
	}
}

// ── Shared handlers ───────────────────────────────────────────────────────────

// setShowAllRoles sets the showAllRoles flag and, when true, pushes role data
// to the overlay. Used by both GM and operator.
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

// ── Input dispatch ────────────────────────────────────────────────────────────

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

// ── Join handlers ─────────────────────────────────────────────────────────────

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

// ── Player action handlers ────────────────────────────────────────────────────

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

// ── GM action handlers ────────────────────────────────────────────────────────

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

// handleResetBuzzer handles both "gm:enableBuzzers" and "gm:resetBuzzer";
// eventType is the event pushed to every client after the reset.
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
	tmpl := loadTemplateFile(m.str("templateName"))
	if tmpl == nil {
		return
	}
	g.mu.Lock()
	if tmpl.Modules != nil {
		mergeModules(&g.activeModules, tmpl.Modules)
	}
	g.template = m.str("templateName")
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

// ── Operator action handlers ──────────────────────────────────────────────────

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

// ── Disconnect ────────────────────────────────────────────────────────────────

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

// ── Helpers ───────────────────────────────────────────────────────────────────

// sanitize escapes HTML special characters to prevent XSS.
func sanitize(s string) string {
	return strings.NewReplacer(
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
		"&", "&amp;",
	).Replace(s)
}

// templateFile represents the on-disk template format.
type templateFile struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Modules     *modulesPatch `json:"modules"`
}

// modulesPatch uses pointer fields so that absent JSON keys leave the
// corresponding Modules fields unchanged.
type modulesPatch struct {
	Buzzer *bool `json:"buzzer"`
	Points *bool `json:"points"`
	Roles  *bool `json:"roles"`
	Voting *bool `json:"voting"`
	Status *bool `json:"status"`
}

func mergeModules(dst *Modules, src *modulesPatch) {
	if src.Buzzer != nil {
		dst.Buzzer = *src.Buzzer
	}
	if src.Points != nil {
		dst.Points = *src.Points
	}
	if src.Roles != nil {
		dst.Roles = *src.Roles
	}
	if src.Voting != nil {
		dst.Voting = *src.Voting
	}
	if src.Status != nil {
		dst.Status = *src.Status
	}
}

// mergeModulesFromMap updates only the fields present in the raw JSON object.
func mergeModulesFromMap(dst *Modules, raw json.RawMessage) {
	if raw == nil {
		return
	}
	var m map[string]bool
	if err := json.Unmarshal(raw, &m); err != nil {
		return
	}
	if v, ok := m["buzzer"]; ok {
		dst.Buzzer = v
	}
	if v, ok := m["points"]; ok {
		dst.Points = v
	}
	if v, ok := m["roles"]; ok {
		dst.Roles = v
	}
	if v, ok := m["voting"]; ok {
		dst.Voting = v
	}
	if v, ok := m["status"]; ok {
		dst.Status = v
	}
}
