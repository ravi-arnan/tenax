// Package alert implements threshold-based alerting rules evaluated
// against sysinfo, systemd unit state, and probe status, with pluggable
// notification sinks.
package alert

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/probe"
	"github.com/ravi-arnan/tenax/sysinfo"
)

// Kind identifies what a rule watches.
type Kind string

const (
	KindCPU    Kind = "cpu"
	KindMemory Kind = "memory"
	KindDisk   Kind = "disk"
	KindUnit   Kind = "unit"
	KindProbe  Kind = "probe"
)

// Rule is a single alerting rule definition.
type Rule struct {
	Name        string        `yaml:"name"`
	Kind        Kind          `yaml:"kind"`
	Threshold   float64       `yaml:"threshold"`
	MountPoint  string        `yaml:"mountpoint,omitempty"` // disk rules
	Unit        string        `yaml:"unit,omitempty"`       // unit rules
	Probe       string        `yaml:"probe,omitempty"`      // probe rules
	For         time.Duration `yaml:"-"`                    // set via ForStr after parse
	ForStr      string        `yaml:"for,omitempty"`
	Cooldown    time.Duration `yaml:"-"`
	CooldownStr string        `yaml:"cooldown,omitempty"`
}

// Validate checks a rule for missing or contradictory fields.
func (r Rule) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	switch r.Kind {
	case KindCPU, KindMemory:
		if r.Threshold <= 0 || r.Threshold >= 100 {
			return fmt.Errorf("rule %q: threshold must be 0-100 percent", r.Name)
		}
	case KindDisk:
		if r.MountPoint == "" {
			return fmt.Errorf("rule %q: mountpoint is required", r.Name)
		}
		if r.Threshold <= 0 || r.Threshold >= 100 {
			return fmt.Errorf("rule %q: threshold must be 0-100 percent", r.Name)
		}
	case KindUnit:
		if r.Unit == "" {
			return fmt.Errorf("rule %q: unit is required", r.Name)
		}
	case KindProbe:
		if r.Probe == "" {
			return fmt.Errorf("rule %q: probe is required", r.Name)
		}
	default:
		return fmt.Errorf("rule %q: unknown kind %q", r.Name, r.Kind)
	}
	return nil
}

// State is the alert state of a rule.
type State string

const (
	StateOK      State = "ok"
	StatePending State = "pending"
	StateFiring  State = "firing"
)

// Event is emitted when a rule transitions to firing or resolves.
type Event struct {
	Rule      string
	Kind      Kind
	State     State
	Message   string
	Timestamp time.Time
}

// Sink receives alert events.
type Sink interface {
	Send(Event) error
}

// Source provides the data rules are evaluated against. Satisfied by
// *agent.Agent.
type Source interface {
	Units() []unit.Unit
	SystemInfo() sysinfo.Snapshot
	Probes() []probe.ProbeStatus
}

// ruleState tracks evaluation of one rule.
type ruleState struct {
	rule       Rule
	state      State
	since      time.Time
	lastNotify time.Time
	firing     bool
} // Evaluator periodically evaluates rules against a Source and pushes
// transitions to sinks.
type Evaluator struct {
	src   Source
	sinks []Sink

	// Interval is how often rules are evaluated. Defaults to 15s.
	Interval time.Duration

	mu     sync.Mutex
	states map[string]*ruleState
	done   chan struct{}
}

// NewEvaluator creates an evaluator for the given rules. Invalid rules are
// rejected with an error.
func NewEvaluator(src Source, rules []Rule, sinks ...Sink) (*Evaluator, error) {
	states := make(map[string]*ruleState, len(rules))
	for _, r := range rules {
		if err := r.Validate(); err != nil {
			return nil, err
		}
		states[r.Name] = &ruleState{rule: r, state: StateOK}
	}
	return &Evaluator{
		src:      src,
		sinks:    sinks,
		Interval: 15 * time.Second,
		states:   states,
		done:     make(chan struct{}),
	}, nil
}

// Run evaluates rules on a ticker until the context is canceled.
func (e *Evaluator) Run(ctx context.Context) {
	t := time.NewTicker(e.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.done:
			return
		case <-t.C:
			e.EvaluateOnce(time.Now())
		}
	}
}

