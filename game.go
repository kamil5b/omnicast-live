package main

import (
	"encoding/json"
	"sync"
)

// ── Core types ────────────────────────────────────────────────────────────────

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

// RoleDefinition is one entry in the GM-defined role list.
type RoleDefinition struct {
	Name  string `json:"name"`
	Max   int    `json:"max"` // max players that can hold this role (0 = unlimited)
	Color string `json:"color,omitempty"`
}

// GameState is the single authoritative game state.
// All mutations must hold g.mu. Callers of broadcastPublicState must NOT
// hold g.mu — the method acquires it internally.
// Lock ordering: g.mu must always be acquired before h.mu (Hub).
type GameState struct {
	mu sync.Mutex

	players         map[string]*player
	roles           map[string]string // playerID → roleText; never broadcast to other players
	revealedRoles   map[string]bool   // playerID → true when GM has revealed that player's role to them explicitly
	roleDefinitions []RoleDefinition  // ordered list of roles defined by GM
	buzzerLocked    bool
	buzzerWinner    string
	votingOpen      bool
	votes           map[string]string // voterID → targetID
	votesRevealed   bool
	revealedVotes   []VoteResult
	showAllRoles    bool
	activeModules   Modules
	template        string
}

func newGameState() *GameState {
	return &GameState{
		players:         make(map[string]*player),
		roles:           make(map[string]string),
		revealedRoles:   make(map[string]bool),
		roleDefinitions: []RoleDefinition{},
		votes:           make(map[string]string),
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
