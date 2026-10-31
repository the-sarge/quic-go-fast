package model

import (
	"reflect"
	"testing"
	"time"
)

func TestGridCastFixedProfiles(t *testing.T) {
	for _, id := range []string{"gridcast-loss", "gridcast-coexistence"} {
		for _, forward := range []bool{true, false} {
			q, err := Scenario(id, forward, 0, 1701, 1460)
			if err != nil {
				t.Fatal(err)
			}
			delay, loss := 50*time.Millisecond, 0.0
			if id == "gridcast-loss" {
				delay = 40 * time.Millisecond
				if forward {
					loss = .001
				}
			}
			if q.Direction.Rate != 100000000 || q.Direction.Capacity != 1251220 || q.Direction.Delay != delay || q.Direction.Loss != loss || q.Direction.Mark || q.Schedule.Competitors || len(q.Schedule.Changes) != 0 {
				t.Fatalf("profile drift: %+v %+v", q.Direction, q.Schedule)
			}
			// Focal and competitor share serialization and capacity even during old L6's transition.
			at := 150 * time.Second
			if !q.Admit(at, Packet{Bytes: make([]byte, 1460)}) || !q.Admit(at, Packet{Bytes: make([]byte, 1460), Competitor: true}) {
				t.Fatal("unexpected seeded drop")
			}
			first := q.Advance(at + delay + 116800*time.Nanosecond)
			second := q.Advance(at + delay + 233600*time.Nanosecond)
			if len(first) != 1 || first[0].Competitor || len(second) != 1 || !second[0].Competitor {
				t.Fatal("not one fixed FIFO")
			}
		}
	}
	q, _ := Scenario("gridcast-coexistence", true, 0, 17, 1460)
	for i := 0; i < 857; i++ {
		if !q.Admit(0, Packet{Bytes: make([]byte, 1460), Competitor: i%2 == 0}) {
			t.Fatal("early overflow")
		}
	}
	if q.Admit(0, Packet{Bytes: make([]byte, 1), Competitor: true}) || q.Stats.Overflow != 1 || q.Stats.MaxQueueBytes != 1251220 {
		t.Fatal("capacity must count both flows' IP bytes")
	}
}

func TestGridCastLossRepeatableAndHistoricalRowsUnchanged(t *testing.T) {
	var receipts []Stats
	for k := 0; k < 2; k++ {
		q, _ := Scenario("gridcast-loss", true, 0, 1701, 1460)
		for i := 0; i < 100000; i++ {
			at := time.Duration(i) * time.Millisecond
			q.Advance(at)
			q.Admit(at, Packet{Bytes: make([]byte, 100)})
		}
		receipts = append(receipts, q.Stats)
	}
	if !reflect.DeepEqual(receipts[0], receipts[1]) || receipts[0].RandomDrop == 0 || receipts[0].Overflow != 0 {
		t.Fatalf("seed parity: %+v", receipts)
	}
	s, _ := Scenario("S6", true, 0, 1701, 1460)
	l, _ := Scenario("L6", true, 0, 1701, 1460)
	if s.Direction.Delay != 50*time.Millisecond || s.Direction.Loss != .001 || !l.Schedule.Competitors {
		t.Fatal("historical row changed")
	}
	if _, err := Scenario("gridcast-loss", true, 3, 17, 1460); err == nil {
		t.Fatal("accepted changing phase")
	}
}
