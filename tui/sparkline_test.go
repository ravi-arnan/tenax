package tui

import (
	"testing"

	"github.com/ravi-arnan/tenax/history"
)

func TestSparkline(t *testing.T) {
	samples := []history.Sample{
		{CPUPct: 0}, {CPUPct: 25}, {CPUPct: 50}, {CPUPct: 100},
	}
	out := sparkline(samples, 10)
	if len([]rune(out)) != 4 {
		t.Fatalf("expected 4 blocks, got %q", out)
	}
	// Ascending CPU should use ascending block characters.
	if out[0] == out[len(out)-1] {
		t.Errorf("expected different blocks for 0%% vs 100%%: %q", out)
	}
}

func TestSparklineEmpty(t *testing.T) {
	if out := sparkline(nil, 10); out != "" {
		t.Errorf("expected empty, got %q", out)
	}
	if out := sparkline([]history.Sample{{CPUPct: 5}}, 10); out != "" {
		t.Errorf("expected empty for single sample, got %q", out)
	}
}

func TestSparklineWidth(t *testing.T) {
	samples := make([]history.Sample, 50)
	for i := range samples {
		samples[i].CPUPct = 50
	}
	out := sparkline(samples, 10)
	if len([]rune(out)) != 10 {
		t.Fatalf("expected width capped at 10, got %d", len([]rune(out)))
	}
}
