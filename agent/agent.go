package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ravi-arnan/tenax/alert"
	"github.com/ravi-arnan/tenax/config"
	"github.com/ravi-arnan/tenax/history"
	"github.com/ravi-arnan/tenax/log"
	"github.com/ravi-arnan/tenax/pkg/health"
	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/probe"
	"github.com/ravi-arnan/tenax/sysinfo"
)

// Agent is the core engine: it polls systemd units and sysinfo, tails the
// journal, runs probes, evaluates alert rules, and records history. It can
// optionally stream its state to a controller (multi-host).
type Agent struct {
	cfg       *config.Config
	unitMgr   *unit.Manager
	logReader *log.JournalReader
	collector *sysinfo.Collector
	probes    *probe.Runner
	evaluator *alert.Evaluator
	history   *history.Store
	uplink    *Uplink

	mu      sync.RWMutex
	units   []unit.Unit
	sysSnap sysinfo.Snapshot
	logBuf  *log.RingBuffer

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates an Agent from the given config. A nil config means "load
// defaults". Subsystems that fail to load (probes, alerts) log a warning
// and start disabled instead of failing the daemon.
func New(cfg *config.Config) (*Agent, error) {
	if cfg == nil {
		c, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("config: %w", err)
		}
		cfg = c
	}

	um, err := unit.NewManager()
	if err != nil {
		return nil, fmt.Errorf("unit manager: %w", err)
	}

	probes, err := loadProbes(cfg)
	if err != nil {
		slog.Warn("probe config; probes disabled", "error", err)
	}

	a := &Agent{
		cfg:       cfg,
		unitMgr:   um,
		logReader: log.NewJournalReader(5000),
		collector: sysinfo.NewCollector(),
		logBuf:    log.NewRingBuffer(5000),
		probes:    probes,
		history:   history.NewStore(cfg.Agent.UnitHistorySize, cfg.Agent.ResourceHistorySize),
	}

	evaluator, err := loadAlerts(a)
	if err != nil {
		slog.Warn("alert config; alerting disabled", "error", err)
	}
	a.evaluator = evaluator

	if cfg.Controller.Endpoint != "" {
		a.uplink = NewUplink(a, cfg.Controller.Endpoint)
		slog.Info("controller uplink configured", "endpoint", cfg.Controller.Endpoint)
	}

	return a, nil
}

// loadProbes builds a probe runner from the configured file (or default
// locations). A nil runner means no probes are configured.
func loadProbes(cfg *config.Config) (*probe.Runner, error) {
	var (
		defs []health.Probe
		err  error
	)
	if cfg.Probes.File != "" {
		defs, err = probe.LoadFile(cfg.Probes.File)
	} else {
		defs, err = probe.LoadDefault()
	}
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		return nil, nil
	}
	return probe.NewRunner(defs)
}

// loadAlerts builds the alert evaluator from the configured file (or
// default locations). A nil evaluator means no alert rules are configured.
func loadAlerts(src alert.Source) (*alert.Evaluator, error) {
	rules, sinks, err := alert.LoadDefault()
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, nil
	}
	return alert.NewEvaluator(src, rules, sinks...)
}

func (a *Agent) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	a.wg.Add(3)
	go a.pollUnits(ctx)
	go a.pollSysinfo(ctx)
	go a.pollLogs(ctx)

	if a.probes != nil {
		a.probes.Start(ctx)
	}

	if a.evaluator != nil {
		a.wg.Add(1)
		go a.evaluator.Run(ctx)
	}

	if a.uplink != nil {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.uplink.Run(ctx)
		}()
	}

	return nil
}

func (a *Agent) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()
	if a.probes != nil {
		a.probes.Stop()
	}
	a.unitMgr.Close()
	return nil
}

func (a *Agent) pollUnits(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(time.Duration(a.cfg.Agent.UnitsIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			units, err := a.unitMgr.ListUnits()
			if err != nil {
				continue
			}

			a.mu.Lock()
			prev := a.units
			a.units = units
			a.mu.Unlock()

			a.recordTransitions(prev, units, time.Now())
		}
	}
}

// recordTransitions diffs old and new unit lists, pushing state changes
// into history. Exposed for tests.
func (a *Agent) recordTransitions(prev, curr []unit.Unit, now time.Time) {
	prevState := make(map[string]string, len(prev))
	for _, u := range prev {
		prevState[u.Name] = u.ActiveState
	}

	var events []history.Event
	for _, u := range curr {
		from, ok := prevState[u.Name]
		if !ok || from == u.ActiveState {
			continue
		}
		events = append(events, history.Event{
			Unit: u.Name,
			From: from,
			To:   u.ActiveState,
			Time: now,
		})
	}

	for _, e := range events {
		a.history.PushEvent(e)
		slog.Info("unit state change", "unit", e.Unit, "from", e.From, "to", e.To)
	}
}

func (a *Agent) pollSysinfo(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(time.Duration(a.cfg.Agent.SysinfoIntervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := a.collector.Snapshot()
			load1, _, _, _ := sysinfo.LoadAvg()

			a.mu.Lock()
			a.sysSnap = snap
			a.mu.Unlock()

			a.history.PushSample(history.Sample{
				Time:   snap.Time,
				CPUPct: snap.CPU.User + snap.CPU.System,
				MemPct: snap.Memory.PercentUsed,
				Load1:  load1,
			})
		}
	}
}

func (a *Agent) pollLogs(ctx context.Context) {
	defer a.wg.Done()

	entries, err := a.logReader.Start(ctx, nil)
	if err != nil {
		slog.Warn("journal reader unavailable", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case e := <-entries:
			a.logBuf.Push(e)
		}
	}
}

func (a *Agent) Units() []unit.Unit {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]unit.Unit, len(a.units))
	copy(result, a.units)
	return result
}

// Unit returns a single unit by name from the last poll.
func (a *Agent) Unit(name string) (unit.Unit, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, u := range a.units {
		if u.Name == name {
			return u, true
		}
	}
	return unit.Unit{}, false
}

func (a *Agent) SystemInfo() sysinfo.Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sysSnap
}

func (a *Agent) LogEntries() []log.Entry {
	return a.logBuf.Entries()
}

// Probes returns the current status of all configured probes. It returns
// nil when no probes are configured.
func (a *Agent) Probes() []probe.ProbeStatus {
	if a.probes == nil {
		return nil
	}
	return a.probes.Snapshot()
}

// AlertStates returns the current state of all alert rules. It returns
// nil when no rules are configured.
func (a *Agent) AlertStates() []alert.StateSnapshot {
	if a.evaluator == nil {
		return nil
	}
	return a.evaluator.States()
}

// UnitEvents returns recent unit state transitions, oldest first.
func (a *Agent) UnitEvents() []history.Event {
	return a.history.Events()
}

// ResourceSamples returns recent resource samples, oldest first.
func (a *Agent) ResourceSamples() []history.Sample {
	return a.history.Samples()
}

func (a *Agent) StartUnit(name string) (int, error) {
	return a.unitMgr.StartUnit(name)
}

func (a *Agent) StopUnit(name string) (int, error) {
	return a.unitMgr.StopUnit(name)
}

func (a *Agent) RestartUnit(name string) (int, error) {
	return a.unitMgr.RestartUnit(name)
}
