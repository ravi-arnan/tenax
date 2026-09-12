package tui

import (
	"strings"
	"testing"
)

func fleetFixture() FleetData {
	return FleetData{
		Hosts: []FleetHost{
			{ID: "alpha", Status: "healthy", Tags: []string{"prod", "web"}, CPUPercent: 12.5, MemPercent: 40, UnitsTotal: 10, UnitsActive: 10},
			{ID: "beta", Status: "degraded", Tags: []string{"prod", "db"}, CPUPercent: 90, MemPercent: 95, UnitsTotal: 8, UnitsActive: 7, UnitsFailed: 1},
			{ID: "gamma", Status: "stale", Tags: nil, CPUPercent: 5},
		},
	}
}

func TestFleetTags(t *testing.T) {
	tags := fleetFixture().Tags()
	want := []string{"db", "prod", "web"}
	if len(tags) != len(want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	for i := range want {
		if tags[i] != want[i] {
			t.Fatalf("tags = %v, want %v", tags, want)
		}
	}
}

func TestFleetFilter(t *testing.T) {
	f := fleetFixture()

	if got := len(f.Filtered()); got != 3 {
		t.Fatalf("all = %d, want 3", got)
	}

	f.Group = "prod"
	got := f.Filtered()
	if len(got) != 2 || got[0].ID != "alpha" || got[1].ID != "beta" {
		t.Fatalf("prod filter = %+v", got)
	}

	f.Group = "web"
	got = f.Filtered()
	if len(got) != 1 || got[0].ID != "alpha" {
		t.Fatalf("web filter = %+v", got)
	}

	f.Group = "nope"
	if len(f.Filtered()) != 0 {
		t.Fatal("unknown group should match nothing")
	}
}

func TestRenderFleetAll(t *testing.T) {
	out := RenderFleet(fleetFixture(), 120)

	for _, want := range []string{"alpha", "beta", "gamma", "prod,web", "db,prod", "untagged", "3 hosts"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

func TestRenderFleetGrouped(t *testing.T) {
	f := fleetFixture()
	f.Group = "prod"
	out := RenderFleet(f, 120)

	if !strings.Contains(out, "group prod") {
		t.Errorf("missing group header: %s", out)
	}
	if !strings.Contains(out, "2 hosts") {
		t.Errorf("missing filtered count: %s", out)
	}
	if strings.Contains(out, "gamma") {
		t.Error("gamma should be filtered out of prod group")
	}
}

func TestRenderFleetEmpty(t *testing.T) {
	out := RenderFleet(FleetData{Group: "nope"}, 80)
	if !strings.Contains(out, "no hosts match") {
		t.Errorf("empty render wrong: %q", out)
	}
}

func TestRenderFleetNarrow(t *testing.T) {
	out := RenderFleet(fleetFixture(), 40)
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 40+10 { // small slack for ANSI codes
			t.Errorf("line exceeds width: %q", line)
		}
	}
}
