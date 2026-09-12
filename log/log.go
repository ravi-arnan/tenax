package log

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
) // Entry is a single journald log line.
type Entry struct {
	Timestamp time.Time `json:"ts"`
	Unit      string    `json:"unit"`
	Message   string    `json:"message"`
	Priority  int       `json:"priority"` // 0-7 syslog
	PID       int       `json:"pid,omitempty"`
	Source    string    `json:"source,omitempty"` // journal transport: kernel, stdout, journal, audit
}

// RingBuffer is a fixed-size circular buffer of entries.
type RingBuffer struct {
	mu      sync.RWMutex
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[r.pos] = e
	r.pos = (r.pos + 1) % r.cap
	if r.pos == 0 {
		r.full = true
	}
}

func (r *RingBuffer) Entries() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.full {
		return r.cap
	}
	return r.pos
}

// JournalReader tails the systemd journal through the native sd-journal
// API (via cgo) and feeds parsed entries to consumers. Entries are also
// kept in an internal ring buffer.
type JournalReader struct {
	buf   *RingBuffer
	mu    sync.Mutex
	open  func() (*sdjournal.Journal, error)
	units []string
}

// NewJournalReader creates a reader with an internal ring buffer of the
// given capacity.
func NewJournalReader(capacity int) *JournalReader {
	return &JournalReader{
		buf:  NewRingBuffer(capacity),
		open: sdjournal.NewJournal,
	}
}

// Start opens the journal, seeks to the tail (only new entries are
// streamed, matching `journalctl -f` behavior), and returns a channel of
// entries. The channel closes when ctx is canceled.
func (j *JournalReader) Start(ctx context.Context, units []string) (<-chan Entry, error) {
	j.mu.Lock()
	j.units = units
	j.mu.Unlock()

	jrn, err := j.open()
	if err != nil {
		return nil, err
	}

	for _, u := range units {
		if err := jrn.AddMatch("_SYSTEMD_UNIT=" + u); err != nil {
			jrn.Close()
			return nil, err
		}
	}

	// Cap field size so huge log lines don't blow up memory.
	jrn.SetDataThreshold(256 * 1024)

	// Start at the tail: stream only entries that arrive from now on.
	if err := jrn.SeekTail(); err != nil {
		jrn.Close()
		return nil, err
	}

	ch := make(chan Entry, 100)

	go func() {
		defer close(ch)
		defer jrn.Close()

		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Wake on new entries; timeout keeps the loop responsive
			// to ctx cancellation.
			jrn.Wait(time.Second)

			for {
				n, err := jrn.Next()
				if err != nil {
					return
				}
				if n == 0 {
					break // caught up; go back to Wait
				}

				je, err := jrn.GetEntry()
				if err != nil {
					continue
				}

				entry := entryToLogEntry(je)
				j.buf.Push(entry)

				select {
				case ch <- entry:
				default: // drop if consumer is slow; ring buffer keeps it
				}
			}
		}
	}()

	return ch, nil
}

// Entries returns the buffered entries.
func (j *JournalReader) Entries() []Entry {
	return j.buf.Entries()
}

// entryToLogEntry maps a raw journal entry to our log Entry. Exposed for
// tests via a plain function on exported types.
func entryToLogEntry(je *sdjournal.JournalEntry) Entry {
	ts := time.UnixMicro(int64(je.RealtimeTimestamp))

	unit := je.Fields["_SYSTEMD_UNIT"]
	if unit == "" {
		unit = je.Fields["SYSLOG_IDENTIFIER"]
	}
	if unit == "" {
		unit = je.Fields["_TRANSPORT"]
	}

	priority := 6
	if p, err := strconv.Atoi(je.Fields["PRIORITY"]); err == nil && p >= 0 && p <= 7 {
		priority = p
	}

	pid := 0
	if s := je.Fields["_PID"]; s != "" {
		if p, err := strconv.Atoi(s); err == nil {
			pid = p
		}
	} else if s := je.Fields["SYSLOG_PID"]; s != "" {
		if p, err := strconv.Atoi(s); err == nil {
			pid = p
		}
	}

	source := je.Fields["_TRANSPORT"]
	if source == "" {
		source = "journal"
	}

	// Strip field prefixes if GetEntry returned raw "K=v" strings.
	msg := je.Fields["MESSAGE"]
	if i := strings.Index(msg, "MESSAGE="); i == 0 {
		msg = strings.TrimPrefix(msg, "MESSAGE=")
	}

	return Entry{
		Timestamp: ts,
		Unit:      unit,
		Message:   msg,
		Priority:  priority,
		PID:       pid,
		Source:    source,
	}
}
