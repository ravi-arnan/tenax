// Package probe implements the probe engine: it schedules health checks,
// tracks per-probe status, and exposes a snapshot for dashboards.
package probe

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ravi-arnan/tenax/pkg/health"
)

// Status is the derived health of a probe based on recent results.
type Status string

const (
	StatusUnknown  Status = "unknown"
	StatusHealthy  Status = "healthy"
	StatusDegraded Status = "degraded"
	StatusDown     Status = "down"
)

// ProbeStatus summarizes the current state of a single probe.
type ProbeStatus struct {
	Probe       health.Probe
	Status      Status
	LastResult  health.Result
	ConsecFails int
	LastRun     time.Time
	NextRun     time.Time
}

// Runner schedules probes and tracks their status.
type Runner struct {
	checkers map[health.ProbeType]health.Checker

	mu      sync.RWMutex
	probes  map[string]*probeState
	order   []string
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	started bool
}

type probeState struct {
	def     health.Probe
	status  Status
	last    health.Result
	fails   int
	lastRun time.Time
}

// NewRunner creates a Runner for the given probe definitions. Definitions
// are validated and defaulted; invalid probes are returned as an error
// listing all problems.
func NewRunner(defs []health.Probe) (*Runner, error) {
	r := &Runner{
		checkers: make(map[health.ProbeType]health.Checker),
		probes:   make(map[string]*probeState),
	}

	var errs []error
	seen := make(map[string]bool)
	for _, d := range defs {
		d = d.WithDefaults()
		if err := d.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[d.Name] {
			errs = append(errs, fmt.Errorf("duplicate probe name %q", d.Name))
			continue
		}
		seen[d.Name] = true

		if _, ok := r.checkers[d.Type]; !ok {
			c, err := health.CheckerFor(d.Type)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			r.checkers[d.Type] = c
		}

		r.probes[d.Name] = &probeState{def: d, status: StatusUnknown}
		r.order = append(r.order, d.Name)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid probe config: %v", errs)
	}
	return r, nil
}

// Start launches one goroutine per probe. Each runs its check on the probe's
// interval; retries happen inline on failure.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	ctx, r.cancel = context.WithCancel(ctx)
	r.mu.Unlock()

	for _, name := range r.order {
		r.wg.Add(1)
		go r.runProbe(ctx, name)
	}
}

// Stop cancels all probe loops and waits for them to finish.
func (r *Runner) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
}

// Snapshot returns the current status of all probes in config order.
func (r *Runner) Snapshot() []ProbeStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ProbeStatus, 0, len(r.order))
	for _, name := range r.order {
		ps := r.probes[name]
		out = append(out, ProbeStatus{
			Probe:       ps.def,
			Status:      ps.status,
			LastResult:  ps.last,
			ConsecFails: ps.fails,
			LastRun:     ps.lastRun,
		})
	}
	return out
}

// Get returns the status of a single probe by name.
func (r *Runner) Get(name string) (ProbeStatus, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ps, ok := r.probes[name]
	if !ok {
		return ProbeStatus{}, false
	}
	return ProbeStatus{
		Probe:       ps.def,
		Status:      ps.status,
		LastResult:  ps.last,
		ConsecFails: ps.fails,
		LastRun:     ps.lastRun,
	}, true
}

// Healthy reports whether every probe is currently healthy.
func (r *Runner) Healthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ps := range r.probes {
		if ps.status == StatusDown || ps.status == StatusDegraded {
			return false
		}
	}
	return true
}

func (r *Runner) runProbe(ctx context.Context, name string) {
	defer r.wg.Done()

	r.mu.RLock()
	ps := r.probes[name]
	def := ps.def
	r.mu.RUnlock()

	checker := r.checkers[def.Type]
	if checker == nil {
		return
	}

	// Run the first check immediately, then on the interval.
	t := time.NewTimer(0)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		r.checkOnce(ctx, checker, name)

		r.mu.RLock()
		interval := r.probes[name].def.Interval
		r.mu.RUnlock()

		t.Reset(interval)
	}
}

func (r *Runner) checkOnce(ctx context.Context, checker health.Checker, name string) {
	r.mu.RLock()
	def := r.probes[name].def
	r.mu.RUnlock()

	res := r.runWithRetries(ctx, checker, def)

	r.mu.Lock()
	ps := r.probes[name]
	ps.last = res
	ps.lastRun = res.Timestamp

	status := StatusHealthy
	if res.Success {
		ps.fails = 0
	} else {
		ps.fails++
		// Retries are extra attempts beyond the first; a probe is only
		// down once it has failed more than retries allows.
		if ps.fails > def.Retries {
			status = StatusDown
		} else {
			status = StatusDegraded
		}
	}
	ps.status = status
	r.mu.Unlock()
}

func (r *Runner) runWithRetries(ctx context.Context, checker health.Checker, def health.Probe) health.Result {
	res := checker.Check(ctx, def)
	attempts := 1 + max(0, def.Retries)
	attempt := 1
	for !res.Success && attempt < attempts {
		attempt++
		res = checker.Check(ctx, def)
	}
	return res
}
