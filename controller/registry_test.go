package controller

import (
	"testing"
	"time"

	tenaxv1 "github.com/ravi-arnan/tenax/proto/tenaxv1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func testSnapshot(hostname string, failed int) *tenaxv1.Snapshot {
	return &tenaxv1.Snapshot{
		AgentVersion: "test-1",
		Hostname:     hostname,
		Os:           "linux/amd64",
		Kernel:       "6.8.0",
		Status:       &tenaxv1.HostStatus{UnitsTotal: 10, UnitsActive: int32(10 - failed), UnitsFailed: int32(failed)},
		Units: []*tenaxv1.UnitState{
			{Name: "nginx.service", ActiveState: "active", SubState: "running", LoadState: "loaded"},
			{Name: "db.service", ActiveState: "failed", SubState: "exited", LoadState: "loaded"},
		},
		Latest: &tenaxv1.ResourceSample{
			CpuPercent: 42.5, MemPercent: 61.0, Load1: 0.7,
			Time: timestamppb.Now(),
		},
		Probes: []*tenaxv1.ProbeState{
			{Name: "web", Type: "http", Status: "healthy", ConsecutiveFailures: 0},
		},
		SentAt: timestamppb.Now(),
	}
}

func TestRegistryUpsertAndList(t *testing.T) {
	reg := NewRegistry(2 * time.Minute)

	reg.Upsert(testSnapshot("alpha", 0), true)
	reg.Upsert(testSnapshot("beta", 2), true)
	reg.Upsert(testSnapshot("alpha", 1), true) // update

	if reg.Count() != 2 {
		t.Fatalf("count = %d, want 2", reg.Count())
	}

	hosts := reg.List()
	if len(hosts) != 2 || hosts[0].ID != "alpha" || hosts[1].ID != "beta" {
		t.Fatalf("list not sorted/complete: %+v", hosts)
	}

	alpha := hosts[0]
	if alpha.Status.GetUnitsFailed() != 1 {
		t.Errorf("alpha failed units = %d, want 1 (updated)", alpha.Status.GetUnitsFailed())
	}
	if alpha.Version != "test-1" || alpha.OS != "linux/amd64" {
		t.Errorf("metadata not stored: %+v", alpha)
	}
	if len(alpha.Units) != 2 || alpha.Units[0].Name != "nginx.service" {
		t.Errorf("units not stored: %+v", alpha.Units)
	}
	if alpha.Latest == nil || alpha.Latest.CPUPercent != 42.5 {
		t.Errorf("latest sample not stored: %+v", alpha.Latest)
	}
	if !alpha.StreamOpen {
		t.Error("stream should be open")
	}
}

func TestRegistryLiveness(t *testing.T) {
	reg := NewRegistry(2 * time.Minute)
	reg.Upsert(testSnapshot("alpha", 0), true)

	now := time.Now()
	reg.SetNow(func() time.Time { return now.Add(90 * time.Second) })
	reg.Upsert(testSnapshot("alpha", 0), true) // heartbeat

	hosts := reg.List()
	if got := hosts[0].StatusNow(reg.now(), reg.deadline); got != "healthy" {
		t.Errorf("after heartbeat = %q, want healthy", got)
	}

	// Advance past the deadline without a heartbeat.
	reg.SetNow(func() time.Time { return now.Add(5 * time.Minute) })
	if got := hosts[0].StatusNow(reg.now(), reg.deadline); got != "stale" {
		t.Errorf("after deadline = %q, want stale", got)
	}
}

func TestRegistryDegradedAndClosed(t *testing.T) {
	reg := NewRegistry(2 * time.Minute)
	reg.Upsert(testSnapshot("alpha", 3), true)

	hosts := reg.List()
	if got := hosts[0].StatusNow(reg.now(), reg.deadline); got != "degraded" {
		t.Errorf("failed units -> %q, want degraded", got)
	}

	reg.MarkClosed("alpha")
	hosts = reg.List()
	if got := hosts[0].StatusNow(reg.now(), reg.deadline); got != "disconnected" {
		t.Errorf("closed stream -> %q, want disconnected", got)
	}
	// Host still visible after close.
	if hosts[0].ID != "alpha" {
		t.Error("closed host should remain in registry")
	}
}

func TestRegistryHostnameFallback(t *testing.T) {
	reg := NewRegistry(time.Minute)
	snap := testSnapshot("", 0)
	snap.Hostname = ""
	reg.Upsert(snap, true)

	if reg.Count() != 1 || reg.List()[0].ID != "unknown" {
		t.Fatalf("expected fallback id, got %+v", reg.List())
	}
}