// Stop causes Run to return.
func (e *Evaluator) Stop() {
	select {
	case <-e.done:
	default:
		close(e.done)
	}
}

// EvaluateOnce runs a single evaluation pass, dispatches any resulting
// events to sinks, and returns them.
func (e *Evaluator) EvaluateOnce(now time.Time) []Event {
	events := e.evaluate(now)
	if len(events) > 0 {
		e.Dispatch(events)
	}
	return events
}

// evaluate runs the rule state machine under lock and returns transition
// events without dispatching.
func (e *Evaluator) evaluate(now time.Time) []Event {
	e.mu.Lock()
	defer e.mu.Unlock()

	var events []Event
	for _, st := range e.states {
		breached := e.breached(st.rule)

		switch {
		case breached && !st.firing:
			if st.state != StatePending {
				st.state = StatePending
				st.since = now
			}
			if now.Sub(st.since) >= st.rule.For {
				st.firing = true
				st.state = StateFiring
				st.lastNotify = now
				events = append(events, e.event(st, "firing"))
			}
		case !breached && st.firing:
			st.firing = false
			st.state = StateOK
			events = append(events, e.event(st, "resolved"))
		case !breached:
			st.state = StateOK
		}
	}
	return events
}

// breached reports whether the rule's condition currently holds.
func (e *Evaluator) breached(r Rule) bool {
	sys := e.src.SystemInfo()
	switch r.Kind {
	case KindCPU:
		return sys.CPU.User+sys.CPU.System >= r.Threshold
	case KindMemory:
		return sys.Memory.PercentUsed >= r.Threshold
	case KindDisk:
		for _, d := range sys.Disk {
			if d.MountPoint == r.MountPoint {
				return d.PercentUsed >= r.Threshold
			}
		}
		return false
	case KindUnit:
		for _, u := range e.src.Units() {
			if u.Name == r.Unit {
				return u.ActiveState == "failed"
			}
		}
		return false
	case KindProbe:
		for _, p := range e.src.Probes() {
			if p.Probe.Name == r.Probe {
				return p.Status == probe.StatusDown
			}
		}
		return false
	}
	return false
}

func (e *Evaluator) event(st *ruleState, action string) Event {
	msg := fmt.Sprintf("rule %s (%s) %s", st.rule.Name, st.rule.Kind, action)
	switch st.rule.Kind {
	case KindCPU:
		msg = fmt.Sprintf("cpu usage above %.0f%% for %s — %s", st.rule.Threshold, st.rule.For, action)
	case KindMemory:
		msg = fmt.Sprintf("memory usage above %.0f%% for %s — %s", st.rule.Threshold, st.rule.For, action)
	case KindDisk:
		msg = fmt.Sprintf("disk %s above %.0f%% for %s — %s", st.rule.MountPoint, st.rule.Threshold, st.rule.For, action)
	case KindUnit:
		msg = fmt.Sprintf("unit %s %s", st.rule.Unit, action)
	case KindProbe:
		msg = fmt.Sprintf("probe %s %s", st.rule.Probe, action)
	}
	return Event{
		Rule:      st.rule.Name,
		Kind:      st.rule.Kind,
		State:     st.state,
		Message:   msg,
		Timestamp: time.Now(),
	}
}

// dispatch sends events to all sinks, logging failures.
func (e *Evaluator) Dispatch(events []Event) {
	for _, ev := range events {
		for _, s := range e.sinks {
			if err := s.Send(ev); err != nil {
				slog.Error("alert sink", "error", err)
			}
		}
	}
}

// StateSnapshot is a read-only view of a rule's alert state.
type StateSnapshot struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	State     State     `json:"state"`
	Threshold float64   `json:"threshold"`
	Since     time.Time `json:"since"`
}

// States returns the current state of all rules in config order.
func (e *Evaluator) States() []StateSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()

	names := make([]string, 0, len(e.states))
	for name := range e.states {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]StateSnapshot, 0, len(names))
	for _, name := range names {
		st := e.states[name]
		out = append(out, StateSnapshot{
			Name:      st.rule.Name,
			Kind:      string(st.rule.Kind),
			State:     st.state,
			Threshold: st.rule.Threshold,
			Since:     st.since,
		})
	}
	return out
}
