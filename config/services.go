// Package config — declarative service definitions for Phase 4.
//
// A services file (default /etc/tenax/services.yaml) declares the desired
// state for each managed service: which systemd unit to expect, which port
// it listens on, how to health-check it, what it depends on, and what
// environment variables it needs.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ServicesFile is the top-level structure of a services.yaml.
type ServicesFile struct {
	Services map[string]Service `yaml:"services"`
}

// Service is the declarative definition of one managed service.
type Service struct {
	// Unit is the systemd unit name (e.g. "nginx.service"). Required.
	Unit string `yaml:"unit"`

	// Port is the TCP port the service listens on (used for quick reachability
	// checks and display; not the same as the health probe).
	Port int `yaml:"port,omitempty"`

	// Health configures the health-check probe for this service.
	Health HealthCheck `yaml:"health,omitempty"`

	// DependsOn lists service names (keys in the same file) that must be
	// healthy before this service is considered ready. Used by the plan
	// engine to order starts.
	DependsOn []string `yaml:"depends_on,omitempty"`

	// Env holds static environment variables that the service needs. Values
	// may reference secrets via $secret:NAME, resolved at apply time.
	Env map[string]string `yaml:"env,omitempty"`

	// Secrets lists secret keys to load from the secrets source. The
	// resolved values are merged into Env (secret keys win on conflict).
	Secrets []string `yaml:"secrets,omitempty"`

	// Restart is the desired restart policy: "always", "on-failure", or
	// empty (no change). Maps to systemd Restart=.
	Restart string `yaml:"restart,omitempty"`

	// Template is an optional Go text/template applied to the unit file at
	// /etc/systemd/system/{unit}. Template variables come from Env and
	// per-host vars (Phase 4.4).
	Template string `yaml:"template,omitempty"`
}

// HealthCheck describes how to probe the service for liveness.
type HealthCheck struct {
	// Type: "http", "port", "exec", or empty (no probe).
	Type string `yaml:"type,omitempty"`

	// Target is the probe target: URL for http, host:port for port, command
	// string for exec.
	Target string `yaml:"target,omitempty"`

	// Interval between probes (default 30s).
	Interval Duration `yaml:"interval,omitempty"`

	// Timeout for a single probe attempt (default 5s).
	Timeout Duration `yaml:"timeout,omitempty"`

	// Retries is the number of consecutive failures before marking down
	// (default 0 = immediate).
	Retries int `yaml:"retries,omitempty"`
}

// Duration wraps time.Duration for YAML unmarshalling from strings like "10s".
type Duration struct{ D time.Duration }

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.D = parsed
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	if d.D == 0 {
		return "", nil
	}
	return d.D.String(), nil
}

// AsDuration returns the underlying time.Duration (zero if unset).
func (d Duration) AsDuration() time.Duration {
	return d.D
}

// DefaultServicesPath returns the default search path for services.yaml.
func DefaultServicesPath() string {
	if p := os.Getenv("TENAX_SERVICES"); p != "" {
		return p
	}
	return "/etc/tenax/services.yaml"
}

// LoadServicesFile parses a services.yaml from disk.
func LoadServicesFile(path string) (*ServicesFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseServices(data)
}

// ParseServices parses a services.yaml from bytes.
func ParseServices(data []byte) (*ServicesFile, error) {
	var sf ServicesFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parse services: %w", err)
	}
	if sf.Services == nil {
		sf.Services = make(map[string]Service)
	}
	return &sf, nil
}

// Validate checks the service definitions for structural errors.
func (sf *ServicesFile) Validate() error {
	for name, svc := range sf.Services {
		if svc.Unit == "" {
			return fmt.Errorf("service %q: unit is required", name)
		}
		if svc.Health.Type != "" && svc.Health.Target == "" {
			return fmt.Errorf("service %q: health target is required when type is set", name)
		}
		if svc.Health.Type != "" && svc.Health.Type != "http" && svc.Health.Type != "port" && svc.Health.Type != "exec" {
			return fmt.Errorf("service %q: unknown health type %q (use http, port, or exec)", name, svc.Health.Type)
		}
		if svc.Port < 0 || svc.Port > 65535 {
			return fmt.Errorf("service %q: invalid port %d", name, svc.Port)
		}
		for _, dep := range svc.DependsOn {
			if _, ok := sf.Services[dep]; !ok {
				return fmt.Errorf("service %q: depends_on unknown service %q", name, dep)
			}
		}
		if svc.Restart != "" && svc.Restart != "always" && svc.Restart != "on-failure" {
			return fmt.Errorf("service %q: restart must be 'always' or 'on-failure'", name)
		}
	}
	// Check for dependency cycles (simple DFS).
	if err := checkCycles(sf.Services); err != nil {
		return err
	}
	return nil
}

// checkCycles detects circular dependencies using DFS.
func checkCycles(services map[string]Service) error {
	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // done
	)
	color := make(map[string]int, len(services))

	var visit func(name string) error
	visit = func(name string) error {
		switch color[name] {
		case gray:
			return fmt.Errorf("dependency cycle involving service %q", name)
		case black:
			return nil
		}
		color[name] = gray
		for _, dep := range services[name].DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		color[name] = black
		return nil
	}

	for name := range services {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

// TopoSort returns service names in dependency order (leaves first).
func (sf *ServicesFile) TopoSort() []string {
	visited := make(map[string]bool, len(sf.Services))
	result := make([]string, 0, len(sf.Services))

	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		for _, dep := range sf.Services[name].DependsOn {
			visit(dep)
		}
		result = append(result, name)
	}

	for name := range sf.Services {
		visit(name)
	}
	return result
}
