package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ravi-arnan/tenax/log"
)

func testEntries() []log.Entry {
	now := time.Now()
	return []log.Entry{
		{Timestamp: now, Unit: "nginx.service", Message: "GET /index 200", Priority: 6},
		{Timestamp: now, Unit: "nginx.service", Message: "worker died", Priority: 3},
		{Timestamp: now, Unit: "sshd", Message: "accepted publickey", Priority: 6},
		{Timestamp: now, Unit: "kernel", Message: "EXT4-fs error", Priority: 2},
	}
}

func TestFilterEntriesAll(t *testing.T) {
	got := filterEntries(testEntries(), "", -1, "")
	if len(got) != 4 {
		t.Fatalf("expected 4, got %d", len(got))
	}
}

func TestFilterEntriesByUnit(t *testing.T) {
	got := filterEntries(testEntries(), "nginx.service", -1, "")
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	for _, e := range got {
		if e.Unit != "nginx.service" {
			t.Errorf("wrong unit leaked: %s", e.Unit)
		}
	}
}

func TestFilterEntriesByPriority(t *testing.T) {
	got := filterEntries(testEntries(), "", 3, "")
	if len(got) != 2 { // prio 3 and 2 only (6 is filtered out)
		t.Fatalf("expected 2 (prio<=3), got %d", len(got))
	}
	for _, e := range got {
		if e.Priority > 3 {
			t.Errorf("prio %d leaked through filter", e.Priority)
		}
	}
}

func TestFilterEntriesBySearch(t *testing.T) {
	got := filterEntries(testEntries(), "", -1, "ERROR")
	if len(got) != 1 || got[0].Unit != "kernel" {
		t.Fatalf("case-insensitive search failed: %+v", got)
	}

	// Search also matches unit names.
	got = filterEntries(testEntries(), "", -1, "ssh")
	if len(got) != 1 || got[0].Unit != "sshd" {
		t.Fatalf("unit-name search failed: %+v", got)
	}
}

func TestFilterEntriesCombined(t *testing.T) {
	got := filterEntries(testEntries(), "nginx.service", 3, "")
	if len(got) != 1 || got[0].Message != "worker died" {
		t.Fatalf("combined filter failed: %+v", got)
	}
}

func TestPriorityCycling(t *testing.T) {
	ls := &logScreen{}
	want := []int{-1, 3, 2, 1, 0, -1}
	for i, w := range want {
		if got := ls.currentPriority(); got != w {
			t.Fatalf("step %d: prio = %d, want %d", i, got, w)
		}
		ls.nextPriority()
	}
}

func TestUnitCycling(t *testing.T) {
	entries := testEntries()
	ls := &logScreen{}

	ls.nextUnit(entries) // all -> nginx.service
	if u := ls.currentUnit(); u != "nginx.service" {
		t.Fatalf("first cycle = %q, want nginx.service", u)
	}

	ls.nextUnit(entries) // -> sshd... but first-seen order: nginx, sshd, kernel
	if u := ls.currentUnit(); u != "sshd" {
		t.Fatalf("second cycle = %q, want sshd", u)
	}

	ls.nextUnit(entries) // -> kernel
	ls.nextUnit(entries) // -> all
	if u := ls.currentUnit(); u != "" {
		t.Fatalf("wrap-around = %q, want all", u)
	}
}

func TestSearchInput(t *testing.T) {
	ls := &logScreen{}
	ls.units = testEntries()

	// Typing activates search mode.
	ls.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !ls.searching {
		t.Fatal("expected search mode")
	}

	for _, r := range "died" {
		ls.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if ls.search != "died" {
		t.Fatalf("search = %q", ls.search)
	}

	// Enter applies: handleKey returns true (still open), search retained.
	if !ls.handleKey(tea.KeyMsg{Type: tea.KeyEnter}) {
		t.Fatal("enter should not close screen")
	}
	if ls.searching || ls.search != "died" {
		t.Fatalf("after enter: searching=%v search=%q", ls.searching, ls.search)
	}

	// Esc during typing cancels and clears.
	ls.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	ls.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	ls.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if ls.search != "" || ls.searching {
		t.Fatalf("esc should clear search, got %q", ls.search)
	}
}

func TestScreenClose(t *testing.T) {
	ls := &logScreen{}
	// handleKey returns false when the screen should close.
	if ls.handleKey(tea.KeyMsg{Type: tea.KeyEsc}) {
		t.Fatal("esc should close the screen")
	}
	if !ls.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")}) {
		t.Fatal("u should keep the screen open")
	}
}

func TestStatusLine(t *testing.T) {
	ls := &logScreen{}
	ls.units = testEntries()
	ls.filtered = filterEntries(ls.units, "", -1, "")

	line := ls.statusLine(100)
	for _, want := range []string{"unit: all", "prio: all", "4 entries"} {
		if !strings.Contains(line, want) {
			t.Errorf("status line missing %q: %s", want, line)
		}
	}

	// Search input mode shows a prompt.
	ls.searching = true
	ls.search = "er"
	line = ls.statusLine(100)
	if !strings.Contains(line, "search: er") {
		t.Errorf("search prompt missing: %s", line)
	}
}

func TestUniqueUnits(t *testing.T) {
	got := uniqueUnits(testEntries())
	want := []string{"nginx.service", "sshd", "kernel"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order: got %v, want %v", got, want)
		}
	}
}
