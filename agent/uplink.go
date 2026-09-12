package agent

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	tenaxv1 "github.com/ravi-arnan/tenax/proto/tenaxv1"
	"github.com/ravi-arnan/tenax/sysinfo"
)

func osName() string {
	return fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
}

func timeToProto(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		t = time.Unix(0, 0)
	}
	return timestamppb.New(t)
}

// Version is the agent version reported to controllers. Kept in sync with
// api.Version.
const Version = "0.1.0-dev"

// defaultReportInterval matches the controller's preferred cadence; the
// value from ConnectResponse can override it.
const defaultReportInterval = 10 * time.Second

// Uplink streams snapshots from this agent to a controller.
type Uplink struct {
	agent    *Agent
	endpoint string
}

// NewUplink creates an uplink to the controller at endpoint (host:port).
func NewUplink(a *Agent, endpoint string) *Uplink {
	return &Uplink{agent: a, endpoint: endpoint}
}

// Run dials the controller and streams snapshots until ctx is canceled,
// reconnecting with backoff on failure. Backoff resets after a
// successful connection (at least one snapshot sent).
func (u *Uplink) Run(ctx context.Context) {
	const initialBackoff = 2 * time.Second
	const maxBackoff = 60 * time.Second
	backoff := initialBackoff

	for {
		if ctx.Err() != nil {
			return
		}

		sent, err := u.streamOnce(ctx)

		// A clean EOF means the server closed the stream; reset backoff.
		if err == nil {
			backoff = initialBackoff
			continue
		}

		if ctx.Err() != nil {
			return
		}

		// If we successfully sent at least one snapshot the connection was
		// established before it broke — reset backoff so we retry quickly.
		if sent {
			backoff = initialBackoff
		} else {
			// Connection never established; exponential backoff.
			if backoff < maxBackoff {
				backoff *= 2
			}
		}

		slog.Warn("uplink disconnected; retrying", "endpoint", u.endpoint, "error", err, "backoff", backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// streamOnce dials, registers, and streams until an error occurs. It
// reports whether at least one snapshot was successfully sent.
func (u *Uplink) streamOnce(ctx context.Context) (sent bool, err error) {
	conn, err := grpc.NewClient(u.endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	client := tenaxv1.NewAgentServiceClient(conn)

	// Respect overall ctx: wrap the stream so Recv/Send stop on cancel.
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	stream, err := client.Connect(streamCtx)
	if err != nil {
		return false, err
	}

	// Read the server's response (cadence hint) in the background.
	reportInterval := defaultReportInterval
	respCh := make(chan *tenaxv1.ConnectResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := stream.CloseAndRecv()
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	select {
	case resp := <-respCh:
		if resp != nil && resp.GetReportIntervalSeconds() > 0 {
			reportInterval = time.Duration(resp.GetReportIntervalSeconds()) * time.Second
		}
	case err := <-errCh:
		// Stream broke before we sent anything; retry outer loop.
		return false, err
	case <-streamCtx.Done():
		return false, streamCtx.Err()
	}

	slog.Info("uplink established", "endpoint", u.endpoint, "interval", reportInterval)

	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()

	send := func() error {
		return stream.Send(u.snapshot())
	}

	// First snapshot immediately (acts as registration).
	if err := send(); err != nil {
		return false, err
	}

	for {
		select {
		case <-streamCtx.Done():
			return true, streamCtx.Err()
		case <-ticker.C:
			if err := send(); err != nil {
				return true, err
			}
		}
	}
}

// snapshot builds the current state batch for the controller.
func (u *Uplink) snapshot() *tenaxv1.Snapshot {
	a := u.agent

	units := a.Units()
	active, failed := 0, 0
	unitStates := make([]*tenaxv1.UnitState, 0, len(units))
	for _, un := range units {
		switch un.ActiveState {
		case "active":
			active++
		case "failed":
			failed++
		}
		unitStates = append(unitStates, &tenaxv1.UnitState{
			Name:        un.Name,
			ActiveState: un.ActiveState,
			SubState:    un.SubState,
			LoadState:   un.LoadState,
		})
	}

	sys := a.SystemInfo()
	load1, _, _, _ := sysinfo.LoadAvg()

	probeStates := make([]*tenaxv1.ProbeState, 0)
	for _, p := range a.Probes() {
		probeStates = append(probeStates, &tenaxv1.ProbeState{
			Name:                p.Probe.Name,
			Type:                string(p.Probe.Type),
			Status:              string(p.Status),
			ConsecutiveFailures: int32(p.ConsecFails),
		})
	}

	uptime := int64(0)
	if sys.Uptime > 0 {
		uptime = int64(sys.Uptime.Seconds())
	}

	return &tenaxv1.Snapshot{
		AgentVersion: Version,
		Hostname:     sysinfo.Hostname(),
		Os:           osName(),
		Kernel:       sysinfo.KernelVersion(),
		Status: &tenaxv1.HostStatus{
			UptimeSeconds: uptime,
			UnitsTotal:    int32(len(units)),
			UnitsActive:   int32(active),
			UnitsFailed:   int32(failed),
		},
		Units: unitStates,
		Latest: &tenaxv1.ResourceSample{
			CpuPercent: sys.CPU.User + sys.CPU.System,
			MemPercent: sys.Memory.PercentUsed,
			Load1:      load1,
			Time:       timeToProto(sys.Time),
		}, Probes: probeStates,
		SentAt: timeToProto(time.Now()),
		Tags:   a.cfg.Controller.Tags,
	}
}
