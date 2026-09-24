// Disposable planning model. No production controller or transport integration.
// Sampling translation: see evidence report and THIRD_PARTY_NOTICES.
package main

import (
	"fmt"
	"math"
	"sort"
)

// Times are integer microseconds, lengths are transport bytes. The path carries
// 1200 bytes/ms (9.6 Mbit/s); host serialization is 100 times faster.
const size = 1200

type scenario struct {
	Name                                                string
	Segments, Drain, Period, Delay, Compress, HostDelay int
	Stall, HostStall, Idle, Partial, Fatal              bool
}

func scenarios() []scenario {
	base := scenario{Name: "ordinary", Segments: 1, Drain: 8, Period: 100}
	out := []scenario{base}
	add := func(name string, change func(*scenario)) { c := base; c.Name = name; change(&c); out = append(out, c) }
	add("delay-5ms", func(c *scenario) { c.Delay = 5000 })
	add("delay-40ms", func(c *scenario) { c.Delay = 40000 })
	add("gso-4", func(c *scenario) { c.Segments = 4 })
	add("gso-16", func(c *scenario) { c.Segments = 16 })
	add("drain-one", func(c *scenario) { c.Period = 4000; c.Drain = 1 })
	add("drain-eight", func(c *scenario) { c.Period = 4000 })
	add("worker-stall", func(c *scenario) { c.Stall = true })
	add("worker-stall-gso4", func(c *scenario) { c.Stall = true; c.Segments = 4 })
	add("worker-stall-gso16", func(c *scenario) { c.Stall = true; c.Segments = 16 })
	add("ack-compress-20ms", func(c *scenario) { c.Compress = 20000 })
	add("ack-compress-80ms", func(c *scenario) { c.Compress = 80000 })
	add("stall-compress-gso", func(c *scenario) { c.Stall = true; c.Compress = 80000; c.Segments = 16 })
	add("host-delay", func(c *scenario) { c.HostDelay = 10000 })
	add("host-stall-compress", func(c *scenario) { c.HostStall = true; c.Compress = 80000 })
	add("idle-restart", func(c *scenario) { c.Idle = true })
	add("partial-size-error", func(c *scenario) { c.Partial = true; c.Period = 4000 })
	add("unknown-progress", func(c *scenario) { c.Fatal = true; c.Period = 4000 })
	return out
}

type packet struct {
	ID, Datagram, Entry, Bytes                int
	Reg, Submit, Depart, Receive, Ack, Retire int
	Outcome                                   string
}
type entry struct {
	IDs   []int
	Ready int
}
type event struct{ Time, Kind, ID int } // ACK, register, submit, depart, retire, idle
type snapshot struct {
	Sent, First, DeliveredAt, Delivered int
	Limited                             bool
}
type sample struct {
	Time, Packet, Bytes, SendElapsed, AckElapsed, Interval, RTT int
	Rate                                                        float64
	Limited, Valid                                              bool
}
type sampler struct {
	Delivered, DeliveredAt, First, Marker, Peak, MinRTT int
	Live                                                map[int]snapshot
}

func newSampler() sampler { return sampler{Live: make(map[int]snapshot)} }
func (s *sampler) sent(p packet, now int) {
	if len(s.Live) == 0 {
		s.First, s.DeliveredAt = now, now
	}
	s.Live[p.ID] = snapshot{now, s.First, s.DeliveredAt, s.Delivered, s.Marker != 0}
	s.Peak = max(s.Peak, len(s.Live))
}
func (s *sampler) idle() {
	// Called only at a known real idle boundary, with no unsent host work.
	s.Marker = max(s.Delivered+len(s.Live)*size, 1)
}
func (s *sampler) ack(p packet, now int) sample {
	x, ok := s.Live[p.ID]
	if !ok {
		return sample{}
	}
	delete(s.Live, p.ID)
	s.Delivered += p.Bytes
	s.DeliveredAt = now
	s.First = x.Sent
	r := sample{Time: now, Packet: p.ID, Bytes: s.Delivered - x.Delivered, SendElapsed: x.Sent - x.First, AckElapsed: now - x.DeliveredAt, RTT: now - x.Sent, Limited: x.Limited}
	r.Interval = max(r.SendElapsed, r.AckElapsed)
	if s.MinRTT == 0 || r.RTT < s.MinRTT {
		s.MinRTT = r.RTT
	}
	r.Valid = r.Interval > 0 && r.Interval >= s.MinRTT
	if r.Valid {
		r.Rate = float64(r.Bytes) * 1e6 / float64(r.Interval)
	}
	if s.Marker != 0 && s.Delivered > s.Marker {
		s.Marker = 0
	}
	return r
}

