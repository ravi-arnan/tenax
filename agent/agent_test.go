package agent

import (
	"testing"
	"time"

	"github.com/ravi-arnan/tenax/history"
	"github.com/ravi-arnan/tenax/pkg/unit"
)

func newTestAgent() *Agent {
	return &Agent{history: history.NewStore(100, 100)}
}

func TestRecordTransitions(t *testing.T) {
	a := newTestAgent()
	now := time.Now()

	prev := []unit.Unit{
		{Name: "nginx.service", ActiveState: "active"},
		{Name: "app.service", ActiveState: "active"},
	}
	curr := []unit.Unit{
		{Name: "nginx.service", ActiveState: "failed"},
		{Name: "app.service", ActiveState: "active"}, // unchanged
		{Name: "new.service", ActiveState: "active"}, // new: no event
	}

	a.recordTransitions(prev, curr, now)

	events := a.history.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	e := events[0]
	if e.Unit != "nginx.service" || e.From != "active" || e.To != "failed" {
		t.Errorf("wrong event: %+v", e)
	}
	if !e.Time.Equal(now) {
		t.Errorf("timestamp = %v, want %v", e.Time, now)
	}
}

func TestRecordTransitionsEmpty(t *testing.T) {
	a := newTestAgent()
	a.recordTransitions(nil, []unit.Unit{{Name: "x", ActiveState: "active"}}, time.Now())
	if len(a.history.Events()) != 0 {
		t.Error("first sighting should not produce events")
	}
}
