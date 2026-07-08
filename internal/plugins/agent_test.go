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
		{"pi --approve", "pi", true}, // prefix match
		{"pia", "pi", true},          // alias
		{"/usr/bin/pi", "pi", true},  // path suffix
		{"zsh", "", false},           // unrelated
		{"pine", "", false},          // no false prefix match
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
