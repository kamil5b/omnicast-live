package main

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// ── Types ─────────────────────────────────────────────────────────────────────

// Modules holds which game modules are active.
type Modules struct {
	Buzzer bool `json:"buzzer"`
	Points bool `json:"points"`
	Roles  bool `json:"roles"`
	Voting bool `json:"voting"`
	Status bool `json:"status"`
}

// player stores both public and private (socket) state.
type player struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	Points        int    `json:"points"`
	Status        string `json:"status"` // ALIVE | DEAD | MUTE
	BuzzerEnabled bool   `json:"buzzerEnabled"`
	Connected     bool   `json:"connected"`
	socketID      string // internal: the hub Client.playerID reference
}

// VoteResult is one row in the revealed-votes list.
type VoteResult struct {
	PlayerID   string `json:"playerId"`
	PlayerName string `json:"playerName"`
	Votes      int    `json:"votes"`
}

// GameState is the single, mutex-protected game state.
type GameState struct {
	mu sync.Mutex

	players       map[string]*player
	roles         map[string]string // playerID → roleText (never sent to other players)
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

// ── Message types ─────────────────────────────────────────────────────────────

// msg is the generic inbound message parsed from the WebSocket.
type msg map[string]json.RawMessage

func (m msg) str(key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	json.Unmarshal(v, &s)
	return s
}

func (m msg) bool_(key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	var b bool
	json.Unmarshal(v, &b)
	return b
}

func (m msg) int_(key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	var i int
	json.Unmarshal(v, &i)
	return i
}

// outMsg builds an outbound message with a "type" field plus the serialised
// fields of v merged at the top level.
func outMsg(msgType string, v interface{}) map[string]interface{} {
	var m map[string]interface{}
	b, _ := json.Marshal(v)
	json.Unmarshal(b, &m)
	if m == nil {
		m = make(map[string]interface{})
	}
	m["type"] = msgType
	return m
}

// ── Public / GM state builders ────────────────────────────────────────────────

type publicPlayerState struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	Points        int    `json:"points"`
	Status        string `json:"status"`
	BuzzerEnabled bool   `json:"buzzerEnabled"`
	Connected     bool   `json:"connected"`
}

type gmPlayerState struct {
	publicPlayerState
	Role string `json:"role"`
}

