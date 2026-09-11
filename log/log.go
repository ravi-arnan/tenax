package log

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
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

type JournalReader struct {
	buf *RingBuffer
	cmd *exec.Cmd
	mu  sync.Mutex
}

func NewJournalReader(capacity int) *JournalReader {
	return &JournalReader{
		buf: NewRingBuffer(capacity),
	}
}

func (j *JournalReader) Start(ctx context.Context, units []string) (<-chan Entry, error) {
	ch := make(chan Entry, 100)

	args := []string{"-f", "-o", "short-iso-precise", "--no-hostname"}
	for _, u := range units {
		args = append(args, "-u", u)
	}

	cmd := exec.CommandContext(ctx, "journalctl", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		close(ch)
		return ch, fmt.Errorf("journalctl pipe: %w", err)
	}

	j.mu.Lock()
	j.cmd = cmd
	j.mu.Unlock()

	if err := cmd.Start(); err != nil {
		close(ch)
		return ch, fmt.Errorf("journalctl start: %w", err)
	}

	go func() {
		defer close(ch)

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 64*1024), 256*1024)

		for scanner.Scan() {
			line := scanner.Text()
			entry := parseJournalLine(line)
			if entry != nil {
				j.buf.Push(*entry)
				select {
				case ch <- *entry:
				default:
				}
			}
		}

		j.mu.Lock()
		j.cmd = nil
		j.mu.Unlock()

		cmd.Wait()
	}()

	return ch, nil
}

func (j *JournalReader) Stop() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.cmd != nil && j.cmd.Process != nil {
		j.cmd.Process.Kill()
	}
}

func (j *JournalReader) Entries() []Entry {
	return j.buf.Entries()
}

func parseJournalLine(line string) *Entry {
	idx := strings.Index(line, " ")
	if idx < 20 {
		return nil
	}

	ts := line[:idx]
	rest := line[idx+1:]

	ts = strings.TrimRight(ts, ",")

	t, err := time.Parse("2006-01-02T15:04:05.999999", ts)
	if err != nil {
		t = time.Now()
	}

	hostIdx := strings.Index(rest, " ")
	if hostIdx < 0 {
		return nil
	}
	rest = rest[hostIdx+1:]

	pidEnd := strings.Index(rest, "]:")
	if pidEnd < 0 {
		pidEnd = strings.Index(rest, ":")
		if pidEnd < 0 {
			return nil
		}
	}

	prefix := rest[:pidEnd]
	msg := strings.TrimSpace(rest[pidEnd+2:])

	pid := 0
	unit := "unknown"

	if pi := strings.LastIndex(prefix, "["); pi >= 0 {
		pidStr := prefix[pi+1:]
		pidStr = strings.TrimRight(pidStr, "]")
		if p, err := strconv.Atoi(pidStr); err == nil {
			pid = p
		}
		unit = strings.TrimSpace(prefix[:pi])
	} else {
		unit = strings.TrimSpace(prefix)
	}

	return &Entry{
		Timestamp: t,
		Unit:      unit,
		Message:   msg,
		Priority:  6,
		PID:       pid,
		Source:    "systemd",
	}
}
