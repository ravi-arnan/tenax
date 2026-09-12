package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravi-arnan/tenax/config"
)

const testServicesYAML = `
services:
  nginx:
    unit: nginx.service
    port: 80
    health:
      type: http
      target: http://127.0.0.1:80/health
      interval: 10s
      timeout: 3s
      retries: 2
    depends_on:
      - app
    env:
      SERVER_NAME: example.com
    secrets:
      - TLS_CERT
    restart: always

  app:
    unit: app.service
    port: 8080
    health:
      type: port
      target: 127.0.0.1:8080
    env:
      NODE_ENV: production
    restart: on-failure

  worker:
    unit: worker.service
    health:
      type: exec
      target: test -f /tmp/worker-ok
      interval: 1m
    depends_on:
      - app
`

func TestParseServices(t *testing.T) {
	sf, err := config.ParseServices([]byte(testServicesYAML))
	if err != nil {
		t.Fatalf("ParseServices: %v", err)
	}

	if len(sf.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(sf.Services))
	}

	nginx := sf.Services["nginx"]
	if nginx.Unit != "nginx.service" {
		t.Errorf("nginx unit = %q", nginx.Unit)
	}
	if nginx.Port != 80 {
		t.Errorf("nginx port = %d, want 80", nginx.Port)
	}
	if nginx.Health.Type != "http" {
		t.Errorf("nginx health type = %q", nginx.Health.Type)
	}
	if nginx.Health.Target != "http://127.0.0.1:80/health" {
		t.Errorf("nginx health target = %q", nginx.Health.Target)
	}
	if nginx.Health.Interval.AsDuration() != 10*time.Second {
		t.Errorf("nginx health interval = %v", nginx.Health.Interval.AsDuration())
	}
	if nginx.Health.Retries != 2 {
		t.Errorf("nginx health retries = %d, want 2", nginx.Health.Retries)
	}
	if len(nginx.DependsOn) != 1 || nginx.DependsOn[0] != "app" {
		t.Errorf("nginx depends_on = %v", nginx.DependsOn)
	}
	if len(nginx.Secrets) != 1 || nginx.Secrets[0] != "TLS_CERT" {
		t.Errorf("nginx secrets = %v", nginx.Secrets)
	}
	if nginx.Restart != "always" {
		t.Errorf("nginx restart = %q", nginx.Restart)
	}

	app := sf.Services["app"]
	if app.Unit != "app.service" {
		t.Errorf("app unit = %q", app.Unit)
	}
	if app.Health.Type != "port" {
		t.Errorf("app health type = %q", app.Health.Type)
	}
	if app.Health.Interval.AsDuration() != 0 {
		t.Errorf("app health interval should be zero (default), got %v", app.Health.Interval.AsDuration())
	}
}

func TestValidateGood(t *testing.T) {
	sf, err := config.ParseServices([]byte(testServicesYAML))
	if err != nil {
		t.Fatal(err)
	}
	if err := sf.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateMissingUnit(t *testing.T) {
	yaml := `services:
  bad:
    port: 80
`
	sf, _ := config.ParseServices([]byte(yaml))
	if err := sf.Validate(); err == nil {
		t.Fatal("expected error for missing unit")
	}
}

func TestValidateUnknownDep(t *testing.T) {
	yaml := `services:
  a:
    unit: a.service
    depends_on:
      - nonexistent
`
	sf, _ := config.ParseServices([]byte(yaml))
	if err := sf.Validate(); err == nil {
		t.Fatal("expected error for unknown dependency")
	}
}

func TestValidateCycle(t *testing.T) {
	yaml := `services:
  a:
    unit: a.service
    depends_on: [b]
  b:
    unit: b.service
    depends_on: [a]
`
	sf, _ := config.ParseServices([]byte(yaml))
	if err := sf.Validate(); err == nil {
		t.Fatal("expected error for dependency cycle")
	}
}

func TestValidateBadHealthType(t *testing.T) {
	yaml := `services:
  x:
    unit: x.service
    health:
      type: ftp
      target: somewhere
`
	sf, _ := config.ParseServices([]byte(yaml))
	if err := sf.Validate(); err == nil {
		t.Fatal("expected error for bad health type")
	}
}

func TestValidateHealthTargetRequired(t *testing.T) {
	yaml := `services:
  x:
    unit: x.service
    health:
      type: http
`
	sf, _ := config.ParseServices([]byte(yaml))
	if err := sf.Validate(); err == nil {
		t.Fatal("expected error for missing health target")
	}
}

func TestValidateBadPort(t *testing.T) {
	yaml := `services:
  x:
    unit: x.service
    port: 99999
`
	sf, _ := config.ParseServices([]byte(yaml))
	if err := sf.Validate(); err == nil {
		t.Fatal("expected error for invalid port")
	}
}

func TestValidateBadRestart(t *testing.T) {
	yaml := `services:
  x:
    unit: x.service
    restart: always-restart
`
	sf, _ := config.ParseServices([]byte(yaml))
	if err := sf.Validate(); err == nil {
		t.Fatal("expected error for bad restart policy")
	}
}

func TestTopoSort(t *testing.T) {
	sf, err := config.ParseServices([]byte(testServicesYAML))
	if err != nil {
		t.Fatal(err)
	}
	order := sf.TopoSort()

	// app must come before nginx and worker
	idx := make(map[string]int)
	for i, name := range order {
		idx[name] = i
	}
	if idx["app"] >= idx["nginx"] {
		t.Errorf("app should come before nginx, order: %v", order)
	}
	if idx["app"] >= idx["worker"] {
		t.Errorf("app should come before worker, order: %v", order)
	}
}

func TestLoadServicesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "services.yaml")
	if err := os.WriteFile(path, []byte(testServicesYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	sf, err := config.LoadServicesFile(path)
	if err != nil {
		t.Fatalf("LoadServicesFile: %v", err)
	}
	if len(sf.Services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(sf.Services))
	}
}

func TestLoadServicesFileNotFound(t *testing.T) {
	_, err := config.LoadServicesFile("/nonexistent/services.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseEmpty(t *testing.T) {
	sf, err := config.ParseServices([]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if sf.Services == nil {
		t.Fatal("services map should not be nil")
	}
	if len(sf.Services) != 0 {
		t.Errorf("expected empty services, got %d", len(sf.Services))
	}
}
