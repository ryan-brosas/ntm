package cli

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/output"
)

func TestResolveAddAgentCommandTemplate_Ollama(t *testing.T) {

	oldCfg := cfg
	defer func() {
		cfg = oldCfg
	}()

	cfg = config.Default()
	cfg.Agents.Ollama = "ollama run {{shellQuote (.Model | default \"codellama:latest\")}}"

	cmd, env, err := resolveAddAgentCommandTemplate(AgentTypeOllama, nil, "http://127.0.0.1:11434")
	if err != nil {
		t.Fatalf("resolveAddAgentCommandTemplate() error = %v", err)
	}
	if cmd != cfg.Agents.Ollama {
		t.Fatalf("resolveAddAgentCommandTemplate() cmd = %q, want %q", cmd, cfg.Agents.Ollama)
	}
	if env["OLLAMA_HOST"] != "http://127.0.0.1:11434" {
		t.Fatalf("resolveAddAgentCommandTemplate() env OLLAMA_HOST = %q", env["OLLAMA_HOST"])
	}
}

func TestNewAddCmd_RegistersOllamaFlag(t *testing.T) {

	cmd := newAddCmd()
	if cmd.Flags().Lookup("ollama") == nil {
		t.Fatal("expected add command to register --ollama")
	}
}

// TestAddThreadsReasoningEffort is the ntm#195 regression guard. The `add`
// command parses the `:effort` segment of `--cc=N:model:effort` into the
// AgentSpec/FlatAgent, but runAdd previously omitted ReasoningEffort from the
// AgentTemplateVars handed to GenerateAgentCommand. The Claude template only
// emits `--effort` under `{{if .ReasoningEffort}}`, so the segment was
// silently dropped and the added pane launched at the CLI default — the same
// class of bug fixed for `spawn` in ntm#188. This drives the real
// parse→Flatten→render path the add loop uses and asserts the effort flows
// through, with a negative control proving an unset effort leaves no flag.
func TestAddThreadsReasoningEffort(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = config.Default()

	// Parse exactly as the --cc flag would, then flatten to the per-pane agent
	// the runAdd loop iterates over.
	spec, err := ParseAgentSpec("1:claude-opus-4-8:xhigh")
	if err != nil {
		t.Fatalf("ParseAgentSpec error = %v", err)
	}
	spec.Type = AgentTypeClaude
	flat := AgentSpecs{spec}.Flatten()
	if len(flat) != 1 {
		t.Fatalf("Flatten() len = %d, want 1", len(flat))
	}
	agent := flat[0]
	if agent.ReasoningEffort != "xhigh" {
		t.Fatalf("FlatAgent.ReasoningEffort = %q, want xhigh", agent.ReasoningEffort)
	}

	// Mirror runAdd's render: thread the flattened agent's effort into the vars.
	withEffort, err := config.GenerateAgentCommand(cfg.Agents.Claude, config.AgentTemplateVars{
		Model:           ResolveModel(agent.Type, agent.Model),
		ReasoningEffort: agent.ReasoningEffort,
	})
	if err != nil {
		t.Fatalf("GenerateAgentCommand (with effort) error = %v", err)
	}
	// The Claude template shell-quotes the value: `--effort 'xhigh'`.
	if !strings.Contains(withEffort, "--effort 'xhigh'") {
		t.Errorf("add render dropped reasoning effort: got %q, want it to contain %q", withEffort, "--effort 'xhigh'")
	}

	// Negative control: no effort parsed → no dangling --effort flag.
	noEffortSpec, err := ParseAgentSpec("1:claude-opus-4-8")
	if err != nil {
		t.Fatalf("ParseAgentSpec (no effort) error = %v", err)
	}
	noEffortSpec.Type = AgentTypeClaude
	noEffortAgent := AgentSpecs{noEffortSpec}.Flatten()[0]
	noEffort, err := config.GenerateAgentCommand(cfg.Agents.Claude, config.AgentTemplateVars{
		Model:           ResolveModel(noEffortAgent.Type, noEffortAgent.Model),
		ReasoningEffort: noEffortAgent.ReasoningEffort,
	})
	if err != nil {
		t.Fatalf("GenerateAgentCommand (no effort) error = %v", err)
	}
	if strings.Contains(noEffort, "--effort") {
		t.Errorf("unset effort left a dangling flag: %q", noEffort)
	}
}

