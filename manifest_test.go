package oracle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Ports tests/test_stop_entry.py::test_own_dir_names_agree_with_manifests_on_disk.
//
// The Python original read the manifests at import time, so the allowlist could
// not drift from the files. The Go binary embeds them at build time instead,
// which is stricter at run time but means a rename that is never rebuilt would
// go unnoticed. This test is what closes that: it re-reads the JSON from disk
// and fails if the embedded copy disagrees.
func TestEmbeddedNamesMatchManifestsOnDisk(t *testing.T) {
	read := func(name string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(".claude-plugin", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var doc struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		return doc.Name
	}

	if want := read("plugin.json"); PluginName != want {
		t.Errorf("PluginName = %q, manifest says %q", PluginName, want)
	}
	if want := read("marketplace.json"); MarketplaceName != want {
		t.Errorf("MarketplaceName = %q, manifest says %q", MarketplaceName, want)
	}
}

func TestOwnDataDirNamesAreTheThreeExactForms(t *testing.T) {
	want := []string{
		PluginName,
		PluginName + "-" + MarketplaceName,
		PluginName + "@" + MarketplaceName,
	}
	if len(OwnDataDirNames) != len(want) {
		t.Fatalf("got %d names, want %d: %v", len(OwnDataDirNames), len(want), OwnDataDirNames)
	}
	for _, name := range want {
		if _, ok := OwnDataDirNames[name]; !ok {
			t.Errorf("missing expected form %q", name)
		}
	}
}

func TestOwnDataDirNamesRejectsPrefixLookalikes(t *testing.T) {
	// An open strings.HasPrefix(name, "oracle-") would accept an unrelated
	// plugin whose name merely begins with ours. The allowlist is exact forms.
	for _, foreign := range []string{
		PluginName + "-db-tools",
		PluginName + "x",
		"codex-openai-codex",
		"",
	} {
		if _, ok := OwnDataDirNames[foreign]; ok {
			t.Errorf("%q must not be accepted as our data dir", foreign)
		}
	}
}

func TestFallbacksAreNotSilentlyInUse(t *testing.T) {
	// If the embed ever breaks, the constants would quietly fall back and the
	// allowlist would still "look" right. Fail loudly instead.
	if _, err := manifestFS.ReadFile(".claude-plugin/plugin.json"); err != nil {
		t.Fatalf("plugin manifest is not embedded: %v", err)
	}
	if _, err := manifestFS.ReadFile(".claude-plugin/marketplace.json"); err != nil {
		t.Fatalf("marketplace manifest is not embedded: %v", err)
	}
}
