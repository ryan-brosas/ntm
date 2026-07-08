package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAgentCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Minimal pi-style plugin: name "pi", alias "pia".
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(`
[agent]
name = "pi"
alias = "pia"
command = "pi --approve"
`), 0o644); err != nil {
		t.Fatalf("write pi.toml: %v", err)
	}

	tests := []struct {
		in   string
		name string
		cmd  string
		ok   bool
	}{
		{"pi", "pi", "pi --approve", true},
		{"pia", "pi", "pi --approve", true}, // alias resolves to canonical name
		{"foo", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		name, cmd, ok := ResolveAgentCommand(tt.in, dir)
		if ok != tt.ok || name != tt.name || cmd != tt.cmd {
			t.Errorf("ResolveAgentCommand(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.in, name, cmd, ok, tt.name, tt.cmd, tt.ok)
		}
	}

	// Non-existent dir: not ok, no panic.
	if _, _, ok := ResolveAgentCommand("pi", filepath.Join(dir, "missing")); ok {
		t.Errorf("ResolveAgentCommand on missing dir should be not-ok")
	}
}

func TestBuildPluginCmdIndexFromAndMatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(`
[agent]
name = "pi"
alias = "pia"
command = "pi --approve"
`), 0o644); err != nil {
		t.Fatalf("write pi.toml: %v", err)
	}

	index := buildPluginCmdIndexFrom(dir)
	// name + alias + command-token all map to the canonical name.
	if index["pi"] != "pi" {
		t.Errorf("index[pi] = %q, want pi", index["pi"])
	}
	if index["pia"] != "pi" {
		t.Errorf("index[pia] = %q, want pi", index["pia"])
	}

	tests := []struct {
		cmd  string
		name string
		ok   bool
	}{
		{"pi", "pi", true},
		{"pi --approve", "pi", true},          // prefix match
		{"pia", "pi", true},                   // alias
		{"PI", "pi", true},                    // case-insensitive
		{"/usr/bin/pi", "pi", true},           // path suffix
		{"/usr/local/bin/pi-foo", "pi", true}, // "/pi-" contains (mirrors built-in isAgent)
		{"./pi", "pi", true},                  // relative path suffix
		{"zsh", "", false},                    // unrelated
		{"pine", "", false},                   // no false prefix match
		{"spi-foo", "", false},                // no false contains match
		{"", "", false},
	}
	for _, tt := range tests {
		name, ok := matchCommandIndex(index, tt.cmd)
		if ok != tt.ok || name != tt.name {
			t.Errorf("matchCommandIndex(%q) = (%q, %v), want (%q, %v)",
				tt.cmd, name, ok, tt.name, tt.ok)
		}
	}

	// Empty/missing dir yields an empty index that matches nothing.
	empty := buildPluginCmdIndexFrom(filepath.Join(dir, "missing"))
	if len(empty) != 0 {
		t.Errorf("missing dir index = %v, want empty", empty)
	}
	if _, ok := matchCommandIndex(empty, "pi"); ok {
		t.Errorf("empty index should not match")
	}
}

// TestPluginCommandPrefixSpecificity verifies that two plugins sharing a binary
// but differing by flags — `pi` (bare) and `pia` (pi --approve) — are told apart
// by the full command: a pane running "pi --approve ..." classifies as `pia`, a
// bare "pi" classifies as `pi`. The longer literal command prefix wins.
func TestPluginCommandPrefixSpecificity(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pi.toml"), []byte(`
[agent]
name = "pi"
command = "pi{{if .Model}} --model {{shellQuote .Model}}{{end}}"
`), 0o644); err != nil {
		t.Fatalf("write pi.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pia.toml"), []byte(`
[agent]
name = "pia"
command = "pi --approve{{if .Model}} --model {{shellQuote .Model}}{{end}}"
`), 0o644); err != nil {
		t.Fatalf("write pia.toml: %v", err)
	}

	index := buildPluginCmdIndexFrom(dir)
	// Distinct keys: bare "pi" -> pi, full "pi --approve" -> pia.
	if index["pi"] != "pi" {
		t.Errorf("index[pi] = %q, want pi (bare command must map to the pi plugin)", index["pi"])
	}
	if index["pi --approve"] != "pia" {
		t.Errorf("index[pi --approve] = %q, want pia", index["pi --approve"])
	}

	tests := []struct {
		cmd  string
		name string
		ok   bool
	}{
		// auto-approve (pia): joined argv carries --approve and/or --model.
		{"pi --approve", "pia", true},
		{"pi --approve --model claude-opus-4-8", "pia", true},
		{"/usr/local/bin/pi --approve", "pia", true},
		// bare (pi): interactive pi, no --approve.
		{"pi", "pi", true},
		{"pi --model claude-opus-4-8", "pi", true}, // --model without --approve is still bare pi
		{"/usr/local/bin/pi", "pi", true},
		// neither
		{"python", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		name, ok := matchCommandIndex(index, tt.cmd)
		if ok != tt.ok || name != tt.name {
			t.Errorf("matchCommandIndex(%q) = (%q, %v), want (%q, %v)",
				tt.cmd, name, ok, tt.name, tt.ok)
		}
	}
}
