package alert_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravi-arnan/tenax/alert"
	"github.com/ravi-arnan/tenax/pkg/health"
	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/probe"
	"github.com/ravi-arnan/tenax/sysinfo"
)

// fakeSource returns deterministic data for evaluator tests.
type fakeSource struct {
	cpuPct float64
	memPct float64
	disk   []sysinfo.DiskUsage
	units  []unit.Unit
	probes []probe.ProbeStatus
}

func (f *fakeSource) Units() []unit.Unit { return f.units }
func (f *fakeSource) SystemInfo() sysinfo.Snapshot {
	return sysinfo.Snapshot{
		CPU:    sysinfo.CPU{User: f.cpuPct, System: 0},
		Memory: sysinfo.Memory{PercentUsed: f.memPct},
		Disk:   f.disk,
	}
}
func (f *fakeSource) Probes() []probe.ProbeStatus { return f.probes }

type captureSink struct{ events []alert.Event }

func (c *captureSink) Send(e alert.Event) error {
	c.events = append(c.events, e)
	return nil
}

func TestEvaluatorCPUFiring(t *testing.T) {
	src := &fakeSource{cpuPct: 95}
	sink := &captureSink{}
	ev, err := alert.NewEvaluator(src, []alert.Rule{
		{Name: "cpu-high", Kind: alert.KindCPU, Threshold: 90, For: 2 * time.Minute},
	}, sink)
	if err != nil {
		t.Fatalf("NewEvaluator: %v", err)
	}

	t0 := time.Now()
	events := ev.EvaluateOnce(t0) // breach -> pending, no events yet
	if len(events) != 0 {
		t.Fatalf("expected no events while pending, got %+v", events)
	}

	events = ev.EvaluateOnce(t0.Add(3 * time.Minute)) // past for -> firing
	if len(events) != 1 || events[0].State != alert.StateFiring {
		t.Fatalf("expected firing event, got %+v", events)
	}
	if len(sink.events) == 0 {
		t.Fatal("expected sink to receive event")
	}

	// Recovery.
	src.cpuPct = 40
	events = ev.EvaluateOnce(t0.Add(6 * time.Minute))
	if len(events) != 1 || events[0].State != alert.StateOK {
		t.Fatalf("expected resolved event, got %+v", events)
	}
}

func TestEvaluatorZeroForFiresImmediately(t *testing.T) {
	src := &fakeSource{cpuPct: 95}
	ev, _ := alert.NewEvaluator(src, []alert.Rule{
		{Name: "cpu-high", Kind: alert.KindCPU, Threshold: 90},
	})

	events := ev.EvaluateOnce(time.Now())
	if len(events) != 1 || events[0].State != alert.StateFiring {
		t.Fatalf("expected immediate firing with for=0, got %+v", events)
	}
}

func TestEvaluatorForDurationHoldsPending(t *testing.T) {
	src := &fakeSource{memPct: 95}
	ev, _ := alert.NewEvaluator(src, []alert.Rule{
		{Name: "mem-high", Kind: alert.KindMemory, Threshold: 90, For: 5 * time.Minute},
	})

	t0 := time.Now()
	ev.EvaluateOnce(t0)
	events := ev.EvaluateOnce(t0.Add(2 * time.Minute))
	if len(events) != 0 {
		t.Fatalf("expected no events before for-duration, got %+v", events)
	}

	events = ev.EvaluateOnce(t0.Add(5*time.Minute + time.Second))
	if len(events) != 1 || events[0].State != alert.StateFiring {
		t.Fatalf("expected firing after for-duration, got %+v", events)
	}
}

func TestEvaluatorUnitFailed(t *testing.T) {
	src := &fakeSource{units: []unit.Unit{{Name: "nginx.service", ActiveState: "failed"}}}
	ev, _ := alert.NewEvaluator(src, []alert.Rule{
		{Name: "nginx-down", Kind: alert.KindUnit, Unit: "nginx.service"},
	})

	events := ev.EvaluateOnce(time.Now().Add(time.Minute))
	// First pass: pending since=now, For=0 -> now-since=0 >= 0 -> firing.
	if len(events) != 1 || events[0].State != alert.StateFiring {
		t.Fatalf("expected firing, got %+v", events)
	}

	src.units[0].ActiveState = "active"
	events = ev.EvaluateOnce(time.Now().Add(2 * time.Minute))
	if len(events) != 1 || events[0].State != alert.StateOK {
		t.Fatalf("expected resolved, got %+v", events)
	}
}

