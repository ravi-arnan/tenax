package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ravi-arnan/tenax/history"
	"github.com/ravi-arnan/tenax/pkg/health"
	"github.com/ravi-arnan/tenax/pkg/unit"
	"github.com/ravi-arnan/tenax/probe"
	"github.com/ravi-arnan/tenax/sysinfo"
)

type fakeSource struct {
	units   []unit.Unit
	probes  []probe.ProbeStatus
	events  []history.Event
	samples []history.Sample
}

func (f *fakeSource) Units() []unit.Unit { return f.units }
func (f *fakeSource) SystemInfo() sysinfo.Snapshot {
	return sysinfo.Snapshot{CPU: sysinfo.CPU{User: 10}}
}
func (f *fakeSource) Probes() []probe.ProbeStatus       { return f.probes }
func (f *fakeSource) UnitEvents() []history.Event       { return f.events }
func (f *fakeSource) ResourceSamples() []history.Sample { return f.samples }

type fakeActions struct{ called []string }

func (f *fakeActions) StartUnit(n string) (int, error) {
	f.called = append(f.called, "start:"+n)
	return 1, nil
}
func (f *fakeActions) StopUnit(n string) (int, error) {
	f.called = append(f.called, "stop:"+n)
	return 2, nil
}
func (f *fakeActions) RestartUnit(n string) (int, error) {
	f.called = append(f.called, "restart:"+n)
	return 3, nil
}

func fixture() (*fakeSource, *fakeActions, *Handler) {
	src := &fakeSource{
		units: []unit.Unit{
			{Name: "nginx.service", Description: "Web server", ActiveState: "active", SubState: "running"},
			{Name: "down.service", ActiveState: "inactive"},
			{Name: "broken.service", ActiveState: "failed"},
		},
		probes: []probe.ProbeStatus{
			{Probe: health.Probe{Name: "web", Type: health.TypeHTTP}, Status: probe.StatusHealthy},
			{Probe: health.Probe{Name: "db", Type: health.TypePort}, Status: probe.StatusDown},
		},
		events: []history.Event{
			{Unit: "nginx.service", From: "inactive", To: "active", Time: time.Now()},
		},
	}
	actions := &fakeActions{}
	return src, actions, NewHandler(src, actions)
}

func TestDashboardRender(t *testing.T) {
	_, _, h := fixture()

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.handleDashboard(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{"nginx.service", "Web server", "broken.service", "db", "down.service"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}

	// Action buttons: start only for non-active unit, stop only for active.
	if !strings.Contains(body, "/ui/units/down.service/start") {
		t.Error("expected start button for inactive unit")
	}
	if strings.Contains(body, "/ui/units/nginx.service/start") {
		t.Error("did not expect start button for active unit")
	}
	if !strings.Contains(body, "/ui/units/nginx.service/stop") {
		t.Error("expected stop button for active unit")
	}

	// Event is rendered most recent first.
	if !strings.Contains(body, "inactive →") {
		t.Error("expected transition rendering")
	}
}

func TestUnitActionRedirects(t *testing.T) {
	_, actions, h := fixture()

	mux := http.NewServeMux()
	h.Register(mux)

	for _, action := range []string{"start", "stop", "restart"} {
		req := httptest.NewRequest("POST", "/ui/units/nginx.service/"+action, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != 303 {
			t.Fatalf("%s: expected 303 redirect, got %d", action, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Errorf("%s: redirect to %q, want /", action, loc)
		}
	}

	if len(actions.called) != 3 {
		t.Errorf("expected 3 action calls, got %v", actions.called)
	}
	if actions.called[0] != "start:nginx.service" {
		t.Errorf("wrong call: %v", actions.called[0])
	}
}

func TestUnitActionUnknown(t *testing.T) {
	_, actions, h := fixture()

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest("POST", "/ui/units/nginx.service/destroy", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	if len(actions.called) != 0 {
		t.Errorf("no action should fire, got %v", actions.called)
	}
}

func TestHTMLAutoEscape(t *testing.T) {
	src, _, h := fixture()
	src.units = append(src.units, unit.Unit{
		Name:        `<script>alert(1)</script>.service`,
		Description: "<b>bold</b>",
		ActiveState: "active",
	})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.handleDashboard(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Error("unit name was not HTML-escaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("expected escaped output")
	}
}
