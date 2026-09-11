package log

import (
	"fmt"
	"time"
)

type Entry struct {
	Timestamp time.Time
	Unit      string
	Message   string
	Priority  int
	PID       int
	Source    string
}

type RingBuffer struct {
	entries []Entry
	cap     int
	pos     int
	full    bool
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		entries: make([]Entry, capacity),
		cap:     capacity,
	}
}

func (r *RingBuffer) Push(e Entry) {
	r.entries[r.pos] = e
	r.pos = (r.pos + 1) % r.cap
	if r.pos == 0 {
		r.full = true
	}
}

func (r *RingBuffer) Entries() []Entry {
	if !r.full {
		result := make([]Entry, r.pos)
		copy(result, r.entries[:r.pos])
		return result
	}
	result := make([]Entry, r.cap)
	copy(result, r.entries[r.pos:])
	copy(result[r.cap-r.pos:], r.entries[:r.pos])
	return result
}

func (r *RingBuffer) Len() int {
	if r.full {
		return r.cap
	}
	return r.pos
}

type JournalReader struct {
	buf *RingBuffer
}

func NewJournalReader(capacity int) *JournalReader {
	return &JournalReader{
		buf: NewRingBuffer(capacity),
	}
}

func (j *JournalReader) Start(units []string) (<-chan Entry, error) {
	ch := make(chan Entry, 100)

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			select {
			case ch <- Entry{
				Timestamp: time.Now(),
				Unit:      "tenax",
				Message:   fmt.Sprintf("tail placeholder for unit %v", units),
				Priority:  6,
				PID:       0,
				Source:    "systemd",
			}:
			default:
			}
		}
	}()

	return ch, nil
}

func (j *JournalReader) Entries() []Entry {
	return j.buf.Entries()
}
