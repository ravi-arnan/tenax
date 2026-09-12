// Package config loads the tenax daemon configuration from tenax.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the tenax.yaml schema.
type Config struct {
	API        APIConfig        `yaml:"api"`
	Agent      AgentConfig      `yaml:"agent"`
	Probes     ProbesConfig     `yaml:"probes"`
	Alerts     AlertsConfig     `yaml:"alerts"`
	Controller ControllerConfig `yaml:"controller,omitempty"`
}

// ControllerConfig points the agent at a tenaxc controller (multi-host).
type ControllerConfig struct {
	// Endpoint is the controller gRPC address, e.g. "ctrl.example:9731".
	// Empty disables the uplink.
	Endpoint string `yaml:"endpoint"`

	// Tags group this host in the fleet (e.g. [prod, web]).
	Tags []string `yaml:"tags,omitempty"`
}

// APIConfig configures the HTTP API server.
type APIConfig struct {
	// Listen is the address to bind, e.g. "127.0.0.1:9721". Empty means
	// the service default (TENAX_PORT or socket activation).
	Listen string `yaml:"listen"`
}

// AgentConfig tunes collection intervals and history sizes.
type AgentConfig struct {
	UnitsIntervalSec    int `yaml:"units_interval_sec,omitempty"`
	SysinfoIntervalSec  int `yaml:"sysinfo_interval_sec,omitempty"`
	UnitHistorySize     int `yaml:"unit_history_size,omitempty"`
	ResourceHistorySize int `yaml:"resource_history_size,omitempty"`
}

// ProbesConfig points at the probe definitions file.
type ProbesConfig struct {
	File string `yaml:"file"`
}

// AlertsConfig points at the alert rules file.
type AlertsConfig struct {
	File string `yaml:"file"`
}

// Defaults applied to a zero-valued config.
const (
	DefaultListen              = ""
	DefaultUnitsIntervalSec    = 3
	DefaultSysinfoIntervalSec  = 5
	DefaultUnitHistorySize     = 500
	DefaultResourceHistorySize = 720
)

// Normalize fills zero values with defaults.
func (c *Config) Normalize() {
	if c.API.Listen == "" {
		c.API.Listen = DefaultListen
	}
	if c.Agent.UnitsIntervalSec <= 0 {
		c.Agent.UnitsIntervalSec = DefaultUnitsIntervalSec
	}
	if c.Agent.SysinfoIntervalSec <= 0 {
		c.Agent.SysinfoIntervalSec = DefaultSysinfoIntervalSec
	}
	if c.Agent.UnitHistorySize <= 0 {
		c.Agent.UnitHistorySize = DefaultUnitHistorySize
	}
	if c.Agent.ResourceHistorySize <= 0 {
		c.Agent.ResourceHistorySize = DefaultResourceHistorySize
	}
}

// DefaultSearchPaths returns the paths Load looks for, in order.
func DefaultSearchPaths() []string {
	paths := []string{}
	if p := os.Getenv("TENAX_CONFIG"); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, "/etc/tenax/tenax.yaml")

	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "tenax", "tenax.yaml"))
	}
	return paths
}

// Load reads the first config file found in DefaultSearchPaths. A missing
// file anywhere returns the zero config and nil error — everything has a
// default.
func Load() (*Config, error) {
	for _, path := range DefaultSearchPaths() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		return LoadFile(path)
	}
	cfg := &Config{}
	cfg.Normalize()
	return cfg, nil
}

// LoadFile reads and parses a specific config file, applying defaults.
func LoadFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Normalize()
	return &cfg, nil
}
