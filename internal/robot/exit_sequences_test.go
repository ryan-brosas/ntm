package robot

import (
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/tmux"
)

// =============================================================================
// exit_sequences.go — resolveAgentExitKind dispatch (bd-tgbow)
// =============================================================================
//
// resolveAgentExitKind is the pure, tmux-free decision behind exitAgent, so it
// can be unit-tested without a live tmux session. The cases below pin the
// dispatch for built-in agents and the pi/pia plugin TUI agents (which quit on
// a double-Ctrl+C gesture, matching Claude Code) and guard against regressions
// that would silently route pi/pia back to the single-Ctrl+C fallback (which
// only clears pi's input editor and never actually quits it).

func TestResolveAgentExitKind_BuiltIns(t *testing.T) {
	tests := []struct {
		name      string
		agentType string
		want      agentExitKind
	}{
		// Claude Code aliases all resolve to the double-Ctrl+C sequence.
		{"cc", "cc", exitKindDoubleCtrlC},
		{"claude", "claude", exitKindDoubleCtrlC},
		{"claude-code", "claude-code", exitKindDoubleCtrlC},
		{"ClaudeCode", "ClaudeCode", exitKindDoubleCtrlC},
		// Codex aliases resolve to the /exit command.
		{"cod", "cod", exitKindExitCommand},
		{"codex", "codex", exitKindExitCommand},
		{"openai-codex", "openai-codex", exitKindExitCommand},
		// Gemini aliases resolve to Escape then /exit.
		{"gmi", "gmi", exitKindEscapeExit},
		{"gemini", "gemini", exitKindEscapeExit},
		{"google-gemini", "google-gemini", exitKindEscapeExit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveAgentExitKind(tt.agentType); got != tt.want {
				t.Errorf("resolveAgentExitKind(%q) = %q, want %q", tt.agentType, got, tt.want)
			}
		})
	}
}

// TestResolveAgentExitKind_PiPiaDoubleCtrlC is the bd-tgbow regression: pi and
// pia (pi-coding-agent) must exit via the double-Ctrl+C sequence, not the
// single-Ctrl+C unknown fallback. A single Ctrl+C only clears pi's input
// editor; the TUI quits on two SIGINTs within 500ms (interactive-mode.handleCtrlC).
func TestResolveAgentExitKind_PiPiaDoubleCtrlC(t *testing.T) {
	tests := []struct {
		name      string
		agentType string
	}{
		{"pi", "pi"},
		{"pia", "pia"},
		{"PI_uppercase", "PI"},
		{"Pi_mixedcase", "Pi"},
		{"PIA_uppercase", "PIA"},
		{"pi_spaces", "  pi  "},
		{"pia_spaces", "\tpia\t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveAgentExitKind(tt.agentType)
			if got != exitKindDoubleCtrlC {
				t.Errorf("resolveAgentExitKind(%q) = %q, want %q (pi/pia must quit via double Ctrl+C, not the single-Ctrl+C fallback)",
					tt.agentType, got, exitKindDoubleCtrlC)
			}
		})
	}
}

// TestResolveAgentExitKind_UnknownFallback confirms that unrecognized agent
// types (and the user pane) still fall back to a single Ctrl+C, preserving the
// pre-bd-tgbow behavior for everything except the known plugin TUI agents.
func TestResolveAgentExitKind_UnknownFallback(t *testing.T) {
	tests := []struct {
		name      string
		agentType string
		want      agentExitKind
	}{
		{"empty", "", exitKindCtrlCFallback},
		{"unknown", "unknown", exitKindCtrlCFallback},
		{"user", "user", exitKindCtrlCFallback},
		{"other_plugin_reviewbot", "reviewbot", exitKindCtrlCFallback},
		{"other_plugin_hermes", "hermes", exitKindCtrlCFallback},
		{"bare_whitespace", "   ", exitKindCtrlCFallback},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveAgentExitKind(tt.agentType); got != tt.want {
				t.Errorf("resolveAgentExitKind(%q) = %q, want %q", tt.agentType, got, tt.want)
			}
		})
	}
}

// TestResolveAgentExitKind_KindValues pins the JSON-facing kind labels so
// robot-status / RestartSequence.ExitMethod consumers keep a stable contract.
func TestResolveAgentExitKind_KindValues(t *testing.T) {
	want := map[agentExitKind]string{
		exitKindDoubleCtrlC:   "double_ctrl_c",
		exitKindExitCommand:   "exit_command",
		exitKindEscapeExit:    "escape_then_exit",
		exitKindCtrlCFallback: "ctrl_c_fallback",
	}
	for kind, wantStr := range want {
		if string(kind) != wantStr {
			t.Errorf("kind label %q = %q, want %q", kind, string(kind), wantStr)
		}
	}
}

// TestExitAgentDispatchesByKind verifies exitAgent routes each kind to the
// expected exit routine by inspecting the RestartSequence.ExitMethod label the
// routine records before any tmux I/O. The exit routines set ExitMethod first,
// then send keys, so even when tmux is unavailable (no live session in CI) the
// label is observable. This keeps the test hermetic while still exercising the
// real exitAgent dispatch end-to-end. See bd-tgbow.
func TestExitAgentDispatchesByKind(t *testing.T) {
	tests := []struct {
		name          string
		agentType     string
		wantExitLabel string
	}{
		{"claude_double_ctrl_c", "cc", "double_ctrl_c"},
		{"codex_exit_command", "cod", "exit_command"},
		{"gemini_escape_exit", "gmi", "escape_then_exit"},
		{"pi_double_ctrl_c", "pi", "double_ctrl_c"},
		{"pia_double_ctrl_c", "pia", "double_ctrl_c"},
	}
	if !tmux.IsInstalled() {
		t.Skip("tmux not installed; dispatch label is set before any tmux send, but the routine still invokes tmux")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seq := &RestartSequence{}
			// exitAgent returns an error when tmux cannot reach the (fake)
			// session; that is expected and irrelevant here — the exit routine
			// records ExitMethod before any tmux send, so the label is set.
			// The nonexistent session name guarantees no real pane is affected.
			_ = exitAgent("ntm-test-nonexistent-session-bd-tgbow", 1, 1, tt.agentType, seq)
			if seq.ExitMethod != tt.wantExitLabel {
				t.Errorf("exitAgent(%q): seq.ExitMethod = %q, want %q",
					tt.agentType, seq.ExitMethod, tt.wantExitLabel)
			}
		})
	}
}
