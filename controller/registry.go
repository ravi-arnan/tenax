// Package controller implements the tenax multi-host controller: it
// accepts agent streams over gRPC, keeps a registry of hosts, and exposes
// the fleet over REST and a small HTML page.
package controller

import (
	"sync"
	"time"

	tenaxv1 "github.com/ravi-arnan/tenax/proto/tenaxv1"
) // Host is the controller-side view of one agent host.
type Host struct {
	ID         string // hostname (unique per fleet)
	Version    string
	OS         string
	Kernel     string
	Tags       []string
	Status     *tenaxv1.HostStatus
	Units      []UnitView
	Latest     *ResourceView
	Probes     []ProbeView
	FirstSeen  time.Time
	LastSeen   time.Time
	StreamOpen bool
}

// HasTag reports whether the host carries the given tag.
func (h Host) HasTag(tag string) bool {
	for _, t := range h.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// JSON-friendly views (proto types don't marshal to nice JSON).
type UnitView struct {
	Name        string `json:"name"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	LoadState   string `json:"load_state"`
}

type ResourceView struct {
	CPUPercent float64   `json:"cpu_percent"`
	MemPercent float64   `json:"mem_percent"`
	Load1      float64   `json:"load1"`
	Time       time.Time `json:"time"`
}

type ProbeView struct {
	Name             string `json:"name"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	ConsecutiveFails int32  `json:"consecutive_failures"`
}

// Registry tracks hosts by ID with a liveness deadline.
type Registry struct {
	mu       sync.RWMutex
	hosts    map[string]*Host
	deadline time.Duration // liveness window
	now      func() time.Time
}

// NewRegistry creates a registry where hosts go stale after deadline
// without an update.
func NewRegistry(deadline time.Duration) *Registry {
	return &Registry{
		hosts:    make(map[string]*Host),
		deadline: deadline,
		now:      time.Now,
	}
}

// SetNow overrides the clock (tests).
func (r *Registry) SetNow(f func() time.Time) {
	r.now = f
}

// Deadline returns the liveness window.
func (r *Registry) Deadline() time.Duration {
	return r.deadline
}

// Upsert records a snapshot from a host. streamOpen marks the gRPC stream
// state; heartbeats keep LastSeen fresh even without state changes.
func (r *Registry) Upsert(snap *tenaxv1.Snapshot, streamOpen bool) *Host {
	id := snap.GetHostname()
	if id == "" {
		id = "unknown"
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	h, ok := r.hosts[id]
	if !ok {
		h = &Host{ID: id, FirstSeen: r.now()}
		r.hosts[id] = h
	}

	h.Version = snap.GetAgentVersion()
	h.OS = snap.GetOs()
	h.Kernel = snap.GetKernel()
	h.Tags = append([]string(nil), snap.GetTags()...)
	h.Status = snap.GetStatus()
	h.LastSeen = r.now()
	h.StreamOpen = streamOpen

	units := snap.GetUnits()
	h.Units = make([]UnitView, 0, len(units))
	for _, u := range units {
		h.Units = append(h.Units, UnitView{
			Name:        u.GetName(),
			ActiveState: u.GetActiveState(),
			SubState:    u.GetSubState(),
			LoadState:   u.GetLoadState(),
		})
	}

	if s := snap.GetLatest(); s != nil {
		h.Latest = &ResourceView{
			CPUPercent: s.GetCpuPercent(),
			MemPercent: s.GetMemPercent(),
			Load1:      s.GetLoad1(),
			Time:       s.GetTime().AsTime(),
		}
	}

	probes := snap.GetProbes()
	h.Probes = make([]ProbeView, 0, len(probes))
	for _, p := range probes {
		h.Probes = append(h.Probes, ProbeView{
			Name:             p.GetName(),
			Type:             p.GetType(),
			Status:           p.GetStatus(),
			ConsecutiveFails: p.GetConsecutiveFailures(),
		})
	}

	return h
}

// MarkClosed flags the host's stream as closed without removing it. The
// host stays visible until the liveness deadline passes.
func (r *Registry) MarkClosed(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.hosts[id]; ok {
		h.StreamOpen = false
		h.LastSeen = r.now()
	}
}

// Host returns a copy of a single host by ID.
func (r *Registry) Host(id string) (Host, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.hosts[id]
	if !ok {
		return Host{}, false
	}
	return *h, true
}

// Alive reports whether the host is within the liveness deadline.
func (h Host) Alive(now time.Time, deadline time.Duration) bool {
	return now.Sub(h.LastSeen) <= deadline
}

// Status derives a coarse fleet status for display.
func (h Host) StatusNow(now time.Time, deadline time.Duration) string {
	if !h.Alive(now, deadline) {
		return "stale"
	}
	if !h.StreamOpen {
		return "disconnected"
	}
	if h.Status.GetUnitsFailed() > 0 {
		return "degraded"
	}
	return "healthy"
}

// List returns all hosts sorted by ID.
func (r *Registry) List() []Host {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Host, 0, len(r.hosts))
	for _, h := range r.hosts {
		out = append(out, *h)
	}
	// Simple insertion sort by ID (fleet sizes are small).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].ID < out[j-1].ID; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Count returns the number of registered hosts.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.hosts)
}
