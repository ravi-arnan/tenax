package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ravi-arnan/tenax/alert"
	"github.com/ravi-arnan/tenax/history"
	"github.com/ravi-arnan/tenax/log"
	"github.com/ravi-arnan/tenax/pkg/health"
	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/probe"
	"github.com/ravi-arnan/tenax/sysinfo"
)

type fakeSource struct {
	units   []unit.Unit
	sys     sysinfo.Snapshot
	probes  []probe.ProbeStatus
	logs    []log.Entry
	alerts  []alert.StateSnapshot
	events  []history.Event
	samples []history.Sample
}

func (f *fakeSource) Units() []unit.Unit { return f.units }
func (f *fakeSource) Unit(name string) (unit.Unit, bool) {
	for _, u := range f.units {
		if u.Name == name {
			return u, true
		}
	}
	return unit.Unit{}, false
}
func (f *fakeSource) SystemInfo() sysinfo.Snapshot       { return f.sys }
func (f *fakeSource) LogEntries() []log.Entry            { return f.logs }
func (f *fakeSource) Probes() []probe.ProbeStatus        { return f.probes }
func (f *fakeSource) AlertStates() []alert.StateSnapshot { return f.alerts }
func (f *fakeSource) UnitEvents() []history.Event        { return f.events }
func (f *fakeSource) ResourceSamples() []history.Sample  { return f.samples }

type fakeActions struct {
	called []string
	err    error
}

func (f *fakeActions) StartUnit(name string) (int, error) {
	f.called = append(f.called, "start:"+name)
	return 42, f.err
}
func (f *fakeActions) StopUnit(name string) (int, error) {
	f.called = append(f.called, "stop:"+name)
	return 43, f.err
}
func (f *fakeActions) RestartUnit(name string) (int, error) {
	f.called = append(f.called, "restart:"+name)
	return 44, f.err
}

func testFixture() (*fakeSource, *fakeActions, *Handler) {
	src := &fakeSource{
		units: []unit.Unit{
			{Name: "nginx.service", ActiveState: "active", SubState: "running", LoadState: "loaded", PID: 100},
			{Name: "broken.service", ActiveState: "failed", LoadState: "loaded"},
		},
		sys: sysinfo.Snapshot{
			Memory: sysinfo.Memory{UsedBytes: 1, TotalBytes: 2, PercentUsed: 50},
		},
		probes: []probe.ProbeStatus{
			{Probe: health.Probe{Name: "web", Type: health.TypeHTTP}, Status: probe.StatusHealthy},
		},
		logs: []log.Entry{
			{Timestamp: time.Now(), Unit: "nginx.service", Message: "first", Priority: 6},
			{Timestamp: time.Now(), Unit: "nginx.service", Message: "error!", Priority: 3},
			{Timestamp: time.Now(), Unit: "sshd", Message: "other", Priority: 6},
		},
		alerts: []alert.StateSnapshot{{Name: "cpu-high", Kind: "cpu", State: alert.StateOK}},
	}
	actions := &fakeActions{}
	return src, actions, NewHandler(src, actions)
}

