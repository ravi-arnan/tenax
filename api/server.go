package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ravi-arnan/tenax/alert"
	"github.com/ravi-arnan/tenax/history"
	"github.com/ravi-arnan/tenax/log"
	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/probe"
	"github.com/ravi-arnan/tenax/service"
	"github.com/ravi-arnan/tenax/sysinfo"
	"github.com/ravi-arnan/tenax/web"
)

// Handler serves the tenax HTTP API on a chosen mux:
//
//	GET  /                            dashboard UI (embedded HTML)
//	POST /ui/units/{name}/{action}    unit actions from the dashboard
//	GET  /metrics                     Prometheus metrics
//	GET  /api/status                  JSON status snapshot
//	GET  /api/units                   list tracked units
//	GET  /api/units/{name}            single unit
//	POST /api/units/{name}/start      start unit
//	POST /api/units/{name}/stop       stop unit
//	POST /api/units/{name}/restart    restart unit
//	GET  /api/logs?unit=&limit=       recent journal entries
//	GET  /api/probes                  probe statuses
//	GET  /api/alerts                  alert rule states
//	GET  /healthz                     liveness
type Handler struct {
	mux     *http.ServeMux
	source  Source
	actions UnitActions
	reg     *prometheus.Registry
}

// Source provides read-only state. Satisfied by *agent.Agent.
type Source interface {
	Units() []unit.Unit
	Unit(name string) (unit.Unit, bool)
	SystemInfo() sysinfo.Snapshot
	LogEntries() []log.Entry
	Probes() []probe.ProbeStatus
	AlertStates() []alert.StateSnapshot
	UnitEvents() []history.Event
	ResourceSamples() []history.Sample
}

// UnitActions provides unit lifecycle operations. Satisfied by *agent.Agent.
type UnitActions interface {
	StartUnit(name string) (int, error)
	StopUnit(name string) (int, error)
	RestartUnit(name string) (int, error)
}

// NewHandler builds the API handler. Both interfaces are satisfied by
// *agent.Agent.
func NewHandler(src Source, actions UnitActions) *Handler {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
		newMetricsCollector(src),
	)

	mux := http.NewServeMux()

	// Dashboard UI at / (embedded, server-rendered).
	webHandler := web.NewHandler(src, actions)
	webHandler.Register(mux)

	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /api/status", handleStatus(src))
	mux.HandleFunc("GET /api/units", handleUnits(src))
	mux.HandleFunc("GET /api/units/{name}", handleUnit(src))
	mux.HandleFunc("POST /api/units/{name}/start", unitAction(src, actions, "start"))
	mux.HandleFunc("POST /api/units/{name}/stop", unitAction(src, actions, "stop"))
	mux.HandleFunc("POST /api/units/{name}/restart", unitAction(src, actions, "restart"))
	mux.HandleFunc("GET /api/logs", handleLogs(src))
	mux.HandleFunc("GET /api/history/units", handleUnitEvents(src))
	mux.HandleFunc("GET /api/history/resources", handleResourceSamples(src))
	mux.HandleFunc("GET /api/probes", handleProbes(src))
	mux.HandleFunc("GET /api/alerts", handleAlerts(src))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return &Handler{mux: mux, source: src, actions: actions, reg: reg}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// Server wraps Handler with a listener and graceful shutdown.
type Server struct {
	srv *http.Server
	ln  net.Listener
}

// NewServer creates the API server bound to the given address (or the
// service default when addr is empty).
func NewServer(src Source, actions UnitActions, addr string) (*Server, error) {
	ln, err := service.Listen(addr)
	if err != nil {
		return nil, err
	}
	return &Server{
		srv: &http.Server{
			Handler:           NewHandler(src, actions),
			ReadHeaderTimeout: 5 * time.Second,
		},
		ln: ln,
	}, nil
}

// Start serves in a background goroutine.
func (s *Server) Start() error {
	go func() {
		if err := s.srv.Serve(s.ln); err != nil && err != http.ErrServerClosed {
			slog.Error("api server", "error", err)
		}
	}()
	return nil
}

// Addr returns the listener address.
func (s *Server) Addr() string {
	return s.ln.Addr().String()
}