type frame struct {
	Time                                               int
	Event                                              string
	Packet                                             int
	Delivered, Outstanding, First, DeliveredAt, Marker [3]int
	Samples                                            [3]sample
}
type result struct {
	HostBurst, Duration                                                   int
	Fault                                                                 bool
	Scenario                                                              scenario
	Quantum                                                               int
	Packets                                                               []packet
	Frames                                                                []frame
	QueuePeak, QueueStops, QuantumStops, Burst, EntryPeak, LocalBytesPeak int
	Peak                                                                  [3]int
	Final                                                                 [3]int
	Delivered                                                             [3]int
}

// simulate is a pure open-loop experiment. The packet schedule does not depend
// on sample estimates: changing a sampler cannot silently change its input.
func simulate(c scenario, quantum int, credit bool) result {
	r := result{Scenario: c, Quantum: quantum}
	queue := []entry{}
	next, datagrams, entryID := 1000, 0, 0
	nicEnd, pathEnd := 0, 0
	done, faulted := false, false
	submissionBytes := map[int]int{}
	accept := func(ids []int, t int) {
		for _, id := range ids {
			p := &r.Packets[id]
			p.Submit, p.Outcome = t, "accepted"
			depart := max(t+c.HostDelay, nicEnd)
			if c.HostStall && depart >= 401000 && depart < 481000 {
				depart = 481000
			}
			p.Depart = depart
			nicEnd = depart + p.Bytes*10/size
			// Departure is start of host transmission. Store-and-forward
			// bottleneck starts after host serialization; fixed propagation
			// is 20ms each way, independent of queues and ACK compression.
			pathEnd = max(nicEnd, pathEnd) + p.Bytes*1000/size
			p.Receive = pathEnd + 20000
			p.Ack = p.Receive + 20000
			if c.Compress > 0 {
				p.Ack = ((p.Ack + c.Compress - 1) / c.Compress) * c.Compress
			}
			submissionBytes[t] += p.Bytes
		}
	}
	// Fixed time steps affect producer/worker wakeups only. Departure and ACK
	// times are exact integer events and are replayed separately below.
	for t := 1000; t < 60000000; t += 100 {
		if !done && datagrams < 1024 && t >= next {
			idle := c.Idle && t >= 501000 && t < 701000
			if !idle {
				pending := 0
				for _, e := range queue {
					pending += len(e.IDs) * size
				}
				budget := quantum * size
				if credit {
					budget -= pending
				}
				if len(queue) == 8 {
					r.QueueStops++
				} else if budget < size {
					r.QuantumStops++
				} else {
					n := min(quantum, budget/size, 1024-datagrams)
					emitted := 0
					for emitted < n && len(queue) < 8 {
						count := min(c.Segments, n-emitted)
						e := entry{Ready: t + c.Delay}
						for range count {
							{
								id := len(r.Packets)
								r.Packets = append(r.Packets, packet{ID: id, Datagram: datagrams, Entry: entryID, Bytes: size, Reg: t, Submit: -1, Depart: -1, Receive: -1, Ack: -1, Retire: -1, Outcome: "queued"})
								e.IDs = append(e.IDs, id)
							}
							datagrams++
						}
						entryID++
						queue = append(queue, e)
						emitted += count
						r.EntryPeak = max(r.EntryPeak, count*size)
					}
					next = t + emitted*1000 // no catch-up tokens after a stall
				}
			}
		}
		r.QueuePeak = max(r.QueuePeak, len(queue))
		pending := 0
		for _, e := range queue {
			pending += len(e.IDs) * size
		}
		r.LocalBytesPeak = max(r.LocalBytesPeak, pending)
		if (t-1000)%c.Period == 0 && !(c.Stall && t >= 401000 && t < 481000) {
			n := 0
			for n < min(c.Drain, len(queue)) && queue[n].Ready <= t {
				n++
			}
			if n > 0 {
				group := append([]entry(nil), queue[:n]...)
				queue = queue[n:]
				if !faulted && t >= 201000 && n >= 2 && (c.Partial || c.Fatal) {
					faulted = true
					accept(group[0].IDs, t) // known prefix for size case; hidden oracle progress for fatal
					for i, e := range group[1:] {
						if c.Fatal || i == 0 {
							for _, id := range e.IDs {
								p := &r.Packets[id]
								p.Submit = t
								p.Outcome = "local-size-rejection"
								p.Retire = t + 200000 // supplied recovery disposal, not BBR loss
								if c.Fatal {
									p.Outcome = "unknown-progress"
									p.Retire = t + 1
								}
							}
						} else {
							accept(e.IDs, t)
						}
					}
					if c.Fatal {
						done = true
						for i := range r.Packets {
							if r.Packets[i].Ack > t || r.Packets[i].Ack < 0 {
								r.Packets[i].Retire = t + 1
							}
						}
						queue = nil
					}
				} else {
					for _, e := range group {
						accept(e.IDs, t)
					}
				}
			}
		}
		if (datagrams == 1024 || done) && len(queue) == 0 {
			break
		}
	}
	if !done && (datagrams != 1024 || len(queue) != 0) {
		panic("model horizon exhausted")
	}
	r.Fault = faulted
	burst, priorEnd := 0, -1
	for _, p := range r.Packets {
		if p.Depart < 0 {
			continue
		}
		if p.Depart != priorEnd {
			burst = 0
		}
		burst += p.Bytes
		r.HostBurst = max(r.HostBurst, burst)
		priorEnd = p.Depart + p.Bytes*10/size
		r.Duration = max(r.Duration, p.Ack)
	}
	for _, b := range submissionBytes {
		r.Burst = max(r.Burst, b)
	}
	events := []event{}
	for _, p := range r.Packets {
		events = append(events, event{p.Reg, 1, p.ID})
		if p.Depart >= 0 {
			events = append(events, event{p.Submit, 2, p.ID}, event{p.Depart, 3, p.ID}, event{p.Ack, 0, p.ID})
		}
		if p.Retire >= 0 {
			events = append(events, event{p.Retire, 4, p.ID})
		}
	}
	if c.Idle {
		events = append(events, event{601000, 5, 0})
	}
	sort.SliceStable(events, func(i, j int) bool {
		a, b := events[i], events[j]
		if a.Time != b.Time {
			return a.Time < b.Time
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.ID < b.ID
	})
	s := [3]sampler{newSampler(), newSampler(), newSampler()}
	closed := false
	for _, e := range events {
		p := r.Packets[e.ID]
		f := frame{Time: e.Time, Packet: e.ID, Event: []string{"ack", "register", "accept", "depart", "dispose", "idle"}[e.Kind]}
		switch e.Kind {
		case 0:
			if !closed {
				for i := range s {
					f.Samples[i] = s[i].ack(p, e.Time)
				}
			}
		case 1, 2, 3:
			if !closed {
				s[e.Kind-1].sent(p, e.Time)
			}
		case 4:
			for i := range s {
				delete(s[i].Live, p.ID)
			}
			if c.Fatal {
				closed = true
			}
		case 5:
			for i := range s {
				s[i].idle()
			}
		}
		for i := range s {
			f.Delivered[i], f.Outstanding[i], f.First[i], f.DeliveredAt[i], f.Marker[i] = s[i].Delivered, len(s[i].Live), s[i].First, s[i].DeliveredAt, s[i].Marker
		}
		r.Frames = append(r.Frames, f)
	}
	for i := range s {
		r.Peak[i], r.Final[i], r.Delivered[i] = s[i].Peak, len(s[i].Live), s[i].Delivered
	}
	return r
}

type metrics struct {
	MaxRate                [3]float64
	MinRTT                 [3]int
	Invalid, Limited       [3]int
	MaxRateGap, MaxRTTBias float64
}

func summarize(r result) metrics {
	m := metrics{}
	for _, f := range r.Frames {
		if f.Event != "ack" {
			continue
		}
		for i, s := range f.Samples {
			if s.Bytes == 0 {
				continue
			}
			if s.Valid {
				m.MaxRate[i] = max(m.MaxRate[i], s.Rate)
			} else {
				m.Invalid[i]++
			}
			if s.Limited {
				m.Limited[i]++
			}
			if m.MinRTT[i] == 0 || s.RTT < m.MinRTT[i] {
				m.MinRTT[i] = s.RTT
			}
		}
		a, b := f.Samples[0], f.Samples[2]
		if a.Valid && b.Valid {
			m.MaxRateGap = max(m.MaxRateGap, math.Abs(a.Rate-b.Rate)/1200000)
		}
		if a.Bytes > 0 && b.Bytes > 0 {
			m.MaxRTTBias = max(m.MaxRTTBias, float64(a.RTT-b.RTT)/1000)
		}
	}
	return m
}
func name(r result, credit bool) string {
	policy := "per-opportunity"
	if credit {
		policy = "queued-credit"
	}
	return fmt.Sprintf("%s/q%d/%s", r.Scenario.Name, r.Quantum, policy)
}
