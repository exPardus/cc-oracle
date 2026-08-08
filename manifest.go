// Package oracle exposes this plugin's identity, derived from the manifest
// JSONs so the data-dir allowlist can never drift from what the harness
// actually names our data dir.
//
// The Python original read the manifests from disk at import time, relative to
// __file__. A compiled binary has no __file__ and can be moved, so the
// manifests are embedded at build time instead: same source of truth, zero
// runtime I/O, and a rename that is not rebuilt cannot silently strand state.
// manifest_test.go asserts the embedded names still match the JSON on disk.
package oracle

import (
	"embed"
	"encoding/json"
	"strings"
)

//go:embed all:.claude-plugin
var manifestFS embed.FS

// Fallbacks match the Python original's, used only if a manifest is
// unreadable or malformed.
const (
	fallbackPlugin      = "oracle"
	fallbackMarketplace = "cc-oracle"
)

// PluginName and MarketplaceName come from .claude-plugin/{plugin,marketplace}.json.
var (
	PluginName      = manifestName(".claude-plugin/plugin.json", fallbackPlugin)
	MarketplaceName = manifestName(".claude-plugin/marketplace.json", fallbackMarketplace)
)

// OwnDataDirNames is the EXACT set of basenames a CLAUDE_PLUGIN_DATA directory
// may have for us to accept it as ours. Never a prefix test: an open
// strings.HasPrefix(name, "oracle-") would accept an unrelated "oracle-db-tools".
var OwnDataDirNames = map[string]struct{}{
	PluginName:                         {},
	PluginName + "-" + MarketplaceName: {},
	PluginName + "@" + MarketplaceName: {},
}

func manifestName(path, fallback string) string {
	raw, err := manifestFS.ReadFile(path)
	if err != nil {
		return fallback
	}
	var doc struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fallback
	}
	if name := strings.TrimSpace(doc.Name); name != "" {
		return name
	}
	return fallback
}