func (g *GameState) buildPublicPlayers() map[string]publicPlayerState {
	m := make(map[string]publicPlayerState, len(g.players))
	for id, p := range g.players {
		m[id] = publicPlayerState{
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

func (g *GameState) buildGMPlayers() map[string]gmPlayerState {
	m := make(map[string]gmPlayerState, len(g.players))
	for id, p := range g.players {
		m[id] = gmPlayerState{
			publicPlayerState: publicPlayerState{
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

func (g *GameState) publicStateMap() map[string]interface{} {
	winner := interface{}(nil)
	if g.buzzerWinner != "" {
		winner = g.buzzerWinner
	}
	var rv interface{} = nil
	if g.revealedVotes != nil {
		rv = g.revealedVotes
	}
	return map[string]interface{}{
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

func (g *GameState) gmStateMap() map[string]interface{} {
	base := g.publicStateMap()
	base["players"] = g.buildGMPlayers()
	// Deep copy votes
	votes := make(map[string]string, len(g.votes))
	for k, v := range g.votes {
		votes[k] = v
	}
	base["votes"] = votes
	return base
}

// ── Broadcast ─────────────────────────────────────────────────────────────────

// broadcastPublicState fans out the appropriate view to each audience.
// Must be called while NOT holding g.mu.
func (g *GameState) broadcastPublicState(h *Hub) {
	g.mu.Lock()
	pub := g.publicStateMap()
	gm := g.gmStateMap()
	// Build per-player private state (while holding lock)
	type playerPrivate struct {
		client *Client
		state  map[string]interface{}
	}
	var privates []playerPrivate
	for _, p := range g.players {
		c := h.findPlayer(p.ID)
		if c == nil {
			continue
		}
		winner := interface{}(nil)
		if g.buzzerWinner != "" {
			winner = g.buzzerWinner
		}
		var rv interface{} = nil
		if g.revealedVotes != nil {
			rv = g.revealedVotes
		}
		privates = append(privates, playerPrivate{
			client: c,
			state: map[string]interface{}{
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
			},
		})
	}
	g.mu.Unlock()

	pub["type"] = "gameState"
	gm["type"] = "gameState"

	h.broadcastRole("overlay", pub)
	h.broadcastRole("operator", pub)
	h.forEachRole("gm", func(c *Client) {
		h.send(c, gm)
	})
	for _, pp := range privates {
		h.send(pp.client, pp.state)
	}
}

// ── Input dispatch ────────────────────────────────────────────────────────────

// dispatch routes an inbound WebSocket message to the correct handler.
func (g *GameState) dispatch(h *Hub, c *Client, raw []byte) {
	var m msg
	if err := json.Unmarshal(raw, &m); err != nil {
		return
	}
	t := m.str("type")

	switch t {

	// ── Join events ──────────────────────────────────────────────────────────
	case "player:join":
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
		h.send(c, map[string]interface{}{"type": "joined", "id": id, "role": "player"})
		g.broadcastPublicState(h)

	case "gm:join":
		c.role = "gm"
		g.mu.Lock()
		state := g.gmStateMap()
		g.mu.Unlock()
		state["type"] = "gameState"
		h.send(c, map[string]interface{}{"type": "joined", "role": "gm"})
		h.send(c, state)

	case "operator:join":
		c.role = "operator"
		g.mu.Lock()
		state := g.publicStateMap()
		g.mu.Unlock()
		state["type"] = "gameState"
		h.send(c, map[string]interface{}{"type": "joined", "role": "operator"})
		h.send(c, state)

	case "overlay:join":
		c.role = "overlay"
		g.mu.Lock()
		state := g.publicStateMap()
		g.mu.Unlock()
		state["type"] = "gameState"
		h.send(c, state)

	// ── Player actions ───────────────────────────────────────────────────────
	case "player:buzz":
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

	case "player:vote":
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
		gmState := g.gmStateMap()
		g.mu.Unlock()

		gmState["type"] = "gameState"
		h.forEachRole("gm", func(gc *Client) {
			h.send(gc, gmState)
		})
		h.send(c, map[string]interface{}{"type": "vote:confirmed", "targetId": targetID})

	// ── GM actions ───────────────────────────────────────────────────────────
	case "gm:setPoints":
		if c.role != "gm" {
			return
		}
		pid := m.str("playerId")
		delta := m.int_("delta")
		g.mu.Lock()
		p := g.players[pid]
		if p == nil || !g.activeModules.Points {
			g.mu.Unlock()
			return
		}
		p.Points += delta
		g.mu.Unlock()
		g.broadcastPublicState(h)

	case "gm:setStatus":
		if c.role != "gm" {
			return
		}
		pid := m.str("playerId")
		status := m.str("status")
		if status != "ALIVE" && status != "DEAD" && status != "MUTE" {
			return
		}
		g.mu.Lock()
		p := g.players[pid]
		if p == nil || !g.activeModules.Status {
			g.mu.Unlock()
			return
		}
		p.Status = status
		g.mu.Unlock()
		g.broadcastPublicState(h)

	case "gm:assignRole":
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
		// Build private state for this player (send role ONLY to them)
		winner := interface{}(nil)
		if g.buzzerWinner != "" {
			winner = g.buzzerWinner
		}
		var rv interface{} = nil
		if g.revealedVotes != nil {
			rv = g.revealedVotes
		}
		playerState := map[string]interface{}{
			"type":          "playerState",
			"id":            p.ID,
			"name":          p.Name,
			"image":         p.Image,
			"points":        p.Points,
			"status":        p.Status,
			"buzzerEnabled": p.BuzzerEnabled,
			"role":          role,
			"votingOpen":    g.votingOpen,
			"buzzerLocked":  g.buzzerLocked,
			"buzzerWinner":  winner,
			"activeModules": g.activeModules,
			"showAllRoles":  g.showAllRoles,
			"revealedVotes": rv,
		}
		gmState := g.gmStateMap()
		g.mu.Unlock()

		gmState["type"] = "gameState"
		pc := h.findPlayer(pid)
		if pc != nil {
			h.send(pc, playerState)
		}
		h.forEachRole("gm", func(gc *Client) {
			h.send(gc, gmState)
		})

	case "gm:enableBuzzers":
		if c.role != "gm" {
			return
		}
		g.mu.Lock()
		g.buzzerLocked = false
		g.buzzerWinner = ""
		g.mu.Unlock()
		g.broadcastPublicState(h)
		h.broadcastAll(map[string]interface{}{"type": "buzzer:enabled"})

	case "gm:resetBuzzer":
		if c.role != "gm" {
			return
		}
		g.mu.Lock()
		g.buzzerLocked = false
		g.buzzerWinner = ""
		g.mu.Unlock()
		g.broadcastPublicState(h)
		h.broadcastAll(map[string]interface{}{"type": "buzzer:reset"})

	case "gm:disableBuzzerForPlayer":
		if c.role != "gm" {
			return
		}
		pid := m.str("playerId")
		enabled := m.bool_("enabled")
		g.mu.Lock()
		p := g.players[pid]
		if p != nil {
			p.BuzzerEnabled = enabled
		}
		g.mu.Unlock()
		g.broadcastPublicState(h)

	case "gm:openVoting":
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

	case "gm:closeVoting":
		if c.role != "gm" {
			return
		}
		g.mu.Lock()
		g.votingOpen = false
		g.mu.Unlock()
		g.broadcastPublicState(h)

	case "gm:revealVotes":
		if c.role != "gm" {
			return
		}
		g.mu.Lock()
		g.votesRevealed = true
		tally := make(map[string]int)
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

	case "gm:showAllRoles":
		if c.role != "gm" {
			return
		}
		show := m.bool_("show")
		g.mu.Lock()
		if !g.activeModules.Roles {
			g.mu.Unlock()
			return
		}
		g.showAllRoles = show
		rolesMsg := map[string]interface{}{"type": "rolesRevealed"}
		if show {
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

	case "gm:removePlayer":
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

	case "gm:resetScores":
		if c.role != "gm" {
			return
		}
		g.mu.Lock()
		for _, p := range g.players {
			p.Points = 0
		}
		g.mu.Unlock()
		g.broadcastPublicState(h)

	case "gm:loadTemplate":
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

	case "gm:setModules":
		if c.role != "gm" {
			return
		}
		var mods Modules
		if v, ok := m["modules"]; ok {
			json.Unmarshal(v, &mods)
		}
		g.mu.Lock()
		// Merge: only overwrite fields that appear in the message.
		mergeModulesFromMap(&g.activeModules, m["modules"])
		g.mu.Unlock()
		g.broadcastPublicState(h)

	// ── Operator actions ─────────────────────────────────────────────────────
	case "operator:overrideImage":
		if c.role != "operator" {
			return
		}
		pid := m.str("playerId")
		imageURL := m.str("imageUrl")
		g.mu.Lock()
		p := g.players[pid]
		if p != nil {
			p.Image = imageURL
		}
		g.mu.Unlock()
		g.broadcastPublicState(h)

	case "operator:overrideImageUpload":
		if c.role != "operator" {
			return
		}
		pid := m.str("playerId")
		filename := m.str("filename")
		g.mu.Lock()
		p := g.players[pid]
		if p != nil {
			p.Image = "/uploads/" + filename
		}
		g.mu.Unlock()
		g.broadcastPublicState(h)

	case "operator:showAllRoles":
		if c.role != "operator" {
			return
		}
		show := m.bool_("show")
		g.mu.Lock()
		if !g.activeModules.Roles {
			g.mu.Unlock()
			return
		}
		g.showAllRoles = show
		rolesMsg := map[string]interface{}{"type": "rolesRevealed"}
		if show {
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

	case "operator:resetScores":
		if c.role != "operator" {
			return
		}
		g.mu.Lock()
		for _, p := range g.players {
			p.Points = 0
		}
		g.mu.Unlock()
		g.broadcastPublicState(h)
	}
}

// onDisconnect is called when a player's connection drops.
func (g *GameState) onDisconnect(h *Hub, c *Client) {
	if c.role != "player" || c.playerID == "" {
		return
	}
	g.mu.Lock()
	p := g.players[c.playerID]
	if p != nil {
		p.Connected = false
	}
	g.mu.Unlock()
	g.broadcastPublicState(h)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// sanitize replaces HTML special characters to prevent XSS.
func sanitize(s string) string {
	replacer := strings.NewReplacer(
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
		"&", "&amp;",
	)
	return replacer.Replace(s)
}

// templateFile is the on-disk template format.
type templateFile struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Modules     *modulesPatch   `json:"modules"`
}

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
