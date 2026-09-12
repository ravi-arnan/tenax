package history

import (
	"sync"
	"testing"
	"time"
)

func TestEventRing(t *testing.T) {
	s := NewStore(3, 5)

	for i := 0; i < 5; i++ {
		s.PushEvent(Event{Unit: string(rune('a' + i)), From: "active", To: "inactive"})
	}

	events := s.Events()
	if len(events) != 3 {
		t.Fatalf("len = %d, want 3", len(events))
	}
	if events[0].Unit != "c" || events[2].Unit != "e" {
		t.Errorf("order wrong: %+v", events)
	}
}

func TestSampleRing(t *testing.T) {
	s := NewStore(2, 4)

	now := time.Now()
	for i := 0; i < 6; i++ {
		s.PushSample(Sample{Time: now, CPUPct: float64(i)})
	}

	samples := s.Samples()
	if len(samples) != 4 {
		t.Fatalf("len = %d, want 4", len(samples))
	}
	if samples[0].CPUPct != 2 || samples[3].CPUPct != 5 {
		t.Errorf("order wrong: first=%v last=%v", samples[0], samples[3])
	}
}

func TestEmptyStore(t *testing.T) {
	s := NewStore(10, 10)
	if len(s.Events()) != 0 || len(s.Samples()) != 0 {
		t.Fatal("expected empty")
	}
}

func TestConcurrentPush(t *testing.T) {
	s := NewStore(64, 64)

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s.PushEvent(Event{Unit: "x"})
				s.PushSample(Sample{})
				s.Events()
				s.Samples()
			}
		}()
	}
	wg.Wait()
}
