package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dicklesworthstone/ntm/internal/config"
	"github.com/Dicklesworthstone/ntm/internal/plugins"
	"github.com/Dicklesworthstone/ntm/internal/session"
)

// TestBuildAgentCommands_RendersTemplates covers the #175 unrendered-template
// launch bug: resume/restore used to pass cfg.Agents.* (raw Go templates, e.g.
// `{{memLimitPrefix}} claude ...`) straight into AgentCommands, so the shell
// tried to exec a literal command named `{{memLimitPrefix}}` and the agent
// never launched. buildAgentCommands must render the templates so a concrete
// command (no `{{`/`}}` markers) reaches the pane.
func TestBuildAgentCommands_RendersTemplates(t *testing.T) {
	prevCfg := cfg
	cfg = config.Default()
	t.Cleanup(func() { cfg = prevCfg })

	// Sanity: the configured templates really do contain template syntax,
	// otherwise this test would pass vacuously.
	if !strings.Contains(cfg.Agents.Claude, "{{") {
		t.Fatalf("precondition failed: default Claude template has no template syntax: %q", cfg.Agents.Claude)
	}

	state := &session.SessionState{Name: "demo", WorkDir: "/data/projects/demo"}
	cmds := buildAgentCommands(state)

	check := func(name, got string) {
		if got == "" {
			return // empty is fine (agent not configured / render skipped)
		}
		if strings.Contains(got, "{{") || strings.Contains(got, "}}") {
			t.Errorf("%s command still contains unrendered template markers: %q", name, got)
		}
	}
	check("claude", cmds.Claude)
	check("codex", cmds.Codex)
	check("gemini", cmds.Gemini)
	check("cursor", cmds.Cursor)
	check("windsurf", cmds.Windsurf)
	check("aider", cmds.Aider)
	check("opencode", cmds.Opencode)
	check("ollama", cmds.Ollama)

	// The rendered Claude command must actually invoke `claude` (proving the
	// template body survived rendering, not just that markers were stripped).
	if cmds.Claude == "" || !strings.Contains(cmds.Claude, "claude") {
		t.Errorf("rendered Claude command = %q, want a concrete `claude ...` invocation", cmds.Claude)
	}
}

// TestBuildAgentCommands_NilConfig verifies the helper is safe when cfg is nil
// (no config loaded): it must return empty commands rather than panicking, so
// the launch path simply skips agents.
func TestBuildAgentCommands_NilConfig(t *testing.T) {
	prevCfg := cfg
	cfg = nil
	t.Cleanup(func() { cfg = prevCfg })

	cmds := buildAgentCommands(&session.SessionState{Name: "x", WorkDir: "/tmp/x"})
	if cmds.Claude != "" || cmds.Codex != "" || cmds.Gemini != "" {
		t.Errorf("expected empty commands with nil cfg, got %+v", cmds)
	}
}

// TestApplyModelCommands_HonorsCapturedModel covers ntm-boi0: resume/restore
// must relaunch each agent on its captured model, not the account default. The
// helper renders the pane's model into PaneState.Command (which the session
// launch path prefers). Panes without a captured model keep Command empty and
// fall back to the no-model type-default command.
func TestApplyModelCommands_HonorsCapturedModel(t *testing.T) {
	prevCfg := cfg
	cfg = config.Default()
	t.Cleanup(func() { cfg = prevCfg })

	state := &session.SessionState{
		Name:    "demo",
		WorkDir: "/data/projects/demo",
		Panes: []session.PaneState{
			{Index: 1, AgentType: "cc", Model: "opus"},
			{Index: 2, AgentType: "cc"}, // no captured model
		},
	}
	applyModelCommands(state)

	withModel := state.Panes[0].Command
	if withModel == "" {
		t.Fatalf("model pane Command is empty; expected a rendered launch command")
	}
	if strings.Contains(withModel, "{{") || strings.Contains(withModel, "}}") {
		t.Errorf("model pane Command still has unrendered template markers: %q", withModel)
	}
	if !strings.Contains(withModel, "--model") {
		t.Errorf("model pane Command missing --model flag: %q", withModel)
	}
	if !strings.Contains(withModel, "claude") {
		t.Errorf("model pane Command missing claude invocation: %q", withModel)
	}
	if state.Panes[1].Command != "" {
		t.Errorf("no-model pane Command should stay empty (type-default fallback), got %q", state.Panes[1].Command)
	}
}

