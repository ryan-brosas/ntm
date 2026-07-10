// Package prompt provides utilities for building and manipulating prompts.
package prompt

import "strings"

// AgentsMDBootstrapMarker is a short sentinel embedded in the AGENTS.md-first
// bootstrap preamble. Callers use it to detect that a prompt has already been
// bootstrapped upstream and to avoid re-injecting the contract (no duplicate
// spam). See ComposeAgentStartupPrompt.
const AgentsMDBootstrapMarker = "[ntm:agents-md]"

// AgentsMDBootstrapOptOut is a sentinel a caller may embed in a custom prompt
// to suppress the automatic AGENTS.md-first preamble — for example when a
// prompt already inlines the full contract, or when an operator explicitly
// wants a bare prompt. Designing an explicit opt-out keeps custom prompts
// *composing* with the contract by default while still allowing an escape
// hatch (bd-qkach: "custom prompts compose rather than replace it unless an
// explicit opt-out is designed").
const AgentsMDBootstrapOptOut = "[ntm:no-agents-md-bootstrap]"

// agentsMDBootstrapPreamble is the deterministic, agent-type-agnostic
// instruction prepended to every spawned agent's startup prompt so the
// AGENTS.md-first contract is visible and verifiable across all spawn
// surfaces (cli spawn, robot spawn, controller). It is deliberately short
// and stable so tests can assert it byte-for-byte. Pi (pi-coding-agent)
// already auto-loads AGENTS.md; NTM makes the contract explicit regardless
// of agent so plugin and built-in panes behave identically.
const agentsMDBootstrapPreamble = "Before any analysis, tool calls, claims, or edits: read every applicable AGENTS.md completely (root/parent instructions and this project's AGENTS.md, plus any nested AGENTS.md for files you touch) and follow them for the whole session. " + AgentsMDBootstrapMarker

// AgentsMDBootstrapPreamble returns the deterministic AGENTS.md-first
// bootstrap preamble. Exposed so callers and tests can reference the exact
// contract text without relying on substring matches.
func AgentsMDBootstrapPreamble() string { return agentsMDBootstrapPreamble }

// ComposeAgentStartupPrompt prepends the deterministic AGENTS.md-first
// bootstrap preamble to userPrompt, composing (never replacing) the user's
// prompt/marching orders. It is agent-type-agnostic so built-in and plugin
// agents (pi/pia, …) share one contract — agentType is accepted for
// future per-type tailoring and logging but does not currently change the
// preamble.
//
// Deduplication and opt-out:
//   - If userPrompt already carries AgentsMDBootstrapMarker (bootstrapped
//     upstream, e.g. by a controller prompt) it is returned unchanged so the
//     contract is injected at most once per prompt.
//   - If userPrompt carries AgentsMDBootstrapOptOut, the preamble is
//     suppressed and the sentinel is stripped, returning the caller's custom
//     prompt verbatim.
//   - If userPrompt is empty, the preamble alone becomes the startup contract.
//
// This is the single shared chokepoint for the bd-qkach "one deterministic
// bootstrap contract" across spawn surfaces.
func ComposeAgentStartupPrompt(agentType, userPrompt string) string {
	userPrompt = strings.TrimSpace(userPrompt)
	if userPrompt == "" {
		return agentsMDBootstrapPreamble
	}
	if strings.Contains(userPrompt, AgentsMDBootstrapOptOut) {
		return strings.TrimSpace(strings.ReplaceAll(userPrompt, AgentsMDBootstrapOptOut, ""))
	}
	if strings.Contains(userPrompt, AgentsMDBootstrapMarker) {
		// Already bootstrapped upstream — don't spam a second copy.
		return userPrompt
	}
	return agentsMDBootstrapPreamble + "\n\n" + userPrompt
}

// HasAgentsMDBootstrap reports whether prompt already carries the bootstrap
// marker (i.e. it has been composed via ComposeAgentStartupPrompt or inlines
// the contract with the sentinel). Useful for callers that compose prompts
// in stages and want to decide whether to bootstrap at a later stage.
func HasAgentsMDBootstrap(p string) bool {
	return strings.Contains(p, AgentsMDBootstrapMarker)
}
