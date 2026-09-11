package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ravi-arnan/tenax/agent"
	"github.com/ravi-arnan/tenax/log"
	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/sysinfo"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("33")).
			Padding(0, 1).
			Render

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")).
			Padding(0, 1).
			Render

	failedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true).
			Render

	activeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Render

	inactiveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Render

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("248")).
			Render

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Render

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render
)

type tickMsg time.Time

type Dashboard struct {
	agt      *agent.Agent
	units    []unit.Unit
	sysSnap  sysinfo.Snapshot
	logLines []log.Entry
	ready    bool

	unitViewport viewport.Model
	logViewport  viewport.Model
	width        int
	height       int
}

func NewDashboard(agt *agent.Agent) *Dashboard {
	return &Dashboard{agt: agt}
}

func (d *Dashboard) Init() tea.Cmd {
	return tea.Batch(
		tick(),
	)
}

func tick() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (d *Dashboard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		d.width = msg.Width
		d.height = msg.Height
		d.ready = true

		headerH := 2
		unitH := (d.height - headerH) * 3 / 5
		logH := d.height - headerH - unitH

		d.unitViewport = viewport.New(d.width, unitH)
		d.logViewport = viewport.New(d.width, logH)
		d.unitViewport.YPosition = headerH
		d.logViewport.YPosition = headerH + unitH

	case tickMsg:
		d.units = d.agt.Units()
		d.sysSnap = d.agt.SystemInfo()
		d.logLines = d.agt.LogEntries()

		d.unitViewport.SetContent(d.renderUnits())
		d.logViewport.SetContent(d.renderLogs())

		return d, tick()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return d, tea.Quit
		case "up", "k":
			d.unitViewport.LineUp(1)
		case "down", "j":
			d.unitViewport.LineDown(1)
		case "pageup":
			d.unitViewport.HalfViewUp()
		case "pagedown":
			d.unitViewport.HalfViewDown()
		}
	}

	return d, nil
}

func (d *Dashboard) View() string {
	if !d.ready {
		return "loading..."
	}

	var b strings.Builder
	b.WriteString(d.renderHeader())
	b.WriteString("\n")
	b.WriteString(d.unitViewport.View())
	b.WriteString(d.logViewport.View())
	return b.String()
}

func (d *Dashboard) renderHeader() string {
	sys := d.sysSnap
	cpu := fmt.Sprintf("%.1f%%", sys.CPU.User+sys.CPU.System)
	mem := fmt.Sprintf("%.1f%%", sys.Memory.PercentUsed)

	load1, load5, load15, _ := sysinfo.LoadAvg()
	hostname := sysinfo.Hostname()
	proc, _ := sysinfo.Processes()
	kernel := sysinfo.KernelVersion()

	uptime := time.Duration(0)
	if sys.Uptime > 0 {
		uptime = sys.Uptime
	}
	uptimeStr := formatDuration(uptime)

	b := d.width - 2
	if b < 60 {
		b = 60
	}

	line := fmt.Sprintf(" tenax  %s | kernel %s | cpu %s | mem %s | load %.2f %.2f %.2f | procs %d | up %s",
		hostname, kernel, cpu, mem, load1, load5, load15, proc, uptimeStr)
	if len(line) > d.width-2 {
		line = line[:d.width-5] + "..."
	}
	return headerStyle(lipgloss.PlaceHorizontal(d.width, lipgloss.Left, line))
}

func (d *Dashboard) renderUnits() string {
	var b strings.Builder
	total := len(d.units)
	active := 0
	failed := 0
	for _, u := range d.units {
		switch u.ActiveState {
		case "active":
			active++
		case "failed":
			failed++
		}
		_ = active
	}

	header := fmt.Sprintf(" %s  %s total, %d failed", titleStyle("Services"), total, failed)
	b.WriteString(header)
	b.WriteString("\n\n")

	for i, u := range d.units {
		if i >= 20 {
			break
		}
		state := d.renderState(u)
		name := u.Name
		if len(name) > 50 {
			name = name[:50]
		}
		b.WriteString(fmt.Sprintf(" %s %s\n", state, name))
	}

	return b.String()
}

func (d *Dashboard) renderState(u unit.Unit) string {
	switch {
	case u.ActiveState == "failed":
		return failedStyle(" ● failed ")
	case u.ActiveState == "active" && u.SubState == "running":
		return activeStyle(" ● active ")
	}
	return inactiveStyle(" ○ " + u.ActiveState)
}

func (d *Dashboard) renderLogs() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(" %s  last %d entries", titleStyle("Logs"), len(d.logLines)))
	b.WriteString("\n\n")

	start := len(d.logLines) - 30
	if start < 0 {
		start = 0
	}
	for _, e := range d.logLines[start:] {
		ts := e.Timestamp.Format("15:04:05")
		unit := e.Unit
		if len(unit) > 24 {
			unit = unit[:24]
		}
		msg := e.Message
		if len(msg) > d.width-50 {
			msg = msg[:d.width-53] + "..."
		}
		b.WriteString(fmt.Sprintf(" %s %-25s %s\n", labelStyle(ts), unit, msg))
	}

	return b.String()
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd%dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
