package probe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravi-arnan/tenax/pkg/health"
	"github.com/ravi-arnan/tenax/probe"
)

func TestRunnerHealthyProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r, err := probe.NewRunner([]health.Probe{
		{Name: "web", Type: health.TypeHTTP, Target: srv.URL, Interval: 50 * time.Millisecond, Timeout: time.Second},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	ctx := context.Background()
	r.Start(ctx)
	defer r.Stop()

	waitFor(t, 2*time.Second, func() bool {
		st, ok := r.Get("web")
		return ok && st.Status == probe.StatusHealthy
	})
}

func TestRunnerFailingProbeGoesDown(t *testing.T) {
	r, err := probe.NewRunner([]health.Probe{
		{Name: "dead", Type: health.TypeHTTP, Target: "http://127.0.0.1:1", Interval: 50 * time.Millisecond, Timeout: time.Second},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	r.Start(context.Background())
	defer r.Stop()

	waitFor(t, 2*time.Second, func() bool {
		st, ok := r.Get("dead")
		return ok && st.Status == probe.StatusDown
	})

	st, _ := r.Get("dead")
	if st.LastResult.Error == "" {
		t.Error("expected error message on failing probe")
	}
	if r.Healthy() {
		t.Error("Healthy() should be false when a probe is down")
	}
}

func TestRunnerInvalidConfig(t *testing.T) {
	if _, err := probe.NewRunner([]health.Probe{{Name: "a", Type: "nope", Target: "x"}}); err == nil {
		t.Fatal("expected error for unknown probe type")
	}
	if _, err := probe.NewRunner([]health.Probe{
		{Name: "dup", Type: health.TypeExec, Target: "true"},
		{Name: "dup", Type: health.TypeExec, Target: "true"},
	}); err == nil {
		t.Fatal("expected error for duplicate names")
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.yaml")

	content := `
probes:
  - name: web
    type: http
    target: http://127.0.0.1:8080/health
    interval: 10s
    timeout: 3s
    retries: 2
  - name: ssh
    type: port
    target: 127.0.0.1:22
  - name: disk
    type: exec
    target: test -f /etc/hostname
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	probes, err := probe.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(probes) != 3 {
		t.Fatalf("expected 3 probes, got %d", len(probes))
	}

	web := probes[0]
	if web.Interval != 10*time.Second {
		t.Errorf("web interval = %v, want 10s", web.Interval)
	}
	if web.Timeout != 3*time.Second {
		t.Errorf("web timeout = %v, want 3s", web.Timeout)
	}
	if web.Retries != 2 {
		t.Errorf("web retries = %d, want 2", web.Retries)
	}

	ssh := probes[1]
	if ssh.Interval != 0 {
		t.Errorf("ssh interval should be unset (defaulted by runner), got %v", ssh.Interval)
	}
}

func TestLoadFileInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probes.yaml")

	if err := os.WriteFile(path, []byte("probes:\n  - name: x\n    type: http\n    interval: notaduration\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := probe.LoadFile(path); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestLoadFileMissing(t *testing.T) {
	if _, err := probe.LoadFile("/nonexistent/probes.yaml"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
