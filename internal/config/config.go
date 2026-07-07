// Package config handles loading/saving the YAML config at
// ~/.config/railway-tui/config.yaml, covering context, notifications,
// polling intervals, theme, and saved layouts.
package config

import (
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the persisted user configuration.
type Config struct {
	// Context defaults applied at launch.
	Project     string `yaml:"project"`
	Environment string `yaml:"environment"`

	Polling       Polling           `yaml:"polling"`
	Notifications Notifications     `yaml:"notifications"`
	Layouts       []Layout          `yaml:"layouts"`
	ActiveLayout  string            `yaml:"active_layout"`
	Theme         string            `yaml:"theme"`
	Keys          map[string]string `yaml:"keys,omitempty"`
}

// Polling controls how often background pollers refresh.
type Polling struct {
	DeploySeconds  int `yaml:"deploy_seconds"`
	MetricsSeconds int `yaml:"metrics_seconds"`
}

func (p Polling) Deploy() time.Duration {
	if p.DeploySeconds <= 0 {
		return 10 * time.Second
	}
	return time.Duration(p.DeploySeconds) * time.Second
}

func (p Polling) Metrics() time.Duration {
	if p.MetricsSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(p.MetricsSeconds) * time.Second
}

// Notifications configures the background watcher and toasts.
type Notifications struct {
	OnDeploySuccess bool     `yaml:"on_deploy_success"`
	OnDeployFail    bool     `yaml:"on_deploy_fail"`
	OnCrash         bool     `yaml:"on_crash"`
	OnLogError      bool     `yaml:"on_log_error"`
	ErrorPatterns   []string `yaml:"error_patterns"`
	MutedServices   []string `yaml:"muted_services"`
	ToastSeconds    int      `yaml:"toast_seconds"`
}

func (n Notifications) Toast() time.Duration {
	if n.ToastSeconds <= 0 {
		return 6 * time.Second
	}
	return time.Duration(n.ToastSeconds) * time.Second
}

// Muted reports whether a service is muted from notifications.
func (n Notifications) Muted(service string) bool {
	for _, m := range n.MutedServices {
		if m == service {
			return true
		}
	}
	return false
}

// Layout is a named pane arrangement.
type Layout struct {
	Name     string   `yaml:"name"`
	Primary  string   `yaml:"primary"`  // pane id filling main area, or left of split
	Split    string   `yaml:"split"`    // optional right/bottom pane id ("" = single)
	Vertical bool     `yaml:"vertical"` // true = stacked, false = side-by-side
	Ratio    float64  `yaml:"ratio"`    // primary size fraction, 0.1..0.9
	Sources  []string `yaml:"sources"`  // log source keys active in this layout
}

// Default returns the built-in default configuration.
func Default() Config {
	return Config{
		Polling: Polling{DeploySeconds: 10, MetricsSeconds: 15},
		Notifications: Notifications{
			OnDeploySuccess: true,
			OnDeployFail:    true,
			OnCrash:         true,
			OnLogError:      true,
			ErrorPatterns:   []string{"panic", "fatal", "traceback", "unhandled"},
			ToastSeconds:    6,
		},
		Theme:        "default",
		ActiveLayout: "logs+deploys",
		Layouts: []Layout{
			{Name: "logs", Primary: "logs"},
			{Name: "logs+deploys", Primary: "logs", Split: "deploys", Vertical: false, Ratio: 0.6},
			{Name: "logs+errors", Primary: "logs", Split: "errors", Vertical: true, Ratio: 0.6},
			{Name: "everything", Primary: "topology", Split: "logs", Vertical: false, Ratio: 0.4},
		},
	}
}

// Path returns the config file path, honoring XDG_CONFIG_HOME.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "railway-tui", "config.yaml")
}

// Load reads config from disk, returning defaults (merged) if absent.
func Load() (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save writes config to disk, creating the directory as needed.
func (c Config) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// LayoutByName finds a layout, falling back to the first.
func (c Config) LayoutByName(name string) Layout {
	for _, l := range c.Layouts {
		if l.Name == name {
			return l
		}
	}
	if len(c.Layouts) > 0 {
		return c.Layouts[0]
	}
	return Layout{Name: "logs", Primary: "logs"}
}