// TestAddThreadsCodexReasoningEffort is the ntm#208 regression guard. Issue
// #208 reproduced against v1.18.3 (commit 6615dd7), which predates the ntm#195
// `add` fix: `ntm add --cod=1:MODEL:EFFORT` parsed the third spec field but
// runAdd handed GenerateAgentCommand an empty ReasoningEffort, so the default
// Codex template's `{{.ReasoningEffort | default "xhigh"}}` always emitted the
// fallback rather than the requested effort. This drives the real
// parse(--cod=1:model:low)→Flatten→render path the add loop uses against the
// default Codex template and asserts the requested effort reaches
// `model_reasoning_effort='low'`, with a negative control proving an unset
// effort falls back to the template default.
func TestAddThreadsCodexReasoningEffort(t *testing.T) {
	oldCfg := cfg
	defer func() { cfg = oldCfg }()
	cfg = config.Default()

	// Parse exactly as the --cod flag would, then flatten to the per-pane agent
	// the runAdd loop iterates over.
	spec, err := ParseAgentSpec("1:gpt-5.3-codex-spark:low")
	if err != nil {
		t.Fatalf("ParseAgentSpec error = %v", err)
	}
	spec.Type = AgentTypeCodex
	flat := AgentSpecs{spec}.Flatten()
	if len(flat) != 1 {
		t.Fatalf("Flatten() len = %d, want 1", len(flat))
	}
	agent := flat[0]
	if agent.ReasoningEffort != "low" {
		t.Fatalf("FlatAgent.ReasoningEffort = %q, want low", agent.ReasoningEffort)
	}

	// Mirror runAdd's render: thread the flattened agent's effort into the vars.
	withEffort, err := config.GenerateAgentCommand(cfg.Agents.Codex, config.AgentTemplateVars{
		Model:           ResolveModel(agent.Type, agent.Model),
		ReasoningEffort: agent.ReasoningEffort,
	})
	if err != nil {
		t.Fatalf("GenerateAgentCommand (with effort) error = %v", err)
	}
	// The Codex template shell-quotes the value: `model_reasoning_effort='low'`.
	if !strings.Contains(withEffort, "model_reasoning_effort='low'") {
		t.Errorf("add render dropped Codex reasoning effort: got %q, want it to contain %q", withEffort, "model_reasoning_effort='low'")
	}

	// Negative control: no effort parsed → template default (not 'low').
	noEffortSpec, err := ParseAgentSpec("1:gpt-5.3-codex-spark")
	if err != nil {
		t.Fatalf("ParseAgentSpec (no effort) error = %v", err)
	}
	noEffortSpec.Type = AgentTypeCodex
	noEffortAgent := AgentSpecs{noEffortSpec}.Flatten()[0]
	noEffort, err := config.GenerateAgentCommand(cfg.Agents.Codex, config.AgentTemplateVars{
		Model:           ResolveModel(noEffortAgent.Type, noEffortAgent.Model),
		ReasoningEffort: noEffortAgent.ReasoningEffort,
	})
	if err != nil {
		t.Fatalf("GenerateAgentCommand (no effort) error = %v", err)
	}
	if strings.Contains(noEffort, "model_reasoning_effort='low'") {
		t.Errorf("unset effort should not render low: %q", noEffort)
	}
}

func TestAddResponseJSONIncludesOllama(t *testing.T) {

	data, err := json.Marshal(output.AddResponse{
		AddedClaude: 1,
		AddedOllama: 2,
		TotalAdded:  3,
	})
	if err != nil {
		t.Fatalf("json.Marshal(AddResponse) error = %v", err)
	}

	encoded := string(data)
	if !strings.Contains(encoded, "\"added_ollama\":2") {
		t.Fatalf("AddResponse JSON = %s, want added_ollama field", encoded)
	}
}

func TestCombineAddPrompts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cassContext string
		userPrompt  string
		want        string
	}{
		{name: "empty", want: ""},
		{name: "context only", cassContext: "past context", want: "past context"},
		{name: "user only", userPrompt: "do the work", want: "do the work"},
		{name: "combined once", cassContext: "past context", userPrompt: "do the work", want: "past context\n\ndo the work"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := combineAddPrompts(tt.cassContext, tt.userPrompt); got != tt.want {
				t.Fatalf("combineAddPrompts(%q, %q) = %q, want %q", tt.cassContext, tt.userPrompt, got, tt.want)
			}
		})
	}
}

