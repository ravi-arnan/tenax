package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ravi-arnan/tenax/config"
)

func TestLoadFileFull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenax.yaml")

	content := `
api:
  listen: 0.0.0.0:9999
agent:
  units_interval_sec: 10
  sysinfo_interval_sec: 15
  unit_history_size: 100
  resource_history_size: 200
probes:
  file: /etc/tenax/probes.yaml
alerts:
  file: /etc/tenax/alerts.yaml
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.API.Listen != "0.0.0.0:9999" {
		t.Errorf("listen = %q", cfg.API.Listen)
	}
	if cfg.Agent.UnitsIntervalSec != 10 {
		t.Errorf("units interval = %d", cfg.Agent.UnitsIntervalSec)
	}
	if cfg.Agent.ResourceHistorySize != 200 {
		t.Errorf("resource history = %d", cfg.Agent.ResourceHistorySize)
	}
	if cfg.Probes.File != "/etc/tenax/probes.yaml" {
		t.Errorf("probes file = %q", cfg.Probes.File)
	}
}

func TestLoadFileDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenax.yaml")
	if err := os.WriteFile(path, []byte("api:\n  listen: 1.2.3.4:5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Agent.UnitsIntervalSec != config.DefaultUnitsIntervalSec {
		t.Errorf("units interval default = %d", cfg.Agent.UnitsIntervalSec)
	}
	if cfg.Agent.UnitHistorySize != config.DefaultUnitHistorySize {
		t.Errorf("unit history default = %d", cfg.Agent.UnitHistorySize)
	}
}

func TestLoadNoFile(t *testing.T) {
	t.Setenv("TENAX_CONFIG", "/nonexistent/tenax.yaml")

	// Also ensure the other search paths don't exist in the test env.
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agent.UnitsIntervalSec != config.DefaultUnitsIntervalSec {
		t.Errorf("expected defaults, got %+v", cfg.Agent)
	}
}

func TestLoadFileBadYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tenax.yaml")
	if err := os.WriteFile(path, []byte("agent: [unclosed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err == nil {
		t.Fatal("expected parse error")
	}
}