// TestApplyModelCommands_NilConfigSafe verifies the helper is a no-op (no panic)
// when no config is loaded, leaving Command empty so launch falls back cleanly.
func TestApplyModelCommands_NilConfigSafe(t *testing.T) {
	prevCfg := cfg
	cfg = nil
	t.Cleanup(func() { cfg = prevCfg })

	state := &session.SessionState{
		Panes: []session.PaneState{{Index: 1, AgentType: "cc", Model: "opus"}},
	}
	applyModelCommands(state) // must not panic
	if state.Panes[0].Command != "" {
		t.Errorf("nil cfg should leave Command empty, got %q", state.Panes[0].Command)
	}
}

// TestRunSessionsShow_LoadFailureRoutesThroughJSONEnvelope covers bd-1yws7:
// when --json is set, runSessionsShow's session.Load failure path must emit
// a parseable JSON envelope and propagate errJSONFailure so automation
// gating on `$?` no longer treats a missing/corrupt saved-session as
// success. Pre-fix the function returned the raw err, which under --json
// surfaced as a stderr "Error:" line and empty stdin to jq.
func TestRunSessionsShow_LoadFailureRoutesThroughJSONEnvelope(t *testing.T) {
	prevJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = prevJSON })

	origStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe error = %v", pipeErr)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()

	// Empty name trips normalizeSavedSessionName inside session.Load,
	// which is the deterministic failure surface for runSessionsShow.
	err := runSessionsShow("")
	_ = w.Close()
	<-done

	if !errors.Is(err, errJSONFailure) {
		t.Fatalf("runSessionsShow returned %v, want errJSONFailure (load failure must route through emitJSONFailureEnvelope under --json)", err)
	}
}

// TestRunSessionsDelete_NotFoundRoutesThroughJSONEnvelope covers bd-1yws7:
// runSessionsDelete previously returned a raw fmt.Errorf for the missing-
// session path, which bypassed --json and forced automation to parse
// stderr text. The fix routes the error through emitJSONFailureEnvelope so
// `ntm sessions delete --json | jq` sees a parseable failure on stdout and
// the process exits non-zero via errJSONFailure.
func TestRunSessionsDelete_NotFoundRoutesThroughJSONEnvelope(t *testing.T) {
	prevJSON := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = prevJSON })

	origStdout := os.Stdout
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatalf("os.Pipe error = %v", pipeErr)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()

	err := runSessionsDelete("ntm-bd-1yws7-nonexistent-12345-do-not-exist", false)
	_ = w.Close()
	<-done

	if !errors.Is(err, errJSONFailure) {
		t.Fatalf("runSessionsDelete returned %v, want errJSONFailure (not-found path must emit JSON envelope under --json)", err)
	}
}

// writePiPluginFixture creates a temp config dir holding an `agents/pi.toml`
// plugin (with an env var + default model) and returns the config-file path
// callers should assign to the package-level cfgFile so selectedConfigDir()
// resolves to the temp dir. Used by the bd-jsqbf plugin-relaunch regression
// tests below.
func writePiPluginFixture(t *testing.T) string {
	t.Helper()
	configDir := t.TempDir()
	agentsDir := filepath.Join(configDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(agents) failed: %v", err)
	}
	piTOML := `[agent]
name = "pi"
alias = "pia"
command = "pi{{if .Model}} --model {{shellQuote .Model}}{{end}}"

[agent.env]
PI_TOKEN = "secret"

[agent.defaults]
model = "grok-default"
`
	if err := os.WriteFile(filepath.Join(agentsDir, "pi.toml"), []byte(piTOML), 0o644); err != nil {
		t.Fatalf("WriteFile(pi.toml) failed: %v", err)
	}
	configPath := filepath.Join(configDir, "ntm.toml")
	if err := os.WriteFile(configPath, []byte("projects_base = \"/tmp\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(ntm.toml) failed: %v", err)
	}
	return configPath
}