func TestIsPiAddAgentType(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		agentType AgentType
		want      bool
	}{
		{agentType: AgentType("pi"), want: true},
		{agentType: AgentType("pia"), want: true},
		{agentType: AgentType(" PI "), want: true},
		{agentType: AgentTypeClaude, want: false},
		{agentType: AgentTypeCodex, want: false},
	} {
		if got := isPiAddAgentType(tt.agentType); got != tt.want {
			t.Errorf("isPiAddAgentType(%q) = %v, want %v", tt.agentType, got, tt.want)
		}
	}
}

func TestDeliverAddedPiPanePromptWaitsThenSubmitsExactlyOnce(t *testing.T) {
	t.Parallel()

	var events []string
	hooks := addPromptDeliveryHooks{
		waitReady: func(paneID string, timeout time.Duration) (bool, error) {
			events = append(events, "wait:"+paneID)
			return true, nil
		},
		sendSingle: func(paneID, prompt string) error {
			events = append(events, "single:"+paneID+":"+prompt)
			return nil
		},
	}

	ready, err := deliverAddedPiPanePromptWithHooks(hooks, "%13", "critical plan", 30*time.Second)
	if err != nil {
		t.Fatalf("deliverAddedPiPanePromptWithHooks() error = %v", err)
	}
	if !ready {
		t.Fatal("deliverAddedPiPanePromptWithHooks() ready = false, want true")
	}
	want := []string{"wait:%13", "single:%13:critical plan"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %#v, want %#v", events, want)
	}
}

func TestDeliverAddedPiPanePromptDoesNotTypeBeforeReady(t *testing.T) {
	t.Parallel()

	var sends int
	hooks := addPromptDeliveryHooks{
		waitReady: func(string, time.Duration) (bool, error) { return false, nil },
		sendSingle: func(string, string) error {
			sends++
			return nil
		},
	}

	ready, err := deliverAddedPiPanePromptWithHooks(hooks, "%13", "critical plan", time.Millisecond)
	if err != nil {
		t.Fatalf("deliverAddedPiPanePromptWithHooks() error = %v", err)
	}
	if ready {
		t.Fatal("deliverAddedPiPanePromptWithHooks() ready = true, want false")
	}
	if sends != 0 {
		t.Fatalf("send calls = %d, want 0 while pane is not ready", sends)
	}
}

func TestDeliverAddedPiPanePromptPropagatesReadinessError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("capture failed")
	var sends int
	hooks := addPromptDeliveryHooks{
		waitReady: func(string, time.Duration) (bool, error) { return false, wantErr },
		sendSingle: func(string, string) error {
			sends++
			return nil
		},
	}

	ready, err := deliverAddedPiPanePromptWithHooks(hooks, "%13", "critical plan", time.Millisecond)
	if !errors.Is(err, wantErr) {
		t.Fatalf("deliverAddedPiPanePromptWithHooks() error = %v, want %v", err, wantErr)
	}
	if ready {
		t.Fatal("deliverAddedPiPanePromptWithHooks() ready = true, want false")
	}
	if sends != 0 {
		t.Fatalf("send calls = %d, want 0 after readiness error", sends)
	}
}

func TestPiAddedPaneReadyFromFooter(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		scrollback string
		want       bool
	}{
		{name: "loading", scrollback: "loading skills...\nstarting MCP adapters...", want: false},
		{name: "connecting prose", scrollback: "MCP: connecting servers...", want: false},
		{name: "server error", scrollback: "MCP: server error", want: false},
		{name: "footer connected", scrollback: "/data/projects/ntm (main)\n(openai-codex) gpt-5.6-sol • xhigh\nMCP: 1/6 servers", want: true},
		{name: "footer zero connected", scrollback: "MCP: 0/6 servers", want: true},
		{name: "startup banner", scrollback: "MCP: 1 servers connected (63 tools)", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := piAddedPaneReady(tt.scrollback); got != tt.want {
				t.Fatalf("piAddedPaneReady() = %v, want %v for %q", got, tt.want, tt.scrollback)
			}
		})
	}
}
