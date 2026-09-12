package alert

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk alert configuration format:
//
//	sinks:
//	  - webhook: https://example.com/alerts
//	rules:
//	  - name: high-cpu
//	    kind: cpu
//	    threshold: 90
//	    for: 2m
type Config struct {
	Sinks []sinkConfig `yaml:"sinks"`
	Rules []Rule       `yaml:"rules"`
}

type sinkConfig struct {
	Webhook string `yaml:"webhook"`
	Log     bool   `yaml:"log"`
	Stdout  bool   `yaml:"stdout"`
}

// LoadFile reads alerts.yaml, parses duration strings, and returns the
// rules plus ready-to-use sinks.
func LoadFile(path string) ([]Rule, []Sink, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var sinks []Sink
	for _, s := range cfg.Sinks {
		if s.Webhook != "" {
			sinks = append(sinks, WebhookSink{URL: s.Webhook})
		}
		if s.Log || len(cfg.Sinks) == 0 {
			sinks = append(sinks, LogSink{})
		}
		if s.Stdout {
			sinks = append(sinks, StdoutSink{})
		}
	}
	if len(sinks) == 0 {
		sinks = append(sinks, LogSink{})
	}

	rules := make([]Rule, 0, len(cfg.Rules))
	for _, r := range cfg.Rules {
		if r.ForStr != "" {
			d, err := time.ParseDuration(r.ForStr)
			if err != nil {
				return nil, nil, fmt.Errorf("rule %q: invalid for %q: %w", r.Name, r.ForStr, err)
			}
			r.For = d
		}
		if r.CooldownStr != "" {
			d, err := time.ParseDuration(r.CooldownStr)
			if err != nil {
				return nil, nil, fmt.Errorf("rule %q: invalid cooldown %q: %w", r.Name, r.CooldownStr, err)
			}
			r.Cooldown = d
		}
		rules = append(rules, r)
	}
	return rules, sinks, nil
}

// DefaultSearchPaths returns the paths LoadDefault looks for, in order.
func DefaultSearchPaths() []string {
	paths := []string{}
	if p := os.Getenv("TENAX_ALERTS"); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, "/etc/tenax/alerts.yaml")

	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "tenax", "alerts.yaml"))
	}
	paths = append(paths, "alerts.yaml")
	return paths
}

// LoadDefault probes DefaultSearchPaths and returns rules and sinks from
// the first file that exists. If no file is found it returns nil, nil, nil.
func LoadDefault() ([]Rule, []Sink, error) {
	for _, path := range DefaultSearchPaths() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		return LoadFile(path)
	}
	return nil, nil, nil
}
