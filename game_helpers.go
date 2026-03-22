package main

import (
	"encoding/json"
	"strings"
)

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

// ── Template types ────────────────────────────────────────────────────────────

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

// mergeModules applies a modulesPatch to a Modules struct, only touching
// fields that are non-nil in the patch.
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