// TestAgentTemplateAndType_PluginFallback covers bd-jsqbf point 4: the helper
// must resolve a plugin agent type (pi/pia) to its plugin command template,
// a model-resolution AgentType, and the plugin env map, so applyModelCommands
// re-renders a captured model into the plugin pane instead of dropping it.
// Built-in types are unchanged (nil env), and unknown types return ok=false.
func TestAgentTemplateAndType_PluginFallback(t *testing.T) {
	prevCfg := cfg
	cfg = config.Default()
	t.Cleanup(func() { cfg = prevCfg })

	pi := plugins.AgentPlugin{Name: "pi", Alias: "pia", Command: "pi --model {{.Model}}", Env: map[string]string{"PI_TOKEN": "secret"}}
	pi.Defaults.Model = "grok-default"
	lookup := map[string]plugins.AgentPlugin{
		"pi":  pi,
		"pia": pi,
	}

	// Plugin type by canonical name resolves to the plugin spec.
	tmpl, cliType, env, ok := agentTemplateAndType("pi", lookup)
	if !ok {
		t.Fatalf("agentTemplateAndType(pi) = ok=false; want true")
	}
	if tmpl != "pi --model {{.Model}}" {
		t.Errorf("pi template = %q, want %q", tmpl, "pi --model {{.Model}}")
	}
	if cliType != AgentType("pi") {
		t.Errorf("pi cliType = %q, want %q", cliType, AgentType("pi"))
	}
	if env["PI_TOKEN"] != "secret" {
		t.Errorf("pi env = %v, want PI_TOKEN=secret", env)
	}

	// Plugin alias resolves too (saved pane types carry the canonical Name,
	// but aliases must work for symmetry with buildAgentCommands/getAgentCommand).
	if _, _, _, ok := agentTemplateAndType("pia", lookup); !ok {
		t.Errorf("agentTemplateAndType(pia) = ok=false; want true")
	}

	// Unknown type without a plugin entry returns ok=false (no panic).
	if _, _, _, ok := agentTemplateAndType("hermes", lookup); ok {
		t.Errorf("agentTemplateAndType(hermes) = ok=true; want false")
	}

	// Built-in types are unchanged: nil env, correct AgentType.
	btmpl, btype, benv, bok := agentTemplateAndType("cc", lookup)
	if !bok {
		t.Fatalf("agentTemplateAndType(cc) = ok=false; want true")
	}
	if btype != AgentTypeClaude {
		t.Errorf("cc cliType = %q, want %q", btype, AgentTypeClaude)
	}
	if benv != nil {
		t.Errorf("cc env = %v, want nil (built-ins carry no env prefix)", benv)
	}
	if !strings.Contains(btmpl, "claude") {
		t.Errorf("cc template = %q, want a claude invocation", btmpl)
	}
}

// TestPluginEnvPrefix_SortedDeterministic covers the env-prefix helper added
// for bd-jsqbf: env vars are applied as a sorted `K='V' ` shell prefix so the
// rendered plugin command is deterministic (maps have non-deterministic
// iteration order). Empty/nil env yields "".
func TestPluginEnvPrefix_SortedDeterministic(t *testing.T) {
	if got := pluginEnvPrefix(nil); got != "" {
		t.Errorf("pluginEnvPrefix(nil) = %q, want \"\"", got)
	}
	if got := pluginEnvPrefix(map[string]string{}); got != "" {
		t.Errorf("pluginEnvPrefix(empty) = %q, want \"\"", got)
	}

	// Single env var.
	if got := pluginEnvPrefix(map[string]string{"PI_TOKEN": "secret"}); got != "PI_TOKEN='secret' " {
		t.Errorf("pluginEnvPrefix(single) = %q, want %q", got, "PI_TOKEN='secret' ")
	}

	// Multiple env vars are sorted by key for determinism.
	got := pluginEnvPrefix(map[string]string{"B_KEY": "2", "A_KEY": "1"})
	want := "A_KEY='1' B_KEY='2' "
	if got != want {
		t.Errorf("pluginEnvPrefix(multi) = %q, want %q", got, want)
	}
}

