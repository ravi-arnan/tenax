package config_test

import (
	"strings"
	"testing"

	"github.com/ravi-arnan/tenax/config"
)

func TestDiffNoChanges(t *testing.T) {
	desired := &config.DesiredState{
		Services: map[string]config.DesiredService{
			"nginx": {Name: "nginx", Unit: "nginx.service", Restart: "always"},
		},
	}
	actual := &config.ActualState{
		Units: map[string]config.UnitState{
			"nginx.service": {Name: "nginx.service", ActiveState: "active"},
		},
	}

	entries := config.Diff(desired, actual)
	if len(entries) != 0 {
		t.Errorf("expected no diffs, got %d: %+v", len(entries), entries)
	}
}

func TestDiffUnitMissing(t *testing.T) {
	desired := &config.DesiredState{
		Services: map[string]config.DesiredService{
			"app": {Name: "app", Unit: "app.service"},
		},
	}
	actual := &config.ActualState{
		Units: map[string]config.UnitState{},
	}

	entries := config.Diff(desired, actual)
	if len(entries) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(entries))
	}
	if entries[0].Action != config.ActionCreate {
		t.Errorf("expected create, got %s", entries[0].Action)
	}
}

func TestDiffUnitFailed(t *testing.T) {
	desired := &config.DesiredState{
		Services: map[string]config.DesiredService{
			"web": {Name: "web", Unit: "web.service", Restart: "always"},
		},
	}
	actual := &config.ActualState{
		Units: map[string]config.UnitState{
			"web.service": {Name: "web.service", ActiveState: "failed"},
		},
	}

	entries := config.Diff(desired, actual)
	if len(entries) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(entries))
	}
	if entries[0].Action != config.ActionRestart {
		t.Errorf("expected restart, got %s", entries[0].Action)
	}
	if !strings.Contains(entries[0].Reason, "unit is failed") {
		t.Errorf("unexpected reason: %s", entries[0].Reason)
	}
}

func TestDiffUnitInactive(t *testing.T) {
	desired := &config.DesiredState{
		Services: map[string]config.DesiredService{
			"db": {Name: "db", Unit: "db.service", Restart: "on-failure"},
		},
	}
	actual := &config.ActualState{
		Units: map[string]config.UnitState{
			"db.service": {Name: "db.service", ActiveState: "inactive"},
		},
	}

	entries := config.Diff(desired, actual)
	if len(entries) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(entries))
	}
	if entries[0].Action != config.ActionStart {
		t.Errorf("expected start, got %s", entries[0].Action)
	}
}

func TestDiffInactiveNoRestartPolicy(t *testing.T) {
	desired := &config.DesiredState{
		Services: map[string]config.DesiredService{
			"optional": {Name: "optional", Unit: "optional.service"},
		},
	}
	actual := &config.ActualState{
		Units: map[string]config.UnitState{
			"optional.service": {Name: "optional.service", ActiveState: "inactive"},
		},
	}

	entries := config.Diff(desired, actual)
	// No restart policy means no action recommended for inactive.
	if len(entries) != 0 {
		t.Errorf("expected no diff for inactive without restart policy, got %d", len(entries))
	}
}

func TestDiffNilInputs(t *testing.T) {
	if got := config.Diff(nil, &config.ActualState{}); got != nil {
		t.Errorf("expected nil for nil desired")
	}
	if got := config.Diff(&config.DesiredState{}, nil); got != nil {
		t.Errorf("expected nil for nil actual")
	}
}

func TestFormatPlan(t *testing.T) {
	entries := []config.DiffEntry{
		{Service: "web", Unit: "web.service", Action: config.ActionRestart, Reason: "unit is failed"},
		{Service: "db", Unit: "db.service", Action: config.ActionStart, Reason: "should be running"},
	}

	out := config.FormatPlan(entries)
	if !strings.Contains(out, "2 change(s)") {
		t.Errorf("missing change count: %s", out)
	}
	if !strings.Contains(out, "web (web.service)") {
		t.Errorf("missing web entry: %s", out)
	}
	if !strings.Contains(out, "restart") {
		t.Errorf("missing restart: %s", out)
	}
}

func TestFormatPlanEmpty(t *testing.T) {
	out := config.FormatPlan(nil)
	if !strings.Contains(out, "No changes needed") {
		t.Errorf("expected no changes message, got %s", out)
	}
}

func TestFormatDiff(t *testing.T) {
	entries := []config.DiffEntry{
		{Service: "web", Action: config.ActionRestart},
		{Service: "db", Action: config.ActionStart},
	}
	out := config.FormatDiff(entries)
	if !strings.Contains(out, "restart web") {
		t.Errorf("missing web: %s", out)
	}
	if !strings.Contains(out, "start db") {
		t.Errorf("missing db: %s", out)
	}
}

func TestFormatDiffEmpty(t *testing.T) {
	out := config.FormatDiff(nil)
	if out != "nothing to do" {
		t.Errorf("expected 'nothing to do', got %q", out)
	}
}

func TestDiffActionString(t *testing.T) {
	cases := map[config.DiffAction]string{
		config.ActionNone:     "no change",
		config.ActionStart:    "start",
		config.ActionStop:     "stop",
		config.ActionRestart:  "restart",
		config.ActionCreate:   "create",
		config.ActionUpdate:   "update",
		config.DiffAction(99): "unknown",
	}
	for action, want := range cases {
		if got := action.String(); got != want {
			t.Errorf("Action(%d).String() = %q, want %q", action, got, want)
		}
	}
}
