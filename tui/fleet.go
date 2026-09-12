package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const fleetTickInterval = 5 * time.Second

// FleetHost is the TUI's view of one controller-registered host. Decoupled
// from the controller package so the TUI stays testable without gRPC.
type FleetHost struct {
	ID          string
	Status      string // healthy, degraded, disconnected, stale
	Tags        []string
	CPUPercent  float64
	MemPercent  float64
	UnitsTotal  int
	UnitsActive int
	UnitsFailed int
	LastSeenAgo string
}

// FleetData is a snapshot of the whole fleet.
type FleetData struct {
	Hosts []FleetHost
	Group string // active tag filter, "" = all
}

// Tags returns distinct tags across hosts, sorted.
func (f FleetData) Tags() []string {
	seen := make(map[string]bool)
	for _, h := range f.Hosts {
		for _, t := range h.Tags {
			seen[t] = true
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Filtered returns hosts matching the active group filter.
func (f FleetData) Filtered() []FleetHost {
	if f.Group == "" {
		return f.Hosts
	}
	out := make([]FleetHost, 0, len(f.Hosts))
	for _, h := range f.Hosts {
		for _, t := range h.Tags {
			if t == f.Group {
				out = append(out, h)
				break
			}
		}
	}
	return out
}

// fleetStatusStyle colors a fleet status string.
func fleetStatusStyle(status string) string {
	switch status {
	case "healthy":
		return activeStyle("● " + status)
	case "degraded":
		return degradedStyle("● " + status)
	default: // stale, disconnected
		return failedStyle("● " + status)
	}
}

// RenderFleet renders the fleet view as a string. Pure function over
// FleetData so it can be tested without a terminal.
func RenderFleet(f FleetData, width int) string {
	var b strings.Builder

	hosts := f.Filtered()

	// Header: title + group filter.
	title := titleStyle("Fleet")
	if f.Group != "" {
		b.WriteString(fmt.Sprintf(" %s  group %s · %d hosts\n\n", title, valueStyle(f.Group), len(hosts)))
	} else {
		b.WriteString(fmt.Sprintf(" %s  %d hosts\n\n", title, len(hosts)))
	}

	if len(hosts) == 0 {
		b.WriteString(inactiveStyle(" no hosts match") + "\n")
		return b.String()
	}

	// Group hosts by their sorted tag set for stable sections.
	type groupKey struct{ tags string }
	byGroup := make(map[groupKey][]FleetHost)
	var keys []groupKey
	for _, h := range hosts {
		tags := append([]string(nil), h.Tags...)
		sort.Strings(tags)
		k := groupKey{tags: strings.Join(tags, ",")}
		if _, ok := byGroup[k]; !ok {
			keys = append(keys, k)
		}
		byGroup[k] = append(byGroup[k], h)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].tags < keys[j].tags })

	for _, k := range keys {
		g := byGroup[k]
		label := k.tags
		if label == "" {
			label = "untagged"
		}
		b.WriteString(labelStyle(" ["+label+"]") + "\n")
		for _, h := range g {
			name := h.ID
			if len(name) > 24 {
				name = name[:24]
			}
			cpu := fmt.Sprintf("cpu %5.1f%%", h.CPUPercent)
			mem := fmt.Sprintf("mem %5.1f%%", h.MemPercent)
			units := fmt.Sprintf("units %d/%d failed=%d", h.UnitsActive, h.UnitsTotal, h.UnitsFailed)
			line := fmt.Sprintf("   %-25s %-22s %s %s %s  %s ago",
				name, fleetStatusStyle(h.Status), cpu, mem, units, h.LastSeenAgo)
			if len(line) > width-2 && width > 30 {
				line = line[:width-5] + "..."
			}
			b.WriteString(line + "\n")
		}
	}

	return b.String()
}

// FleetModel is the bubbletea model for the controller fleet dashboard.
type FleetModel struct {
	fetch func() FleetData // current fleet snapshot
	data  FleetData
	width int
	ready bool
}

// NewFleetModel creates a fleet model with a fetch callback.
func NewFleetModel(fetch func() FleetData) FleetModel {
	return FleetModel{fetch: fetch}
}

type fleetTickMsg time.Time

// FleetTick is the refresh cadence for the fleet view.
func FleetTick() tea.Cmd {
	return tea.Tick(fleetTickInterval, func(t time.Time) tea.Msg {
		return fleetTickMsg(t)
	})
}

func (m FleetModel) Init() tea.Cmd {
	return FleetTick()
}

// Update implements tea.Model.
func (m FleetModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m, cmd = m.update(msg)
	return m, cmd
}

func (m FleetModel) update(msg tea.Msg) (FleetModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.ready = true
	case fleetTickMsg:
		if m.fetch != nil {
			m.data = m.fetch()
		}
		return m, FleetTick()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			// Cycle group filter through all -> each tag.
			tags := m.data.Tags()
			idx := -1
			for i, t := range tags {
				if t == m.data.Group {
					idx = i
					break
				}
			}
			if idx+1 < len(tags) {
				m.data.Group = tags[idx+1]
			} else {
				m.data.Group = ""
			}
		}
	}
	return m, nil
}

func (m FleetModel) View() string {
	if !m.ready {
		return "loading fleet..."
	}
	var b strings.Builder
	b.WriteString(RenderFleet(m.data, m.width))
	b.WriteString(helpStyle("  tab group · q quit"))
	return b.String()
}