// Stop gracefully shuts down.
func (s *Server) Stop(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// --- handlers ---

type probeJSON struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Fails  int    `json:"consecutive_failures"`
}

type sysMemJSON struct {
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
	Percent    float64 `json:"percent_used"`
}

type statusJSON struct {
	Hostname string      `json:"hostname"`
	Time     time.Time   `json:"time"`
	Units    int         `json:"units_total"`
	Active   int         `json:"units_active"`
	Failed   int         `json:"units_failed"`
	Probes   []probeJSON `json:"probes"`
	Memory   sysMemJSON  `json:"memory"`
}

func handleStatus(src Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		units := src.Units()
		active, failed := 0, 0
		for _, u := range units {
			switch u.ActiveState {
			case "active":
				active++
			case "failed":
				failed++
			}
		}

		sys := src.SystemInfo()
		probeList := make([]probeJSON, 0)
		for _, p := range src.Probes() {
			probeList = append(probeList, probeJSON{
				Name:   p.Probe.Name,
				Type:   string(p.Probe.Type),
				Status: string(p.Status),
				Fails:  p.ConsecFails,
			})
		}

		writeJSON(w, http.StatusOK, statusJSON{
			Hostname: hostName(),
			Time:     time.Now(),
			Units:    len(units),
			Active:   active,
			Failed:   failed,
			Probes:   probeList,
			Memory: sysMemJSON{
				UsedBytes:  sys.Memory.UsedBytes,
				TotalBytes: sys.Memory.TotalBytes,
				Percent:    sys.Memory.PercentUsed,
			},
		})
	}
}

func handleUnits(src Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		units := src.Units()

		stateFilter := r.URL.Query().Get("state")
		if stateFilter != "" {
			filtered := units[:0]
			for _, u := range units {
				if u.ActiveState == stateFilter {
					filtered = append(filtered, u)
				}
			}
			units = filtered
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{"units": units})
	}
}

func handleUnit(src Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		u, ok := src.Unit(name)
		if !ok {
			writeErr(w, http.StatusNotFound, "unit not found: "+name)
			return
		}
		writeJSON(w, http.StatusOK, u)
	}
}

func unitAction(src Source, actions UnitActions, action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if _, ok := src.Unit(name); !ok {
			writeErr(w, http.StatusNotFound, "unit not found: "+name)
			return
		}

		var jobID int
		var err error
		switch action {
		case "start":
			jobID, err = actions.StartUnit(name)
		case "stop":
			jobID, err = actions.StopUnit(name)
		case "restart":
			jobID, err = actions.RestartUnit(name)
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, action+" "+name+": "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"unit":   name,
			"action": action,
			"job":    jobID,
		})
	}
}

func handleLogs(src Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		limit := 200
		if s := q.Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > 5000 {
				writeErr(w, http.StatusBadRequest, "limit must be 1-5000")
				return
			}
			limit = n
		}

		unitFilter := q.Get("unit")
		minPrio := -1
		if s := q.Get("priority"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 || n > 7 {
				writeErr(w, http.StatusBadRequest, "priority must be 0-7")
				return
			}
			minPrio = n
		}

		entries := src.LogEntries()
		out := make([]log.Entry, 0, len(entries))
		for _, e := range entries {
			if unitFilter != "" && e.Unit != unitFilter {
				continue
			}
			if minPrio >= 0 && e.Priority > minPrio {
				continue
			}
			out = append(out, e)
		}

		if len(out) > limit {
			out = out[len(out)-limit:]
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"entries": out})
	}
}

func handleUnitEvents(src Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events := src.UnitEvents()
		if events == nil {
			events = []history.Event{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"events": events})
	}
}

func handleResourceSamples(src Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		samples := src.ResourceSamples()
		if samples == nil {
			samples = []history.Sample{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"samples": samples})
	}
}

func handleProbes(src Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		probes := src.Probes()
		out := make([]probeJSON, 0, len(probes))
		for _, p := range probes {
			out = append(out, probeJSON{
				Name:   p.Probe.Name,
				Type:   string(p.Probe.Type),
				Status: string(p.Status),
				Fails:  p.ConsecFails,
			})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"probes": out})
	}
}

func handleAlerts(src Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		states := src.AlertStates()
		if states == nil {
			states = []alert.StateSnapshot{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"alerts": states})
	}
}

func hostName() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
