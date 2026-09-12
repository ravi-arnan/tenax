package health_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ravi-arnan/tenax/pkg/health"
)

func TestHTTPChecker(t *testing.T) {
	t.Run("healthy 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		c := health.HTTPChecker{}
		res := c.Check(context.Background(), health.Probe{Name: "t", Type: health.TypeHTTP, Target: srv.URL, Timeout: 2 * time.Second})
		if !res.Success {
			t.Fatalf("expected success, got error: %s", res.Error)
		}
	})

	t.Run("500 is failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		c := health.HTTPChecker{}
		res := c.Check(context.Background(), health.Probe{Name: "t", Type: health.TypeHTTP, Target: srv.URL, Timeout: 2 * time.Second})
		if res.Success {
			t.Fatal("expected failure for 500")
		}
		if res.Error == "" {
			t.Fatal("expected error message")
		}
	})

	t.Run("connection refused is failure", func(t *testing.T) {
		// Port 1 on localhost should be closed.
		c := health.HTTPChecker{}
		res := c.Check(context.Background(), health.Probe{Name: "t", Type: health.TypeHTTP, Target: "http://127.0.0.1:1", Timeout: 2 * time.Second})
		if res.Success {
			t.Fatal("expected failure for refused connection")
		}
	})

	t.Run("redirect followed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/final" {
				w.WriteHeader(http.StatusOK)
				return
			}
			http.Redirect(w, r, "/final", http.StatusFound)
		}))
		defer srv.Close()

		c := health.HTTPChecker{}
		res := c.Check(context.Background(), health.Probe{Name: "t", Type: health.TypeHTTP, Target: srv.URL, Timeout: 2 * time.Second})
		if !res.Success {
			t.Fatalf("expected redirect to be followed: %s", res.Error)
		}
	})
}

func TestPortChecker(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	addr := ln.Addr().String()

	t.Run("open port", func(t *testing.T) {
		c := health.PortChecker{}
		res := c.Check(context.Background(), health.Probe{Name: "t", Type: health.TypePort, Target: addr, Timeout: 2 * time.Second})
		if !res.Success {
			t.Fatalf("expected success: %s", res.Error)
		}
	})

	t.Run("closed port", func(t *testing.T) {
		c := health.PortChecker{}
		res := c.Check(context.Background(), health.Probe{Name: "t", Type: health.TypePort, Target: "127.0.0.1:1", Timeout: 2 * time.Second})
		if res.Success {
			t.Fatal("expected failure for closed port")
		}
	})
}

func TestExecChecker(t *testing.T) {
	t.Run("exit 0 healthy", func(t *testing.T) {
		c := health.ExecChecker{}
		res := c.Check(context.Background(), health.Probe{Name: "t", Type: health.TypeExec, Target: "true", Timeout: 2 * time.Second})
		if !res.Success {
			t.Fatalf("expected success: %s", res.Error)
		}
	})

	t.Run("non-zero exit unhealthy", func(t *testing.T) {
		c := health.ExecChecker{}
		res := c.Check(context.Background(), health.Probe{Name: "t", Type: health.TypeExec, Target: "exit 3", Timeout: 2 * time.Second})
		if res.Success {
			t.Fatal("expected failure for exit 3")
		}
	})

	t.Run("captured output on failure", func(t *testing.T) {
		c := health.ExecChecker{}
		res := c.Check(context.Background(), health.Probe{Name: "t", Type: health.TypeExec, Target: "echo boom; exit 1", Timeout: 2 * time.Second})
		if res.Success {
			t.Fatal("expected failure")
		}
		if !contains(res.Error, "boom") {
			t.Fatalf("expected output in error, got: %s", res.Error)
		}
	})
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		probe   health.Probe
		wantErr bool
	}{
		{"valid http", health.Probe{Name: "a", Type: health.TypeHTTP, Target: "http://x"}, false},
		{"valid port", health.Probe{Name: "a", Type: health.TypePort, Target: "h:1"}, false},
		{"valid exec", health.Probe{Name: "a", Type: health.TypeExec, Target: "true"}, false},
		{"missing name", health.Probe{Type: health.TypeHTTP, Target: "http://x"}, true},
		{"missing target", health.Probe{Name: "a", Type: health.TypeHTTP}, true},
		{"bad port target", health.Probe{Name: "a", Type: health.TypePort, Target: "no-port"}, true},
		{"unknown type", health.Probe{Name: "a", Type: "snmp", Target: "x"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.probe.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestWithDefaults(t *testing.T) {
	p := health.Probe{Name: "a", Type: health.TypeHTTP, Target: "http://x"}.WithDefaults()
	if p.Interval != 30*time.Second {
		t.Errorf("interval default = %v, want 30s", p.Interval)
	}
	if p.Timeout != 5*time.Second {
		t.Errorf("timeout default = %v, want 5s", p.Timeout)
	}
	if p.Retries != 0 {
		t.Errorf("retries default = %d, want 0", p.Retries)
	}
}

func TestCheckerFor(t *testing.T) {
	for _, typ := range []health.ProbeType{health.TypeHTTP, health.TypePort, health.TypeExec} {
		if _, err := health.CheckerFor(typ); err != nil {
			t.Errorf("CheckerFor(%s): %v", typ, err)
		}
	}
	if _, err := health.CheckerFor("bogus"); err == nil {
		t.Error("expected error for unknown type")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

var _ = filepath.Join
var _ = os.Getenv
