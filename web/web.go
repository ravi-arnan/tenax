// Package web serves the server-rendered dashboard UI. It is embedded in
// the binary and depends on nothing but html/template — no JS, no build
// step, consistent with the single-binary principle.
package web

import (
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/ravi-arnan/tenax/history"
	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/probe"
	"github.com/ravi-arnan/tenax/sysinfo"
)

//go:embed dashboard.html
var dashboardHTML string

var tmpl = template.Must(template.New("dashboard").Parse(dashboardHTML))

// Source provides the data the dashboard renders. Satisfied by *agent.Agent.
type Source interface {
	Units() []unit.Unit
	SystemInfo() sysinfo.Snapshot
	Probes() []probe.ProbeStatus
	UnitEvents() []history.Event
	ResourceSamples() []history.Sample
}

// Actions provides unit lifecycle operations. Satisfied by *agent.Agent.
type Actions interface {
	StartUnit(name string) (int, error)
	StopUnit(name string) (int, error)
	RestartUnit(name string) (int, error)
}

// Handler serves the dashboard at "/".
type Handler struct {
	src     Source
	actions Actions
}

// NewHandler creates the web UI handler.
func NewHandler(src Source, actions Actions) *Handler {
	return &Handler{src: src, actions: actions}
}

type unitView struct {
	Name       string
	Desc       string
	State      string
	SubState   string
	Active     bool
	Failed     bool
	CanStart   bool
	CanStop    bool
	CanRestart bool
}

type probeView struct {
	Name   string
	Type   string
	Status string
	Up     bool
	Bad    bool
}

type eventView struct {
	Time string
	Unit string
	From string
	To   string
}

type pageData struct {
	Hostname string
	Kernel   string
	CPU      float64
	Mem      float64
	Load1    float64
	Spark    string
	Units    []unitView
	Probes   []probeView
	Events   []eventView
}

// Register mounts the dashboard and action routes on the given mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.handleDashboard)
	mux.HandleFunc("POST /ui/units/{name}/{action}", h.handleUnitAction)
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	units := h.src.Units()
	uvs := make([]unitView, 0, len(units))
	for _, u := range units {
		uvs = append(uvs, unitView{
			Name:       u.Name,
			Desc:       u.Description,
			State:      u.ActiveState,
			SubState:   u.SubState,
			Active:     u.ActiveState == "active",
			Failed:     u.ActiveState == "failed",
			CanStart:   u.ActiveState != "active",
			CanStop:    u.ActiveState == "active" || u.ActiveState == "failed",
			CanRestart: u.ActiveState == "active",
		})
	}

	probes := h.src.Probes()
	pvs := make([]probeView, 0, len(probes))
	for _, p := range probes {
		pvs = append(pvs, probeView{
			Name:   p.Probe.Name,
			Type:   string(p.Probe.Type),
			Status: string(p.Status),
			Up:     p.Status == probe.StatusHealthy,
			Bad:    p.Status == probe.StatusDown || p.Status == probe.StatusDegraded,
		})
	}

	events := h.src.UnitEvents()
	evs := make([]eventView, 0, len(events))
	// Most recent first for display.
	for i := len(events) - 1; i >= 0 && len(evs) < 15; i-- {
		e := events[i]
		evs = append(evs, eventView{
			Time: e.Time.Format("15:04:05"),
			Unit: e.Unit,
			From: e.From,
			To:   e.To,
		})
	}

	samples := h.src.ResourceSamples()

	sys := h.src.SystemInfo()
	data := pageData{
		Hostname: sysinfo.Hostname(),
		Kernel:   sysinfo.KernelVersion(),
		CPU:      sys.CPU.User + sys.CPU.System,
		Mem:      sys.Memory.PercentUsed,
		Load1:    load1(),
		Spark:    sparkBlocks(samples),
		Units:    uvs,
		Probes:   pvs,
		Events:   evs,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("web template", "error", err)
	}
}

func (h *Handler) handleUnitAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	action := r.PathValue("action")

	var err error
	switch action {
	case "start":
		_, err = h.actions.StartUnit(name)
	case "stop":
		_, err = h.actions.StopUnit(name)
	case "restart":
		_, err = h.actions.RestartUnit(name)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	if err != nil {
		slog.Error("web unit action", "action", action, "unit", name, "error", err)
	}

	// Post/Redirect/Get back to the dashboard.
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func load1() float64 {
	v, _, _, err := sysinfo.LoadAvg()
	if err != nil {
		return 0
	}
	return v
}

// sparkBlocks renders recent CPU samples as unicode blocks for the header.
func sparkBlocks(samples []history.Sample) string {
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	const maxPoints = 30
	if len(samples) > maxPoints {
		samples = samples[len(samples)-maxPoints:]
	}
	if len(samples) < 2 {
		return ""
	}
	out := make([]rune, 0, len(samples))
	for _, s := range samples {
		idx := int(s.CPUPct / 100 * float64(len(blocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		out = append(out, blocks[idx])
	}
	return string(out)
}
