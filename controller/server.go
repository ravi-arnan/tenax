package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"

	tenaxv1 "github.com/ravi-arnan/tenax/proto/tenaxv1"
)

// ControllerID identifies this controller in ConnectResponse.
const ControllerID = "tenax-controller-1"

// DefaultReportInterval is the cadence the controller asks agents for.
const DefaultReportInterval = 10 * time.Second

// GRPCServer implements tenaxv1.AgentServiceServer.
type GRPCServer struct {
	tenaxv1.UnimplementedAgentServiceServer

	reg *Registry
	mu  sync.Mutex
	// openStreams tracks live stream IDs per host for diagnostics.
	openStreams map[string]int
}

// NewGRPCServer creates the agent-facing gRPC server.
func NewGRPCServer(reg *Registry) *GRPCServer {
	return &GRPCServer{
		reg:         reg,
		openStreams: make(map[string]int),
	}
}

// Connect consumes a client-stream of snapshots from one agent. The
// stream ends when the agent closes it or context is canceled.
func (s *GRPCServer) Connect(stream tenaxv1.AgentService_ConnectServer) error {
	var hostID string

	defer func() {
		if hostID != "" {
			s.mu.Lock()
			s.openStreams[hostID]--
			if s.openStreams[hostID] <= 0 {
				delete(s.openStreams, hostID)
			}
			s.mu.Unlock()
			s.reg.MarkClosed(hostID)
			slog.Info("agent stream closed", "host", hostID)
		}
	}()

	for {
		snap, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if hostID == "" {
			hostID = snap.GetHostname()
			if hostID == "" {
				hostID = "unknown"
			}
			s.mu.Lock()
			s.openStreams[hostID]++
			s.mu.Unlock()
			slog.Info("agent connected", "host", hostID, "version", snap.GetAgentVersion())
		}

		s.reg.Upsert(snap, true)
	}
}

// Server bundles the gRPC and HTTP surfaces.
type Server struct {
	grpc    *GRPCServer
	reg     *Registry
	grpcSrv *grpc.Server
	httpSrv *http.Server

	grpcLn net.Listener
	httpLn net.Listener
}

// NewServer wires the controller servers on the given listeners (nil
// listeners are created on defaults: :9731 grpc, 127.0.0.1:9730 http).
func NewServer(reg *Registry, grpcLn, httpLn net.Listener) (*Server, error) {
	var err error
	if grpcLn == nil {
		grpcLn, err = net.Listen("tcp", ":9731")
		if err != nil {
			return nil, fmt.Errorf("grpc listen: %w", err)
		}
	}
	if httpLn == nil {
		httpLn, err = net.Listen("tcp", "127.0.0.1:9730")
		if err != nil {
			return nil, fmt.Errorf("http listen: %w", err)
		}
	}
	return &Server{
		grpc:   NewGRPCServer(reg),
		reg:    reg,
		grpcLn: grpcLn,
		httpLn: httpLn,
	}, nil
}

// Start launches both servers in background goroutines.
func (s *Server) Start() error {
	go func() {
		grpcSrv := grpc.NewServer()
		tenaxv1.RegisterAgentServiceServer(grpcSrv, s.grpc)
		if err := grpcSrv.Serve(s.grpcLn); err != nil {
			slog.Error("controller grpc", "error", err)
		}
	}()
	go func() {
		mux := http.NewServeMux()
		s.RegisterHTTP(mux)
		if err := http.Serve(s.httpLn, mux); err != nil {
			slog.Error("controller http", "error", err)
		}
	}()
	return nil
}

// Addrs returns the bound addresses.
func (s *Server) Addrs() (grpcAddr, httpAddr string) {
	return s.grpcLn.Addr().String(), s.httpLn.Addr().String()
}

