package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ravi-arnan/tenax/log"
	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/sysinfo"
)

type Agent struct {
	mu        sync.RWMutex
	unitMgr   *unit.Manager
	logReader *log.JournalReader
	collector *sysinfo.Collector

	units   []unit.Unit
	sysSnap sysinfo.Snapshot
	logBuf  *log.RingBuffer

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func New() (*Agent, error) {
	um, err := unit.NewManager()
	if err != nil {
		return nil, fmt.Errorf("unit manager: %w", err)
	}

	return &Agent{
		unitMgr:   um,
		logReader: log.NewJournalReader(5000),
		collector: sysinfo.NewCollector(),
		logBuf:    log.NewRingBuffer(5000),
	}, nil
}

func (a *Agent) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	a.wg.Add(3)
	go a.pollUnits(ctx)
	go a.pollSysinfo(ctx)
	go a.pollLogs(ctx)

	return nil
}

func (a *Agent) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()
	a.unitMgr.Close()
	return nil
}

func (a *Agent) pollUnits(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(3 * time.Second)
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
			a.units = units
			a.mu.Unlock()
		}
	}
}

func (a *Agent) pollSysinfo(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := a.collector.Snapshot()
			a.mu.Lock()
			a.sysSnap = snap
			a.mu.Unlock()
		}
	}
}

func (a *Agent) pollLogs(ctx context.Context) {
	defer a.wg.Done()

	entries, err := a.logReader.Start(ctx, nil)
	if err != nil {
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

func (a *Agent) SystemInfo() sysinfo.Snapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sysSnap
}

func (a *Agent) LogEntries() []log.Entry {
	return a.logBuf.Entries()
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
