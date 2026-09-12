package log

import (
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-systemd/v22/sdjournal"
)

func TestEntryToLogEntry(t *testing.T) {
	ts := uint64(1700000000_000000) // usec

	je := &sdjournal.JournalEntry{
		RealtimeTimestamp: ts,
		Fields: map[string]string{
			"MESSAGE":       "hello world",
			"PRIORITY":      "3",
			"_PID":          "1234",
			"_SYSTEMD_UNIT": "nginx.service",
			"_TRANSPORT":    "journal",
		},
	}

	e := entryToLogEntry(je)

	wantTS := time.UnixMicro(int64(ts))
	if !e.Timestamp.Equal(wantTS) {
		t.Errorf("timestamp = %v, want %v", e.Timestamp, wantTS)
	}
	if e.Unit != "nginx.service" {
		t.Errorf("unit = %q, want nginx.service", e.Unit)
	}
	if e.Message != "hello world" {
		t.Errorf("message = %q", e.Message)
	}
	if e.Priority != 3 {
		t.Errorf("priority = %d, want 3", e.Priority)
	}
	if e.PID != 1234 {
		t.Errorf("pid = %d, want 1234", e.PID)
	}
	if e.Source != "journal" {
		t.Errorf("source = %q", e.Source)
	}
}

func TestEntryToLogEntryFallbacks(t *testing.T) {
	// No unit: falls back to syslog identifier, then transport.
	je := &sdjournal.JournalEntry{
		Fields: map[string]string{
			"MESSAGE":           "kernel line",
			"SYSLOG_IDENTIFIER": "sshd",
			"SYSLOG_PID":        "99",
		},
	}
	e := entryToLogEntry(je)
	if e.Unit != "sshd" {
		t.Errorf("unit fallback = %q, want sshd", e.Unit)
	}
	if e.PID != 99 {
		t.Errorf("pid fallback = %d, want 99", e.PID)
	}
	if e.Priority != 6 {
		t.Errorf("default priority = %d, want 6", e.Priority)
	}

	// Invalid priority and pid are ignored.
	je2 := &sdjournal.JournalEntry{
		Fields: map[string]string{
			"MESSAGE":  "x",
			"PRIORITY": "bogus",
			"_PID":     "bogus",
		},
	}
	e2 := entryToLogEntry(je2)
	if e2.Priority != 6 || e2.PID != 0 {
		t.Errorf("invalid fields not defaulted: %+v", e2)
	}
}

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(3)

	for i := 0; i < 5; i++ {
		rb.Push(Entry{Message: string(rune('a' + i))})
	}

	if got := rb.Len(); got != 3 {
		t.Fatalf("len = %d, want 3", got)
	}

	entries := rb.Entries()
	want := "cde"
	got := ""
	for _, e := range entries {
		got += e.Message
	}
	if got != want {
		t.Fatalf("entries = %q, want %q", got, want)
	}
}

func TestRingBufferEmpty(t *testing.T) {
	rb := NewRingBuffer(4)
	if rb.Len() != 0 {
		t.Fatal("expected empty buffer")
	}
	if entries := rb.Entries(); len(entries) != 0 {
		t.Fatal("expected no entries")
	}
}

func TestRingBufferConcurrent(t *testing.T) {
	rb := NewRingBuffer(64)

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				rb.Push(Entry{Message: "x"})
				rb.Entries()
				rb.Len()
			}
		}()
	}
	wg.Wait()
}