// RegisterHTTP mounts the controller REST + HTML routes.
func (s *Server) RegisterHTTP(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/hosts", s.handleHosts)
	mux.HandleFunc("GET /api/hosts/{id}", s.handleHost)
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	hosts := s.reg.List()

	// Optional group filter: ?group=tag keeps only hosts carrying the tag.
	group := r.URL.Query().Get("group")
	type hostOut struct {
		Host
		FleetStatus string `json:"fleet_status"`
	}
	out := make([]hostOut, 0, len(hosts))
	for _, h := range hosts {
		if group != "" && !h.HasTag(group) {
			continue
		}
		out = append(out, hostOut{Host: h, FleetStatus: h.StatusNow(now, s.reg.deadline)})
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"hosts": out})
}

func (s *Server) handleHost(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h, ok := s.reg.Host(id)
	if !ok {
		http.Error(w, `{"error":"host not found"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	hosts := s.reg.List()

	// Optional group filter from ?group=tag; also collect all tags to
	// render filter links.
	group := r.URL.Query().Get("group")
	tagSet := make(map[string]bool)
	for _, h := range hosts {
		for _, t := range h.Tags {
			tagSet[t] = true
		}
	}
	if group != "" {
		filtered := hosts[:0]
		for _, h := range hosts {
			if h.HasTag(group) {
				filtered = append(filtered, h)
			}
		}
		hosts = filtered
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><meta charset="utf-8">`)
	b.WriteString(`<meta http-equiv="refresh" content="10"><title>tenax fleet</title>`)
	b.WriteString(`<style>body{background:#0d1117;color:#e6edf3;font:14px ui-monospace,monospace;padding:1.5rem}
	h1{font-size:1.1rem}table{border-collapse:collapse}td,th{padding:0.35rem 0.9rem;border-bottom:1px solid #30363d;text-align:left}
	th{color:#8b949e;font-weight:normal}.healthy{color:#3fb950}.degraded{color:#d29922}.stale,.disconnected{color:#f85149}.muted{color:#8b949e}</style></head><body>`)
	b.WriteString(`<h1>tenax fleet</h1>`)

	// Group filter links.
	if len(tagSet) > 0 {
		b.WriteString(`<p>group: `)
		if group == "" {
			b.WriteString(`<strong>all</strong>`)
		} else {
			b.WriteString(`<a href="/">all</a>`)
		}
		tags := make([]string, 0, len(tagSet))
		for t := range tagSet {
			tags = append(tags, t)
		}
		sort.Strings(tags)
		for _, t := range tags {
			if t == group {
				b.WriteString(` | <strong>` + html.EscapeString(t) + `</strong>`)
			} else {
				b.WriteString(` | <a href="/?group=` + html.EscapeString(t) + `">` + html.EscapeString(t) + `</a>`)
			}
		}
		b.WriteString(`</p>`)
	}

	b.WriteString(fmt.Sprintf(`<p class="muted">%d hosts</p>`, len(hosts)))
	b.WriteString(`<table><tr><th></th><th>host</th><th>status</th><th>cpu</th><th>mem</th><th>failed units</th><th>last seen</th><tr>`)

	for _, h := range hosts {
		status := h.StatusNow(now, s.reg.deadline)
		cpu, mem := "-", "-"
		if h.Latest != nil {
			cpu = fmt.Sprintf("%.1f%%", h.Latest.CPUPercent)
			mem = fmt.Sprintf("%.1f%%", h.Latest.MemPercent)
		}
		lastSeen := now.Sub(h.LastSeen).Round(time.Second)
		dot := map[string]string{"healthy": "healthy", "degraded": "degraded"}[status]
		if dot == "" {
			dot = "stale"
		}
		b.WriteString(fmt.Sprintf(`<tr><td class="%s">●</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td class="muted">%s ago</td></tr>`,
			dot, html.EscapeString(h.ID), status, cpu, mem, h.Status.GetUnitsFailed(), lastSeen))
	}
	b.WriteString(`</table></body></html>`)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, b.String())
}

// Stop is a placeholder for graceful shutdown (streams are client-driven).
func (s *Server) Stop(ctx context.Context) error {
	return nil
}