func TestEvaluatorProbeDown(t *testing.T) {
	src := &fakeSource{probes: []probe.ProbeStatus{{
		Probe:  health.Probe{Name: "web", Type: health.TypeHTTP},
		Status: probe.StatusDown,
	}}}
	ev, _ := alert.NewEvaluator(src, []alert.Rule{
		{Name: "web-down", Kind: alert.KindProbe, Probe: "web"},
	})

	events := ev.EvaluateOnce(time.Now().Add(time.Minute))
	if len(events) != 1 || events[0].State != alert.StateFiring {
		t.Fatalf("expected firing, got %+v", events)
	}
}

func TestEvaluatorDiskRule(t *testing.T) {
	src := &fakeSource{disk: []sysinfo.DiskUsage{{MountPoint: "/", PercentUsed: 95}}}
	ev, _ := alert.NewEvaluator(src, []alert.Rule{
		{Name: "root-full", Kind: alert.KindDisk, MountPoint: "/", Threshold: 90},
	})

	events := ev.EvaluateOnce(time.Now().Add(time.Minute))
	if len(events) != 1 || events[0].State != alert.StateFiring {
		t.Fatalf("expected firing, got %+v", events)
	}
}

func TestRuleValidate(t *testing.T) {
	cases := []struct {
		name    string
		rule    alert.Rule
		wantErr bool
	}{
		{"valid cpu", alert.Rule{Name: "c", Kind: alert.KindCPU, Threshold: 90}, false},
		{"bad threshold", alert.Rule{Name: "c", Kind: alert.KindCPU, Threshold: 150}, true},
		{"disk no mount", alert.Rule{Name: "d", Kind: alert.KindDisk, Threshold: 90}, true},
		{"unit missing", alert.Rule{Name: "u", Kind: alert.KindUnit}, true},
		{"probe missing", alert.Rule{Name: "p", Kind: alert.KindProbe}, true},
		{"unknown kind", alert.Rule{Name: "x", Kind: "temp"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rule.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestStatesSnapshot(t *testing.T) {
	src := &fakeSource{cpuPct: 99, memPct: 99}
	ev, _ := alert.NewEvaluator(src, []alert.Rule{
		{Name: "a", Kind: alert.KindCPU, Threshold: 50, For: time.Hour},
		{Name: "b", Kind: alert.KindMemory, Threshold: 50},
	})

	ev.EvaluateOnce(time.Now())
	states := ev.States()
	if len(states) != 2 {
		t.Fatalf("expected 2 states, got %d", len(states))
	}
	if states[0].Name != "a" || states[0].State != alert.StatePending {
		t.Errorf("expected rule a pending, got %+v", states[0])
	}
	if states[1].Name != "b" || states[1].State != alert.StateFiring {
		t.Errorf("expected rule b firing (for=0), got %+v", states[1])
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")

	content := `
sinks:
  - log: true
rules:
  - name: cpu-high
    kind: cpu
    threshold: 90
    for: 2m
  - name: nginx-down
    kind: unit
    unit: nginx.service
  - name: web-down
    kind: probe
    probe: web
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	rules, sinks, err := alert.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	if rules[0].For != 2*time.Minute {
		t.Errorf("for = %v, want 2m", rules[0].For)
	}
	if len(sinks) == 0 {
		t.Error("expected at least one sink")
	}
}

func TestLoadFileInvalidDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "alerts.yaml")
	if err := os.WriteFile(path, []byte("rules:\n  - name: x\n    kind: cpu\n    threshold: 90\n    for: bogus\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, _, err := alert.LoadFile(path); err == nil {
		t.Fatal("expected error for invalid duration")
	}
}
