package plugins

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
)

// pluginNameRegex enforces allowed characters for plugin names (must match tmux pane regex)
var pluginNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// AgentPlugin defines a custom agent type loaded from config
type AgentPlugin struct {
	Name        string            `toml:"name"`
	Alias       string            `toml:"alias"`
	Command     string            `toml:"command"`
	Description string            `toml:"description"`
	Env         map[string]string `toml:"env"`
	Defaults    struct {
		// Model is the default model an agent of this plugin type spawns with
		// when the invocation omits an explicit model (e.g. bare `--hermes=1`).
		// Consumed by the CLI's model resolution as the lowest-precedence
		// fallback, below explicit specs and global config defaults.
		Model string   `toml:"model"`
		Tags  []string `toml:"tags"`
	} `toml:"defaults"`
}

type agentConfigFile struct {
	Agent AgentPlugin `toml:"agent"`
}

// LoadAgentPlugins scans the given directory for .toml files and loads them.
func LoadAgentPlugins(dir string) ([]AgentPlugin, error) {
	var plugins []AgentPlugin

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".toml") {
			path := filepath.Join(dir, entry.Name())
			var cfg agentConfigFile
			if _, err := toml.DecodeFile(path, &cfg); err != nil {
				log.Printf("plugins: failed to parse plugin %s: %v", entry.Name(), err)
				continue
			}

			// Set defaults/validate
			if cfg.Agent.Name == "" {
				cfg.Agent.Name = strings.TrimSuffix(entry.Name(), ".toml")
			}

			if !pluginNameRegex.MatchString(cfg.Agent.Name) {
				log.Printf("plugins: plugin %s has invalid name %q (allowed: a-z, 0-9, _, -), skipping", entry.Name(), cfg.Agent.Name)
				continue
			}

			if cfg.Agent.Command == "" {
				log.Printf("plugins: plugin %s missing 'command' field", cfg.Agent.Name)
				continue
			}

			plugins = append(plugins, cfg.Agent)
		}
	}

	return plugins, nil
}

// ResolveAgentCommand looks up an agent type in the plugin registry at
// agentsDir (by plugin name or alias) and returns the plugin's canonical
// Name, its Command template, and ok=true. Used by the controller and
// restart paths so plugin-defined agent types (e.g. "pi"/"pia") are
// first-class launch targets, mirroring the spawn/add plugin dispatch.
// Returns ok=false when agentsDir is unreadable or no plugin matches.
func ResolveAgentCommand(agentType, agentsDir string) (name, command string, ok bool) {
	loaded, err := LoadAgentPlugins(agentsDir)
	if err != nil || len(loaded) == 0 {
		return "", "", false
	}
	for _, p := range loaded {
		if p.Name == agentType || (p.Alias != "" && p.Alias == agentType) {
			return p.Name, p.Command, true
		}
	}
	return "", "", false
}

// pluginCmdIndexOnce guards one-time construction of pluginCmdIndex so the
// pane-type detector does not re-read plugin TOML on every pane parse.
var pluginCmdIndexOnce sync.Once

// pluginCmdIndex maps a lowercased match token (plugin name, alias, or the
// leading binary of its command) to the plugin's canonical name. Used by
// AgentTypeForCommand for pane-type detection.
var pluginCmdIndex map[string]string

// defaultAgentsDir mirrors config.DefaultPath()'s directory resolution so the
// low-level tmux pane-type detector can locate agent plugins WITHOUT importing
// the config package (which would create an import cycle: config imports tmux).
// Returns the "agents" directory next to the resolved config file.
func defaultAgentsDir() string {
	if env := os.Getenv("NTM_CONFIG"); env != "" {
		return filepath.Join(filepath.Dir(env), "agents")
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			base = filepath.Join(home, ".config")
		}
	}
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "ntm", "agents")
}

// firstCommandToken returns the lowercased leading binary token of a command
// template (e.g. "pi" for "pi --approve{{if .Model}} ...").
func firstCommandToken(command string) string {
	command = strings.TrimSpace(command)
	for i, r := range command {
		if r == ' ' || r == '\t' {
			return strings.ToLower(command[:i])
		}
	}
	return strings.ToLower(command)
}

// buildPluginCmdIndexFrom builds the command-match index for a given agents dir.
// Separated from AgentTypeForCommand so it is unit-testable with a temp dir.
func buildPluginCmdIndexFrom(dir string) map[string]string {
	index := make(map[string]string)
	loaded, _ := LoadAgentPlugins(dir)
	for _, p := range loaded {
		index[strings.ToLower(p.Name)] = p.Name
		if p.Alias != "" {
			index[strings.ToLower(p.Alias)] = p.Name
		}
		if tok := firstCommandToken(p.Command); tok != "" {
			index[tok] = p.Name
		}
	}
	return index
}

// matchCommandIndex reports the plugin whose token matches command using the
// isAgent-style rules (bare equality, "tok " prefix, "/tok" suffix, "/tok " in
// path). Mirrors detectAgentFromCommand's matching for built-in agents.
func matchCommandIndex(index map[string]string, command string) (string, bool) {
	cmd := strings.ToLower(strings.TrimSpace(command))
	if cmd == "" {
		return "", false
	}
	if name, ok := index[cmd]; ok {
		return name, true
	}
	for tok, name := range index {
		if strings.HasPrefix(cmd, tok+" ") ||
			strings.HasSuffix(cmd, "/"+tok) ||
			strings.Contains(cmd, "/"+tok+" ") {
			return name, true
		}
	}
	return "", false
}

// AgentTypeForCommand reports the plugin whose name, alias, or command binary
// matches the given pane command, so panes running a plugin agent (e.g. `pi`)
// classify as the plugin type instead of "user". The index is built once per
// process from the default agents dir; if a plugin is added mid-process, restart
// the process (e.g. the monitor) to pick it up.
func AgentTypeForCommand(command string) (string, bool) {
	pluginCmdIndexOnce.Do(func() {
		pluginCmdIndex = buildPluginCmdIndexFrom(defaultAgentsDir())
	})
	return matchCommandIndex(pluginCmdIndex, command)
}
