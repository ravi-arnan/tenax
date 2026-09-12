package config

import (
	"fmt"
	"sort"
	"strings"
)

// DesiredState is the target state from services.yaml, resolved with
// secrets and host variables.
type DesiredState struct {
	Services map[string]DesiredService
}

// DesiredService is one service's resolved desired state.
type DesiredService struct {
	Name       string
	Unit       string
	Port       int
	Env        map[string]string
	Health     HealthCheck
	Restart    string
	DependsOn  []string
	Template   string
}

// ActualState is what the agent observes on the host right now.
type ActualState struct {
	Units map[string]UnitState // keyed by unit name
}

// UnitState is the observed state of one systemd unit.
type UnitState struct {
	Name        string
	ActiveState string // "active", "inactive", "failed", ...
	SubState    string
	Loaded      bool
}

// DiffAction is what the plan engine recommends for a service.
type DiffAction int

const (
	ActionNone DiffAction = iota
	ActionStart
	ActionStop
	ActionRestart
	ActionCreate   // unit file doesn't exist yet
	ActionUpdate   // unit file exists but differs
)

func (a DiffAction) String() string {
	switch a {
	case ActionNone:
		return "no change"
	case ActionStart:
		return "start"
	case ActionStop:
		return "stop"
	case ActionRestart:
		return "restart"
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	}
	return "unknown"
}

// DiffEntry is one recommended action for the plan output.
type DiffEntry struct {
	Service string
	Unit    string
	Action  DiffAction
	Reason  string
}

// Diff computes the actions needed to bring actual in line with desired.
// Results are sorted in dependency order (dependencies before dependents).
func Diff(desired *DesiredState, actual *ActualState) []DiffEntry {
	if desired == nil || actual == nil {
		return nil
	}

	// Collect entries keyed by service name.
	byName := make(map[string]DiffEntry)

	for name, svc := range desired.Services {
		unit, exists := actual.Units[svc.Unit]

		if !exists {
			byName[name] = DiffEntry{
				Service: name,
				Unit:    svc.Unit,
				Action:  ActionCreate,
				Reason:  "unit not found on host",
			}
			continue
		}

		switch {
		case unit.ActiveState == "failed":
			byName[name] = DiffEntry{
				Service: name,
				Unit:    svc.Unit,
				Action:  ActionRestart,
				Reason:  "unit is failed",
			}
		case unit.ActiveState == "inactive" && svc.Restart != "":
			byName[name] = DiffEntry{
				Service: name,
				Unit:    svc.Unit,
				Action:  ActionStart,
				Reason:  "unit is inactive but should be running",
			}
		}
	}

	// Sort by dependency order (deps first).
	order := topoSortDesired(desired)
	entries := make([]DiffEntry, 0, len(byName))
	for _, name := range order {
		if e, ok := byName[name]; ok {
			entries = append(entries, e)
		}
	}

	return entries
}

// topoSortDesired returns service names in dependency order (leaves first).
func topoSortDesired(desired *DesiredState) []string {
	visited := make(map[string]bool, len(desired.Services))
	result := make([]string, 0, len(desired.Services))

	var visit func(name string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		if svc, ok := desired.Services[name]; ok {
			for _, dep := range svc.DependsOn {
				visit(dep)
		}
		}
		result = append(result, name)
	}

	// Deterministic: sort keys first, then visit.
	names := make([]string, 0, len(desired.Services))
	for name := range desired.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		visit(name)
	}
	return result
}

// FormatPlan produces a human-readable plan from a list of diffs.
func FormatPlan(entries []DiffEntry) string {
	if len(entries) == 0 {
		return "No changes needed — desired state matches actual state."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Plan: %d change(s)\n\n", len(entries)))

	for _, e := range entries {
		marker := "  "
		switch e.Action {
		case ActionRestart, ActionCreate, ActionUpdate:
			marker = "~ "
		case ActionStart:
			marker = "+ "
		case ActionStop:
			marker = "- "
		}
		b.WriteString(fmt.Sprintf("%s%s (%s) — %s\n", marker, e.Service, e.Unit, e.Action))
		if e.Reason != "" {
			b.WriteString(fmt.Sprintf("    %s\n", e.Reason))
		}
	}

	return b.String()
}

// FormatDiff produces a compact one-line-per-service diff for CLI output.
func FormatDiff(entries []DiffEntry) string {
	if len(entries) == 0 {
		return "nothing to do"
	}
	var parts []string
	for _, e := range entries {
		parts = append(parts, fmt.Sprintf("%s %s", e.Action, e.Service))
	}
	return strings.Join(parts, ", ")
}
