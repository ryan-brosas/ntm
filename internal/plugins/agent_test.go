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