// TestBuildAgentCommands_PluginRenderedWithEnv covers bd-jsqbf point 3:
// buildAgentCommands must render each plugin's command template (with the
// plugin's default model) into AgentCommands.Plugins, keyed by the lowercased
// canonical Name and alias, with the sorted env-var prefix applied — so the
// resume/restore fresh-launch fallback relaunches pi/pia panes with the same
// environment as `ntm add`/spawn. Built-in commands are unaffected.
func TestBuildAgentCommands_PluginRenderedWithEnv(t *testing.T) {
	prevCfg := cfg
	cfg = config.Default()
	prevCfgFile := cfgFile
	cfgFile = writePiPluginFixture(t)
	t.Cleanup(func() {
		cfg = prevCfg
		cfgFile = prevCfgFile
	})

	state := &session.SessionState{Name: "demo", WorkDir: "/data/projects/demo"}
	cmds := buildAgentCommands(state)

	// Canonical name key is populated and carries the env prefix + default model.
	piCmd, ok := cmds.Plugins["pi"]
	if !ok {
		t.Fatalf("cmds.Plugins[pi] missing; plugin not rendered into AgentCommands")
	}
	if !strings.HasPrefix(piCmd, "PI_TOKEN='secret' ") {
		t.Errorf("pi command missing env prefix: %q", piCmd)
	}
	if !strings.Contains(piCmd, "pi --model") {
		t.Errorf("pi command missing rendered `pi --model`: %q", piCmd)
	}
	if !strings.Contains(piCmd, "grok-default") {
		t.Errorf("pi command missing default model grok-default: %q", piCmd)
	}
	if strings.Contains(piCmd, "{{") || strings.Contains(piCmd, "}}") {
		t.Errorf("pi command still has unrendered template markers: %q", piCmd)
	}

	// Alias key is also populated (aliases never displace a canonical name).
	if _, ok := cmds.Plugins["pia"]; !ok {
		t.Errorf("cmds.Plugins[pia] missing; alias not rendered")
	}

	// Built-in Claude command is unchanged by the plugin rendering.
	if cmds.Claude == "" || !strings.Contains(cmds.Claude, "claude") {
		t.Errorf("built-in Claude command disturbed by plugin rendering: %q", cmds.Claude)
	}
}

// TestApplyModelCommands_PluginPaneKeepsModelAndEnv covers bd-jsqbf point 4:
// applyModelCommands must re-render a plugin pane's captured model into its
// command template (with the plugin env prefix), instead of dropping the
// model and falling back to the no-model default. A plugin pane WITHOUT a
// captured model keeps Command empty so the launch path falls back to the
// (default-model, env-prefixed) Plugins map entry from buildAgentCommands.
func TestApplyModelCommands_PluginPaneKeepsModelAndEnv(t *testing.T) {
	prevCfg := cfg
	cfg = config.Default()
	prevCfgFile := cfgFile
	cfgFile = writePiPluginFixture(t)
	t.Cleanup(func() {
		cfg = prevCfg
		cfgFile = prevCfgFile
	})

	state := &session.SessionState{
		Name:    "demo",
		WorkDir: "/data/projects/demo",
		Panes: []session.PaneState{
			{Index: 1, AgentType: "pi", Model: "grok-4.5"}, // captured model
			{Index: 2, AgentType: "pi"},                    // no captured model
		},
	}
	applyModelCommands(state)

	withModel := state.Panes[0].Command
	if withModel == "" {
		t.Fatalf("captured-model pi pane Command empty; expected a rendered launch command")
	}
	if !strings.HasPrefix(withModel, "PI_TOKEN='secret' ") {
		t.Errorf("captured-model pi pane missing env prefix: %q", withModel)
	}
	if !strings.Contains(withModel, "--model") {
		t.Errorf("captured-model pi pane missing --model flag: %q", withModel)
	}
	if !strings.Contains(withModel, "grok-4.5") {
		t.Errorf("captured-model pi pane missing captured model grok-4.5: %q", withModel)
	}
	if strings.Contains(withModel, "grok-default") {
		t.Errorf("captured-model pi pane used the plugin DEFAULT model instead of the captured one: %q", withModel)
	}
	if strings.Contains(withModel, "{{") || strings.Contains(withModel, "}}") {
		t.Errorf("captured-model pi pane has unrendered template markers: %q", withModel)
	}

	// No-model pane keeps Command empty: the fresh-launch fallback will use
	// the env-prefixed default-model Plugins entry from buildAgentCommands.
	if state.Panes[1].Command != "" {
		t.Errorf("no-model pi pane Command should stay empty (Plugins-map fallback), got %q", state.Panes[1].Command)
	}
}
