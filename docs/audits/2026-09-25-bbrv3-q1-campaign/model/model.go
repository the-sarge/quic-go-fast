// Package model is the frozen Q1 packet-path model, not a maintained emulator.
// It accepts complete, canonical IPv4 packets supplied by the native adapter.
package model

import (
	"container/heap"
	"math"
	"math/rand/v2"
	"time"
)

type Packet struct {
	ScheduledAt time.Duration // Scheduled propagation departure, set only on delivery.
	Bytes       []byte
	ECN         uint8
	Competitor  bool
}
type Direction struct {
	Rate     int64
	Capacity int
	Delay    time.Duration
	Mark     bool
	Loss     float64
	Seed     uint64
}
type RateChange struct {
	At    time.Duration
	Rate  int64
	Delay time.Duration
}
type Schedule struct {
	Kind        string
	Phase       time.Duration
	Changes     []RateChange
	Competitors bool
}
type Stats struct {
	Received, Admitted, Delivered, Marked, EarlyDrop, Overflow, RandomDrop, EventDrop, InterruptionDrop uint64
	MaxQueueBytes                                                                                       int
	MaxPropagationBytes                                                                                 int
	DeliveredBytes                                                                                      uint64
	PropagationOverflow                                                                                 uint64
}
type queued struct {
	Packet
	Finish, Due time.Duration
}
type pending []queued

func (p pending) Len() int           { return len(p) }
func (p pending) Less(i, j int) bool { return p[i].Due < p[j].Due }
func (p pending) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p *pending) Push(x any)        { *p = append(*p, x.(queued)) }
func (p *pending) Pop() any {
	a := *p
	x := a[len(a)-1]
	a[len(a)-1] = queued{}
	*p = a[:len(a)-1]
	return x
}

type Queue struct {
	PropagationLimit int
	Direction        Direction
	Schedule         Schedule
	Stats            Stats
	fifo             []queued
	pending          pending
	bytes, propBytes int
	lastFinish       time.Duration
	interrupted      bool
	rng              *rand.Rand
}

func New(d Direction, s Schedule) *Queue {
	rate, delay := d.Rate, d.Delay
	for _, c := range s.Changes {
		rate = max(rate, c.Rate)
		delay = max(delay, c.Delay)
	}
	if s.Kind == "L1" {
		delay += 200 * time.Millisecond
	}
	if s.Competitors {
		delay = max(delay, 50*time.Millisecond)
	}
	limit := int(math.Ceil(float64(rate)*delay.Seconds()/8)) + d.Capacity + 2*1460
	return &Queue{PropagationLimit: limit, Direction: d, Schedule: s, lastFinish: -time.Hour, rng: rand.New(rand.NewPCG(d.Seed, d.Seed^0x517cc1b727220a95))}
}
func (q *Queue) parameters(at time.Duration) (int64, time.Duration) {
	r, d := q.Direction.Rate, q.Direction.Delay
	for _, c := range q.Schedule.Changes {
		if at < c.At {
			break
		}
		r, d = c.Rate, c.Delay
	}
	return r, d
}
func (q *Queue) finish(at time.Duration, bytes int) time.Duration {
	remaining := float64(bytes) * 8
	for {
		rate, _ := q.parameters(at)
		next := time.Duration(math.MaxInt64)
		for _, c := range q.Schedule.Changes {
			if c.At > at {
				next = c.At
				break
			}
		}
		duration := time.Duration(math.Ceil(remaining * float64(time.Second) / float64(rate)))
		if duration <= next-at {
			return at + duration
		}
		remaining -= float64(next-at) / float64(time.Second) * float64(rate)
		at = next
	}
}
func inEvent(at, phase time.Duration) bool {
	return at >= phase && (at-phase)%(15*time.Second) < 50*time.Millisecond
}

// Admit observes the byte occupancy after admitting this IP packet. At S8's
// threshold, only ECT(0) is changed to CE; Not-ECT gets equivalent early drop.
// Call Advance(at) before Admit(at) and consume its returned deliveries.
func (q *Queue) Admit(at time.Duration, p Packet) bool {
	q.Stats.Received++
	if q.Schedule.Kind == "L3" && at >= 210*time.Second && at < 210*time.Second+500*time.Millisecond {
		q.Stats.InterruptionDrop++
		return false
	}
	if q.Schedule.Kind == "L2" && inEvent(at, q.Schedule.Phase) {
		q.Stats.EventDrop++
		return false
	}
	if q.Direction.Loss > 0 && q.rng.Float64() < q.Direction.Loss {
		q.Stats.RandomDrop++
		return false
	}
	n := len(p.Bytes)
	if q.bytes+n > q.Direction.Capacity {
		q.Stats.Overflow++
		return false
	}
	if q.Direction.Mark && q.bytes+n > q.Direction.Capacity/4 {
		if p.ECN == 0 {
			q.Stats.EarlyDrop++
			return false
		}
		if p.ECN == 2 {
			p.ECN = 3
			q.Stats.Marked++
		}
	}
	finish := q.finish(max(at, q.lastFinish), n)
	q.lastFinish = finish
	_, delay := q.parameters(finish)
	if p.Competitor && q.Schedule.Competitors {
		delay = 50 * time.Millisecond
		if finish >= 100*time.Second && finish < 200*time.Second {
			delay = 12500 * time.Microsecond
		}
	}
	if q.Schedule.Kind == "L1" && inEvent(finish, q.Schedule.Phase) {
		delay += 200 * time.Millisecond
	}
	q.fifo = append(q.fifo, queued{p, finish, finish + delay})
	q.bytes += n
	q.Stats.MaxQueueBytes = max(q.Stats.MaxQueueBytes, q.bytes)
	q.Stats.Admitted++
	return true
}
func (q *Queue) advance(at time.Duration) []Packet {
	var out []Packet
	for {
		// Service and propagation are different queues. Interleave their events so
		// a late scheduler wake cannot inflate pending storage with past departures.
		service := time.Duration(math.MaxInt64)
		delivery := time.Duration(math.MaxInt64)
		if len(q.fifo) > 0 {
			service = q.fifo[0].Finish
		}
		if len(q.pending) > 0 {
			delivery = q.pending[0].Due
		}
		if min(service, delivery) > at {
			break
		}
		if delivery <= service {
			x := heap.Pop(&q.pending).(queued)
			q.propBytes -= len(x.Bytes)
			q.Stats.Delivered++
			q.Stats.DeliveredBytes += uint64(len(x.Bytes))
			x.Packet.ScheduledAt = x.Due
			out = append(out, x.Packet)
		} else {
			x := q.fifo[0]
			q.fifo[0] = queued{}
			q.fifo = q.fifo[1:]
			q.bytes -= len(x.Bytes)
			if q.propBytes+len(x.Bytes) > q.PropagationLimit {
				q.Stats.PropagationOverflow++
				continue
			}
			q.propBytes += len(x.Bytes)
			heap.Push(&q.pending, x)
			q.Stats.MaxPropagationBytes = max(q.Stats.MaxPropagationBytes, q.propBytes)
		}
	}
	return out
}
func (q *Queue) Advance(at time.Duration) []Packet {
	var out []Packet
	if q.Schedule.Kind == "L3" && !q.interrupted && at >= 210*time.Second {
		out = q.advance(210*time.Second - time.Nanosecond)
		q.Stats.InterruptionDrop += uint64(len(q.fifo) + len(q.pending))
		q.fifo = nil
		q.pending = nil
		q.bytes = 0
		q.propBytes = 0
		q.lastFinish = 210*time.Second + 500*time.Millisecond
		q.interrupted = true
	}
	return append(out, q.advance(at)...)
}
