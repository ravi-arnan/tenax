package probe

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ravi-arnan/tenax/pkg/health"
	"gopkg.in/yaml.v3"
)

// Config is the on-disk probe configuration format:
//
//	probes:
//	  - name: web
//	    type: http
//	    target: http://127.0.0.1:8080/health
//	    interval: 10s
//	    timeout: 3s
//	    retries: 2
type Config struct {
	Probes []probeConfig `yaml:"probes"`
}

type probeConfig struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Target   string `yaml:"target"`
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
	Retries  int    `yaml:"retries"`
}

// LoadFile reads and parses a probe config file.
func LoadFile(path string) ([]health.Probe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	probes := make([]health.Probe, 0, len(cfg.Probes))
	for _, pc := range cfg.Probes {
		p := health.Probe{
			Name:    pc.Name,
			Type:    health.ProbeType(pc.Type),
			Target:  pc.Target,
			Retries: pc.Retries,
		}

		if pc.Interval != "" {
			d, err := time.ParseDuration(pc.Interval)
			if err != nil {
				return nil, fmt.Errorf("probe %q: invalid interval %q: %w", pc.Name, pc.Interval, err)
			}
			p.Interval = d
		}
		if pc.Timeout != "" {
			d, err := time.ParseDuration(pc.Timeout)
			if err != nil {
				return nil, fmt.Errorf("probe %q: invalid timeout %q: %w", pc.Name, pc.Timeout, err)
			}
			p.Timeout = d
		}

		probes = append(probes, p)
	}
	return probes, nil
}

// DefaultSearchPaths returns the paths LoadDefault looks for, in order.
func DefaultSearchPaths() []string {
	paths := []string{}
	if p := os.Getenv("TENAX_PROBES"); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, "/etc/tenax/probes.yaml")

	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "tenax", "probes.yaml"))
	}
	paths = append(paths, "probes.yaml")
	return paths
}

// LoadDefault probes DefaultSearchPaths and returns the probes from the
// first file that exists. If no file is found it returns nil, nil.
func LoadDefault() ([]health.Probe, error) {
	for _, path := range DefaultSearchPaths() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		return LoadFile(path)
	}
	return nil, nil
}
