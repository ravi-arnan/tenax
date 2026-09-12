package controller

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	tenaxv1 "github.com/ravi-arnan/tenax/proto/tenaxv1"
)

// insecureCreds is a transport credential that accepts everything (bufconn).
type insecureCreds struct{}

func (insecureCreds) ClientHandshake(_ context.Context, _ string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, credentials.TLSInfo{}, nil
}
func (insecureCreds) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return conn, credentials.TLSInfo{}, nil
}
func (insecureCreds) Info() credentials.ProtocolInfo          { return credentials.ProtocolInfo{} }
func (insecureCreds) Clone() credentials.TransportCredentials { return insecureCreds{} }
func (insecureCreds) OverrideServerName(string) error         { return nil }

// startBufconn spins up the gRPC server over an in-memory listener.
func startBufconn(t *testing.T) (*Registry, *tenaxv1.AgentServiceClient) {
	t.Helper()

	reg := NewRegistry(2 * time.Minute)
	grpcSrv := grpc.NewServer()
	tenaxv1.RegisterAgentServiceServer(grpcSrv, NewGRPCServer(reg))

	ln := bufconn.Listen(1024 * 1024)
	go grpcSrv.Serve(ln)
	t.Cleanup(grpcSrv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return ln.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecureCreds{}),
	)
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	client := tenaxv1.NewAgentServiceClient(conn)
	return reg, &client
}

func TestConnectStreamIntegration(t *testing.T) {
	reg, client := startBufconn(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := (*client).Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Registration snapshot.
	if err := stream.Send(testSnapshot("alpha", 0)); err != nil {
		t.Fatalf("send: %v", err)
	}
	// Update with a failure.
	if err := stream.Send(testSnapshot("alpha", 2)); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Give the server a moment to process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h, ok := reg.Host("alpha"); ok && h.Status.GetUnitsFailed() == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	h, ok := reg.Host("alpha")
	if !ok {
		t.Fatal("alpha not registered")
	}
	if h.Status.GetUnitsFailed() != 2 {
		t.Errorf("failed units = %d, want 2", h.Status.GetUnitsFailed())
	}
	if !h.StreamOpen {
		t.Error("stream should be marked open")
	}

	stream.CloseSend()
	time.Sleep(50 * time.Millisecond)

	h, _ = reg.Host("alpha")
	if h.StreamOpen {
		t.Error("stream should be marked closed after CloseSend + EOF")
	}
}

func TestHTTPHandlers(t *testing.T) {
	reg := NewRegistry(2 * time.Minute)
	reg.Upsert(testSnapshot("alpha", 1), true)

	srv := &Server{reg: reg}
	mux := http.NewServeMux()
	srv.RegisterHTTP(mux)

	// Fleet list.
	req := httptest.NewRequest("GET", "/api/hosts", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("hosts status = %d", rec.Code)
	}
	var out struct {
		Hosts []struct {
			ID          string `json:"id"`
			FleetStatus string `json:"fleet_status"`
		} `json:"hosts"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Hosts) != 1 || out.Hosts[0].ID != "alpha" || out.Hosts[0].FleetStatus != "degraded" {
		t.Errorf("hosts output wrong: %+v", out.Hosts)
	}

	// Single host.
	req = httptest.NewRequest("GET", "/api/hosts/alpha", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("host status = %d", rec.Code)
	}

	// Missing host.
	req = httptest.NewRequest("GET", "/api/hosts/nope", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	// HTML index renders host.
	req = httptest.NewRequest("GET", "/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 || !bytesContains(rec.Body.Bytes(), []byte("alpha")) {
		t.Errorf("index should render host alpha")
	}
}

func bytesContains(b, sub []byte) bool {
	return string(b) != "" && indexOfBytes(b, sub) >= 0
}

func indexOfBytes(b, sub []byte) int {
	for i := 0; i+len(sub) <= len(b); i++ {
		match := true
		for j := range sub {
			if b[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

var _ = timestamppb.Now
