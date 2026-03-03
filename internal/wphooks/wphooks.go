// Package wphooks provides lookup for WordPress core hook definitions.
//
// It embeds the official wp-hooks/wordpress-core-hooks JSON data (actions +
// filters) and exposes fast name-based lookups. This allows tools to
// distinguish between WP core hooks and custom hooks defined by plugins.
package wphooks

import (
	_ "embed"
	"encoding/json"
	"sync"
)

//go:embed data/actions.json
var actionsJSON []byte

//go:embed data/filters.json
var filtersJSON []byte

// HookDef describes a single WordPress core hook.
type HookDef struct {
	Name string `json:"name"`
	File string `json:"file"`
	Type string `json:"type"` // "action" or "filter"
	Args int    `json:"args"`
}

// hookFile is the top-level JSON structure of the wp-hooks data files.
type hookFile struct {
	Hooks []HookDef `json:"hooks"`
}

var (
	initOnce sync.Once
	hookMap  map[string]*HookDef
)

func ensureLoaded() {
	initOnce.Do(func() {
		hookMap = make(map[string]*HookDef, 3000)
		for _, raw := range [][]byte{actionsJSON, filtersJSON} {
			var hf hookFile
			if err := json.Unmarshal(raw, &hf); err != nil {
				continue
			}
			for i := range hf.Hooks {
				h := &hf.Hooks[i]
				hookMap[h.Name] = h
			}
		}
	})
}

// IsKnownHook returns true if name is a WordPress core hook.
func IsKnownHook(name string) bool {
	ensureLoaded()
	_, ok := hookMap[name]
	return ok
}

// Lookup returns the hook definition for a WP core hook, or nil if unknown.
func Lookup(name string) *HookDef {
	ensureLoaded()
	return hookMap[name]
}

// Count returns the total number of loaded WP core hooks.
func Count() int {
	ensureLoaded()
	return len(hookMap)
}
