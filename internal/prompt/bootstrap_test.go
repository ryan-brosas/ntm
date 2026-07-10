package prompt

import (
	"strings"
	"testing"
)

// TestComposeAgentStartupPrompt_PluginTypes verifies the AGENTS.md-first
// bootstrap contract applies identically to the pi/pia plugin agent types
// and the built-in agent types — the core bd-qkach parity requirement.
func TestComposeAgentStartupPrompt_PluginTypes(t *testing.T) {
	agentTypes := []string{"pi", "pia", "cc", "cod", "gmi", "agy", "cursor", "windsurf", "aider", "oc", "ollama"}
	userPrompt := "Work on bd-qkach: add the AGENTS.md-first bootstrap."

	for _, at := range agentTypes {
		t.Run(at, func(t *testing.T) {
			got := ComposeAgentStartupPrompt(at, userPrompt)
			if !strings.HasPrefix(got, agentsMDBootstrapPreamble) {
				t.Fatalf("agentType %q: result does not start with the bootstrap preamble.\ngot: %q", at, got)
			}
			if !strings.Contains(got, AgentsMDBootstrapMarker) {
				t.Fatalf("agentType %q: result missing bootstrap marker %q", at, AgentsMDBootstrapMarker)
			}
			if !strings.Contains(got, userPrompt) {
				t.Fatalf("agentType %q: user prompt was not preserved (must compose, not replace).\ngot: %q", at, got)
			}
		})
	}
}

// TestComposeAgentStartupPrompt_ComposesNotReplaces guarantees the contract
// composes with the user prompt rather than discarding it, and that the
// AGENTS.md instruction precedes the user's marching orders.
func TestComposeAgentStartupPrompt_ComposesNotReplaces(t *testing.T) {
	user := "Marching orders: fix the auth bug in internal/cli/login.go."
	got := ComposeAgentStartupPrompt("pi", user)

	if !strings.Contains(got, user) {
		t.Fatalf("user prompt must be preserved verbatim; got: %q", got)
	}
	preambleIdx := strings.Index(got, agentsMDBootstrapPreamble)
	userIdx := strings.Index(got, user)
	if preambleIdx < 0 || userIdx < 0 {
		t.Fatalf("missing preamble or user prompt in: %q", got)
	}
	if preambleIdx >= userIdx {
		t.Fatalf("preamble must precede the user prompt; got preamble@%d user@%d in: %q", preambleIdx, userIdx, got)
	}
}

// TestComposeAgentStartupPrompt_EmemptReturnsPreambleOnly — when no user
// prompt is supplied the bootstrap preamble alone is the startup contract.
func TestComposeAgentStartupPrompt_EmptyReturnsPreambleOnly(t *testing.T) {
	for _, at := range []string{"pi", "cc", ""} {
		got := ComposeAgentStartupPrompt(at, "")
		if got != agentsMDBootstrapPreamble {
			t.Fatalf("agentType %q: empty prompt should yield the bare preamble; got: %q", at, got)
		}
		if !strings.Contains(got, AgentsMDBootstrapMarker) {
			t.Fatalf("bare preamble must carry the marker; got: %q", got)
		}
	}
}

// TestComposeAgentStartupPrompt_NoDuplicateSpam verifies the contract is
// injected at most once: a prompt that already carries the marker is
// returned unchanged, so composing twice (e.g. controller -> spawn) never
// duplicates the preamble.
func TestComposeAgentStartupPrompt_NoDuplicateSpam(t *testing.T) {
	once := ComposeAgentStartupPrompt("pia", "do work")
	twice := ComposeAgentStartupPrompt("cc", once)
	if once != twice {
		t.Fatalf("re-composing a bootstrapped prompt must be a no-op.\nonce:  %q\ntwice: %q", once, twice)
	}
	// Count occurrences of the preamble.
	if c := strings.Count(twice, agentsMDBootstrapPreamble); c != 1 {
		t.Fatalf("preamble must appear exactly once, got %d in: %q", c, twice)
	}
}

// TestComposeAgentStartupPrompt_ExplicitOptOut verifies the designed opt-out:
// a custom prompt carrying AgentsMDBootstrapOptOut suppresses the preamble
// and the sentinel is stripped, returning the caller's prompt verbatim.
func TestComposeAgentStartupPrompt_ExplicitOptOut(t *testing.T) {
	user := "[ntm:no-agents-md-bootstrap] Just show me the diff, no preamble."
	got := ComposeAgentStartupPrompt("pi", user)
	if strings.Contains(got, agentsMDBootstrapPreamble) {
		t.Fatalf("opt-out must suppress the preamble; got: %q", got)
	}
	if strings.Contains(got, AgentsMDBootstrapOptOut) {
		t.Fatalf("opt-out sentinel must be stripped; got: %q", got)
	}
	if !strings.Contains(got, "Just show me the diff, no preamble.") {
		t.Fatalf("opt-out must preserve the rest of the prompt; got: %q", got)
	}
}

// TestComposeAgentStartupPrompt_OptOutOnlyWhitespace ensures an opt-out-only
// prompt collapses cleanly to an empty string rather than leaving junk.
func TestComposeAgentStartupPrompt_OptOutOnlyWhitespace(t *testing.T) {
	got := ComposeAgentStartupPrompt("cc", "  [ntm:no-agents-md-bootstrap]  ")
	if got != "" {
		t.Fatalf("opt-out-only prompt should collapse to empty; got: %q", got)
	}
}

// TestAgentsMDBootstrapPreamble_StableContract pins the exact contract text so
// docs/tests can rely on it. Update this test deliberately if the wording
// changes (it is the "visible and verifiable" contract from bd-qkach).
func TestAgentsMDBootstrapPreamble_StableContract(t *testing.T) {
	got := AgentsMDBootstrapPreamble()
	// The preamble must mention AGENTS.md, the root/parent + project scope,
	// the nested-AGENTS.md scope, and carry the marker sentinel.
	for _, want := range []string{"AGENTS.md", "root/parent", "nested AGENTS.md", AgentsMDBootstrapMarker} {
		if !strings.Contains(got, want) {
			t.Errorf("preamble missing required fragment %q; got: %q", want, got)
		}
	}
	if strings.HasSuffix(got, " ") {
		t.Errorf("preamble should not have trailing whitespace (marker must be the final token); got: %q", got)
	}
}

// TestHasAgentsMDBootstrap checks the helper predicate used by staged
// composers.
func TestHasAgentsMDBootstrap(t *testing.T) {
	if HasAgentsMDBootstrap("plain prompt") {
		t.Fatal("plain prompt should not report bootstrap present")
	}
	composed := ComposeAgentStartupPrompt("pi", "task")
	if !HasAgentsMDBootstrap(composed) {
		t.Fatal("composed prompt should report bootstrap present")
	}
}
