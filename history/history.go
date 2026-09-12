// Package history stores recent operational history: systemd unit state
// transitions and system resource samples, both as fixed-size rings.
package history

import (
	"sync"
	"time"
)

// Event is a unit state transition.
type Event struct {
	Unit string    `json:"unit"`
	From string    `json:"from_state"`
	To   string    `json:"to_state"`
	Time time.Time `json:"time"`
}

// Sample is one point-in-time resource measurement.
type Sample struct {
	Time   time.Time `json:"time"`
	CPUPct float64   `json:"cpu_percent"`
	MemPct float64   `json:"mem_percent"`
	Load1  float64   `json:"load1"`
}

// ring is a generic fixed-size circular buffer.
type ring[T any] struct {
	mu   sync.RWMutex
	buf  []T
	cap  int
	pos  int
	full bool
}

func newRing[T any](capacity int) *ring[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &ring[T]{buf: make([]T, capacity), cap: capacity}
}

func (r *ring[T]) push(v T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.pos] = v
	r.pos = (r.pos + 1) % r.cap
	if r.pos == 0 {
		r.full = true
	}
}

func (r *ring[T]) snapshot() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]T, 0, r.len())
	if !r.full {
		out = append(out, r.buf[:r.pos]...)
		return out
	}
	out = append(out, r.buf[r.pos:]...)
	out = append(out, r.buf[:r.pos]...)
	return out
}

func (r *ring[T]) len() int {
	if r.full {
		return r.cap
	}
	return r.pos
}

// Store keeps unit events and resource samples.
type Store struct {
	events  *ring[Event]
	samples *ring[Sample]
}

// NewStore creates a store with the given capacities.
func NewStore(eventCap, sampleCap int) *Store {
	return &Store{
		events:  newRing[Event](eventCap),
		samples: newRing[Sample](sampleCap),
	}
}

// PushEvent records a unit state transition.
func (s *Store) PushEvent(e Event) { s.events.push(e) }

// Events returns a copy of recorded events, oldest first.
func (s *Store) Events() []Event { return s.events.snapshot() }

// PushSample records a resource sample.
func (s *Store) PushSample(sp Sample) { s.samples.push(sp) }

// Samples returns a copy of recorded samples, oldest first.
func (s *Store) Samples() []Sample { return s.samples.snapshot() }
