package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ravi-arnan/tenax/log"
)

// logPriorities cycles through syslog severity filters. Lower value =
// more severe. -1 shows everything.
var logPriorities = []int{-1, 3, 2, 1, 0}

var priorityNames = map[int]string{
	-1: "all",
	0:  "emerg",
	1:  "alert",
	2:  "crit",
	3:  "err",
	4:  "warning",
	5:  "notice",
	6:  "info",
	7:  "debug",
}

// logScreen holds the state of the full-screen log view.
type logScreen struct {
	viewport viewport.Model

	units    []log.Entry // latest snapshot from the agent
	filtered []log.Entry // after unit/prio/search filters

	unitIdx   int // index into unitNames; 0 = all
	unitNames []string
	prioIdx   int // index into logPriorities
	search    string
	searching bool // search input mode active
	follow    bool // stick to bottom as new entries arrive
	width     int
	height    int
}

func newLogScreen(width, height int) logScreen {
	ls := logScreen{
		viewport: viewport.New(width, height),
		follow:   true,
		prioIdx:  0,
	}
	ls.viewport.YPosition = 2 // below the status bar
	return ls
}

// nextUnit cycles the unit filter: all -> unit1 -> unit2 -> ... -> all.
func (ls *logScreen) nextUnit(entries []log.Entry) {
	names := uniqueUnits(entries)
	if len(names) == 0 {
		ls.unitIdx = 0
		ls.unitNames = nil
		return
	}
	ls.unitNames = names
	ls.unitIdx = (ls.unitIdx + 1) % (len(names) + 1) // +1 for "all"
}

// currentUnit returns the unit filter, "" for all.
func (ls *logScreen) currentUnit() string {
	if ls.unitIdx == 0 || ls.unitIdx > len(ls.unitNames) {
		return ""
	}
	return ls.unitNames[ls.unitIdx-1]
}

// nextPriority cycles the severity filter.
func (ls *logScreen) nextPriority() {
	ls.prioIdx = (ls.prioIdx + 1) % len(logPriorities)
}

// currentPriority returns the max priority shown; -1 for all.
func (ls *logScreen) currentPriority() int {
	return logPriorities[ls.prioIdx]
}

// filterEntries applies unit, priority, and search filters. Pure function:
// search is case-insensitive substring match on the message and unit name.
func filterEntries(entries []log.Entry, unit string, maxPrio int, search string) []log.Entry {
	out := make([]log.Entry, 0, len(entries))
	needle := strings.ToLower(search)
	for _, e := range entries {
		if unit != "" && e.Unit != unit {
			continue
		}
		if maxPrio >= 0 && e.Priority > maxPrio {
			continue
		}
		if needle != "" &&
			!strings.Contains(strings.ToLower(e.Message), needle) &&
			!strings.Contains(strings.ToLower(e.Unit), needle) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// refresh recomputes the filtered list and re-renders content. Keeps the
// view pinned to the bottom when follow is on.
func (ls *logScreen) refresh(width int) {
	ls.filtered = filterEntries(ls.units, ls.currentUnit(), ls.currentPriority(), ls.search)
	ls.viewport.Width = width
	ls.viewport.SetContent(ls.renderEntries(width))
	if ls.follow {
		ls.viewport.GotoBottom()
	}
}

func (ls *logScreen) renderEntries(width int) string {
	var b strings.Builder
	msgW := width - 40
	if msgW < 20 {
		msgW = 20
	}
	for _, e := range ls.filtered {
		ts := e.Timestamp.Format("15:04:05")
		unit := e.Unit
		if len(unit) > 24 {
			unit = unit[:24]
		}
		msg := e.Message
		if len(msg) > msgW {
			msg = msg[:msgW-3] + "..."
		}
		prio := priorityChar(e.Priority)
		b.WriteString(fmt.Sprintf(" %s %s %-25s%s %s\n", labelStyle(ts), prio, unit, msg, ""))
	}
	return b.String()
}

// statusLine renders the filter/search status bar.
func (ls *logScreen) statusLine(width int) string {
	if ls.searching {
		return helpStyle(fmt.Sprintf(" search: %s█  (enter: apply, esc: cancel)", ls.search))
	}

	parts := []string{
		fmt.Sprintf("unit: %s", orAll(ls.currentUnit())),
		fmt.Sprintf("prio: %s", priorityNames[ls.currentPriority()]),
	}
	if ls.search != "" {
		parts = append(parts, fmt.Sprintf("match: %q", ls.search))
	}
	parts = append(parts, fmt.Sprintf("%d entries", len(ls.filtered)))
	parts = append(parts, "u/prio// search · f follow · esc back")

	line := helpStyle(strings.Join(parts, " · "))
	return line
}

func orAll(s string) string {
	if s == "" {
		return "all"
	}
	return s
}

// priorityChar renders syslog severity as a colored marker.
func priorityChar(p int) string {
	switch {
	case p <= 2:
		return failedStyle("!")
	case p <= 4:
		return degradedStyle("~")
	default:
		return inactiveStyle("·")
	}
}

// uniqueUnits returns distinct unit names in first-seen order.
func uniqueUnits(entries []log.Entry) []string {
	seen := make(map[string]bool)
	var out []string
	for _, e := range entries {
		if e.Unit != "" && !seen[e.Unit] {
			seen[e.Unit] = true
			out = append(out, e.Unit)
		}
	}
	return out
}

// handleKey processes a key on the log screen. Returns false when the
// screen wants to close (esc/q).
func (ls *logScreen) handleKey(msg tea.KeyMsg) bool {
	key := msg.String()

	if ls.searching {
		switch key {
		case "enter":
			ls.searching = false
		case "esc":
			ls.search = ""
			ls.searching = false
		case "backspace":
			if len(ls.search) > 0 {
				ls.search = ls.search[:len(ls.search)-1]
			}
		case "ctrl+c":
			return false
		default:
			if len(key) == 1 {
				ls.search += key
			}
		}
		return true
	}

	switch key {
	case "esc", "q":
		return false
	case "/":
		ls.searching = true
		ls.search = ""
	case "u":
		ls.nextUnit(ls.units)
	case "p":
		ls.nextPriority()
	case "f":
		ls.follow = !ls.follow
	case "g":
		ls.viewport.GotoTop()
		ls.follow = false
	case "G":
		ls.follow = true
		ls.viewport.GotoBottom()
	case "up", "k":
		ls.viewport.LineUp(1)
		ls.follow = ls.viewport.AtBottom()
	case "down", "j":
		ls.viewport.LineDown(1)
		ls.follow = ls.viewport.AtBottom()
	case "pageup":
		ls.viewport.HalfViewUp()
		ls.follow = ls.viewport.AtBottom()
	case "pagedown":
		ls.viewport.HalfViewDown()
		ls.follow = ls.viewport.AtBottom()
	}
	return true
}

// updateSize re-sizes the viewport.
func (ls *logScreen) updateSize(width, height int) {
	ls.width = width
	ls.height = height
	ls.viewport.Width = width
	ls.viewport.Height = height - 2 // status bar top + bottom
	ls.viewport.YPosition = 1
}

var _ = time.Now