func do(h *Handler, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestStatusEndpoint(t *testing.T) {
	_, _, h := testFixture()
	rec := do(h, "GET", "/api/status")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var s statusJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.Units != 2 || s.Active != 1 || s.Failed != 1 {
		t.Errorf("unexpected counts: %+v", s)
	}
}

func TestUnitsListAndFilter(t *testing.T) {
	_, _, h := testFixture()

	rec := do(h, "GET", "/api/units")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var out struct {
		Units []unit.Unit `json:"units"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(out.Units))
	}
	if out.Units[0].Name != "nginx.service" {
		t.Errorf("json tags broken: %+v", out.Units[0])
	}

	rec = do(h, "GET", "/api/units?state=failed")
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Units) != 1 || out.Units[0].Name != "broken.service" {
		t.Errorf("state filter failed: %+v", out.Units)
	}
}

func TestUnitDetailAndNotFound(t *testing.T) {
	_, _, h := testFixture()

	rec := do(h, "GET", "/api/units/nginx.service")
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}

	rec = do(h, "GET", "/api/units/nope.service")
	if rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestUnitActions(t *testing.T) {
	src, actions, h := testFixture()

	for _, tc := range []struct{ action, want string }{
		{"start", "start:nginx.service"},
		{"stop", "stop:nginx.service"},
		{"restart", "restart:nginx.service"},
	} {
		rec := do(h, "POST", "/api/units/nginx.service/"+tc.action)
		if rec.Code != 200 {
			t.Fatalf("%s: status = %d", tc.action, rec.Code)
		}
		last := actions.called[len(actions.called)-1]
		if last != tc.want {
			t.Errorf("called = %q, want %q", last, tc.want)
		}
	}

	// Unknown unit -> 404 and no action call.
	rec := do(h, "POST", "/api/units/nope.service/start")
	if rec.Code != 404 || len(actions.called) != 3 {
		t.Errorf("expected 404 without action, got %d, called=%v", rec.Code, actions.called)
	}

	// Action failure -> 500.
	actions.err = errMock
	rec = do(h, "POST", "/api/units/"+src.units[0].Name+"/start")
	if rec.Code != 500 {
		t.Errorf("expected 500 on action error, got %d", rec.Code)
	}
}

var errMock = &mockError{}

type mockError struct{}

func (*mockError) Error() string { return "mock failure" }

func TestLogsFilters(t *testing.T) {
	_, _, h := testFixture()

	// All entries.
	rec := do(h, "GET", "/api/logs")
	var out struct {
		Entries []log.Entry `json:"entries"`
	}
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(out.Entries))
	}

	// Unit filter.
	rec = do(h, "GET", "/api/logs?unit=nginx.service")
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Entries) != 2 {
		t.Errorf("unit filter: expected 2, got %d", len(out.Entries))
	}

	// Priority filter (errors only).
	rec = do(h, "GET", "/api/logs?priority=3")
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Entries) != 1 || out.Entries[0].Message != "error!" {
		t.Errorf("priority filter failed: %+v", out.Entries)
	}

	// Limit.
	rec = do(h, "GET", "/api/logs?limit=1")
	json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Entries) != 1 {
		t.Errorf("limit failed: got %d", len(out.Entries))
	}

	// Bad limit.
	rec = do(h, "GET", "/api/logs?limit=bogus")
	if rec.Code != 400 {
		t.Errorf("expected 400 for bad limit, got %d", rec.Code)
	}
}

func TestProbesAndAlertsEndpoints(t *testing.T) {
	_, _, h := testFixture()

	rec := do(h, "GET", "/api/probes")
	var p struct {
		Probes []probeJSON `json:"probes"`
	}
	json.Unmarshal(rec.Body.Bytes(), &p)
	if len(p.Probes) != 1 || p.Probes[0].Name != "web" {
		t.Errorf("probes broken: %+v", p.Probes)
	}

	rec = do(h, "GET", "/api/alerts")
	var a struct {
		Alerts []alert.StateSnapshot `json:"alerts"`
	}
	json.Unmarshal(rec.Body.Bytes(), &a)
	if len(a.Alerts) != 1 || a.Alerts[0].Name != "cpu-high" {
		t.Errorf("alerts broken: %+v", a.Alerts)
	}
}

func TestHistoryEndpoints(t *testing.T) {
	src := &fakeSource{
		events: []history.Event{
			{Unit: "nginx.service", From: "active", To: "failed", Time: time.Now()},
		},
		samples: []history.Sample{
			{Time: time.Now(), CPUPct: 42.5, MemPct: 61.0, Load1: 0.5},
		},
	}
	h := NewHandler(src, &fakeActions{})

	rec := do(h, "GET", "/api/history/units")
	var ev struct {
		Events []history.Event `json:"events"`
	}
	json.Unmarshal(rec.Body.Bytes(), &ev)
	if len(ev.Events) != 1 || ev.Events[0].Unit != "nginx.service" || ev.Events[0].To != "failed" {
		t.Errorf("unit events broken: %+v", ev.Events)
	}

	rec = do(h, "GET", "/api/history/resources")
	var sm struct {
		Samples []history.Sample `json:"samples"`
	}
	json.Unmarshal(rec.Body.Bytes(), &sm)
	if len(sm.Samples) != 1 || sm.Samples[0].CPUPct != 42.5 {
		t.Errorf("resource samples broken: %+v", sm.Samples)
	}
}

func TestHealthzAndMethodRouting(t *testing.T) {
	_, _, h := testFixture()

	if rec := do(h, "GET", "/healthz"); rec.Code != 200 {
		t.Errorf("healthz = %d", rec.Code)
	}
	// Wrong method on a routed path -> 405.
	if rec := do(h, "POST", "/healthz"); rec.Code != 405 {
		t.Errorf("expected 405 for wrong method, got %d", rec.Code)
	}
	// Unknown path -> 404.
	if rec := do(h, "GET", "/api/nothing"); rec.Code != 404 {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}
