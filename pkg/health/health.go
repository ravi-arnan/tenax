// Package health defines the probe model and checkers used by the probe
// engine. A probe is a periodic health check against a target; a checker
// performs a single check and returns the result.
package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"time"
)

// ProbeType identifies what kind of check a probe performs.
type ProbeType string

const (
	TypeHTTP ProbeType = "http"
	TypePort ProbeType = "port"
	TypeExec ProbeType = "exec"
)

// Probe is a periodic health check definition.
type Probe struct {
	Name     string        `yaml:"name"`
	Type     ProbeType     `yaml:"type"`
	Target   string        `yaml:"target"` // URL, host:port, or command
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	Retries  int           `yaml:"retries"`
}

// Result is the outcome of a single probe run.
type Result struct {
	ProbeName string
	Success   bool
	Latency   time.Duration
	Error     string
	Timestamp time.Time
}

// Validate checks a probe definition for missing or invalid fields.
func (p Probe) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("probe name is required")
	}
	switch p.Type {
	case TypeHTTP:
		if p.Target == "" {
			return fmt.Errorf("probe %q: target (URL) is required", p.Name)
		}
	case TypePort:
		if _, _, err := net.SplitHostPort(p.Target); err != nil {
			return fmt.Errorf("probe %q: target must be host:port", p.Name)
		}
	case TypeExec:
		if p.Target == "" {
			return fmt.Errorf("probe %q: target (command) is required", p.Name)
		}
	default:
		return fmt.Errorf("probe %q: unknown type %q", p.Name, p.Type)
	}
	return nil
}

// WithDefaults fills in zero-valued fields with sane defaults.
func (p Probe) WithDefaults() Probe {
	if p.Interval <= 0 {
		p.Interval = 30 * time.Second
	}
	if p.Timeout <= 0 {
		p.Timeout = 5 * time.Second
	}
	if p.Retries < 0 {
		p.Retries = 0
	}
	return p
}

// Checker performs a single check for a probe.
type Checker interface {
	Check(ctx context.Context, p Probe) Result
}

// HTTPChecker checks an HTTP endpoint, treating any 2xx/3xx status as
// healthy.
type HTTPChecker struct {
	// Client is used for the request. Zero value is valid.
	Client *http.Client
}

func (h HTTPChecker) Check(ctx context.Context, p Probe) Result {
	start := time.Now()
	res := Result{ProbeName: p.Name, Timestamp: start}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Target, nil)
	if err != nil {
		res.Error = fmt.Sprintf("build request: %v", err)
		return res
	}
	req.Header.Set("User-Agent", "tenax-probe/0.1")

	client := h.Client
	if client == nil {
		client = &http.Client{
			Timeout: p.Timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		}
	}

	resp, err := client.Do(req)
	latency := time.Since(start)
	res.Latency = latency
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer resp.Body.Close()
	drain(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		res.Success = true
	} else {
		res.Error = fmt.Sprintf("unexpected status %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return res
}

// PortChecker checks that a TCP port accepts a connection.
type PortChecker struct{}

func (PortChecker) Check(ctx context.Context, p Probe) Result {
	start := time.Now()
	res := Result{ProbeName: p.Name, Timestamp: start}

	dialer := net.Dialer{Timeout: p.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", p.Target)
	res.Latency = time.Since(start)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	conn.Close()
	res.Success = true
	return res
}

// ExecChecker runs a command and considers exit status 0 healthy.
type ExecChecker struct {
	// Dir is the working directory for the command (optional).
	Dir string
}

func (e ExecChecker) Check(ctx context.Context, p Probe) Result {
	start := time.Now()
	res := Result{ProbeName: p.Name, Timestamp: start}

	cmd := exec.CommandContext(ctx, "sh", "-c", p.Target)
	cmd.Dir = e.Dir

	out, err := cmd.CombinedOutput()
	res.Latency = time.Since(start)
	if err != nil {
		res.Error = err.Error()
		if len(out) > 0 {
			res.Error = fmt.Sprintf("%s: %s", res.Error, truncate(string(out), 512))
		}
		return res
	}
	res.Success = true
	return res
}

// CheckerFor returns the checker for a probe type.
func CheckerFor(t ProbeType) (Checker, error) {
	switch t {
	case TypeHTTP:
		return HTTPChecker{}, nil
	case TypePort:
		return PortChecker{}, nil
	case TypeExec:
		return ExecChecker{}, nil
	default:
		return nil, fmt.Errorf("no checker for probe type %q", t)
	}
}

func drain(body interface{ Read([]byte) (int, error) }) {
	buf := make([]byte, 4096)
	for {
		if _, err := body.Read(buf); err != nil {
			return
		}
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
