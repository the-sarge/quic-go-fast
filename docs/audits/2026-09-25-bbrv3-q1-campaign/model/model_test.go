package model

import (
	"testing"
	"time"
)

func TestDirectionalRateFiniteQueueAndCE(t *testing.T) {
	// Synthetic calibration: 1000 b/s, 200 byte FIFO, 50ms propagation.
	q := New(Direction{Rate: 1000, Capacity: 200, Delay: 50 * time.Millisecond, Mark: true}, Schedule{})
	first := Packet{Bytes: make([]byte, 100), ECN: 2}
	if !q.Admit(0, first) {
		t.Fatal("first packet rejected")
	}
	if q.Admit(0, Packet{Bytes: make([]byte, 60), ECN: 0}) {
		t.Fatal("non-ECT was not early dropped")
	}
	if !q.Admit(0, first) {
		t.Fatal("second ECT packet rejected")
	}
	if q.Admit(0, first) {
		t.Fatal("byte capacity exceeded")
	}
	if got := q.Advance(849 * time.Millisecond); len(got) != 0 {
		t.Fatal("left before serialization plus propagation")
	}
	got := q.Advance(850 * time.Millisecond)
	if len(got) != 1 || got[0].ECN != 3 {
		t.Fatalf("mark / first departure: %+v", got)
	}
	if got = q.Advance(1650 * time.Millisecond); len(got) != 1 || got[0].ECN != 3 {
		t.Fatal("second departure incorrect")
	}
	if q.Stats.EarlyDrop != 1 || q.Stats.Overflow != 1 || q.Stats.Marked != 2 || q.Stats.MaxQueueBytes != 200 {
		t.Fatalf("wrong observation: %+v", q.Stats)
	}
}

func TestChangingRateRetainsFixedQueueAndPartialService(t *testing.T) {
	q := New(Direction{Rate: 8000, Capacity: 500}, Schedule{Changes: []RateChange{{At: 100 * time.Millisecond, Rate: 16000}}})
	q.Admit(0, Packet{Bytes: make([]byte, 300)})
	q.Admit(0, Packet{Bytes: make([]byte, 100)})
	if len(q.Advance(199*time.Millisecond)) != 0 {
		t.Fatal("packet left before partial service completed")
	}
	if len(q.Advance(200*time.Millisecond)) != 1 || len(q.Advance(250*time.Millisecond)) != 1 {
		t.Fatal("rate transition reset or mischarged queue")
	}
	if q.Direction.Capacity != 500 || q.Stats.MaxQueueBytes != 400 {
		t.Fatal("capacity changed")
	}
}
func TestPostServiceDelayCanReorderWithoutPausingService(t *testing.T) {
	q := New(Direction{Rate: 8000, Capacity: 1000, Delay: 10 * time.Millisecond}, Schedule{Kind: "L1", Phase: 3 * time.Second})
	p := make([]byte, 100)
	p[0] = 1
	q.Admit(2900*time.Millisecond, Packet{Bytes: p})
	q.Advance(3 * time.Second)
	other := make([]byte, 100)
	other[0] = 2
	q.Admit(3*time.Second, Packet{Bytes: other})
	early := q.Advance(3110 * time.Millisecond)
	late := q.Advance(3210 * time.Millisecond)
	if len(early) != 1 || len(late) != 1 || early[0].Bytes[0] != 2 || late[0].Bytes[0] != 1 {
		t.Fatal("delay event paused service or failed to reorder")
	}
}
func TestInterruptionFlushesQueueAndPropagation(t *testing.T) {
	q := New(Direction{Rate: 8000, Capacity: 1000, Delay: time.Second}, Schedule{Kind: "L3"})
	q.Admit(209*time.Second, Packet{Bytes: make([]byte, 100)})
	q.Advance(209500 * time.Millisecond) // Already pending propagation.
	q.Admit(209950*time.Millisecond, Packet{Bytes: make([]byte, 100)})
	if len(q.Advance(210*time.Second)) != 0 {
		t.Fatal("packet survived interruption")
	}
	if q.Admit(210200*time.Millisecond, Packet{Bytes: make([]byte, 100)}) {
		t.Fatal("arrival accepted during interruption")
	}
	q.Advance(210500 * time.Millisecond)
	q.Admit(210500*time.Millisecond, Packet{Bytes: make([]byte, 100)})
	if len(q.Advance(211600*time.Millisecond)) != 1 || q.Stats.InterruptionDrop != 3 {
		t.Fatalf("bad flush/restart: %+v", q.Stats)
	}
}

func TestPropagationStorageIsBoundedAndOverflowIsDistinct(t *testing.T) {
	q := New(Direction{Rate: 8000, Capacity: 1000, Delay: time.Second}, Schedule{Kind: "L1"})
	if q.PropagationLimit != 5120 {
		t.Fatalf("bound: %d", q.PropagationLimit)
	}
	// Inject a smaller native storage allowance to distinguish emulator failure
	// from the declared bottleneck's ordinary queue overflow.
	q.PropagationLimit = 100
	q.Admit(0, Packet{Bytes: make([]byte, 100)})
	q.Admit(0, Packet{Bytes: make([]byte, 100)})
	q.Advance(300 * time.Millisecond)
	if q.Stats.PropagationOverflow != 1 || q.Stats.Overflow != 0 || q.Stats.MaxPropagationBytes != 100 {
		t.Fatalf("storage accounting: %+v", q.Stats)
	}
}
func TestOutageArrivalWindowBoundaries(t *testing.T) {
	q := New(Direction{Rate: 8000000, Capacity: 1000}, Schedule{Kind: "L2", Phase: 3 * time.Second})
	for _, tc := range []struct {
		at       time.Duration
		accepted bool
	}{{2999 * time.Millisecond, true}, {3 * time.Second, false}, {3049 * time.Millisecond, false}, {3050 * time.Millisecond, true}, {18 * time.Second, false}} {
		q.Advance(tc.at)
		if q.Admit(tc.at, Packet{Bytes: make([]byte, 100)}) != tc.accepted {
			t.Fatalf("at %s", tc.at)
		}
	}
}
