package plugins

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
