// PROTOTYPE: disposable ProbeRTT model, not a congestion controller.
// Algorithm translations: see SOURCES.md and THIRD_PARTY_NOTICES.
package main

import "fmt"

const packetBytes = 1460 // Synthetic full-sized packet, not a QUIC MTU recommendation.
// One packet/ms = 1,460,000 bytes/s = 11.68 Mbit/s, held constant.
const bytesPerMS = packetBytes

// All times are integer milliseconds. The reducer has no I/O or wall clock.
type event struct {
	now, rtt, flight, deliveredAtSend, delivered, idleDuration int
	downExit                                                   bool
}
type controller struct {
	variant                                          string
	minRTT, minStamp, probeMin, probeStamp, savedCap int
	probing, idleRestart, roundDone                  bool
	deadline, roundBoundary, entered                 int
	entries, exits, probeMS                          int
}

func (c controller) target() int {
	cap := bytesPerMS * c.minRTT / 2
	if c.variant == "proposal" {
		cap = min(cap, c.savedCap)
	}
	return max(4*packetBytes, cap)
}
func (c controller) step(e event) (controller, string) {
	note := ""
	if e.idleDuration > 0 {
		if c.variant == "quiche" {
			c.minStamp += e.idleDuration
		} else {
			c.idleRestart = true
		}
		return c, "idle restart"
	}
	expired := false
	switch c.variant {
	case "draft06":
		expired = e.now > c.probeStamp+5000
		if e.rtt < c.probeMin || expired {
			c.probeMin, c.probeStamp = e.rtt, e.now
		}
		if c.probeMin < c.minRTT || e.now > c.minStamp+10000 {
			c.minRTT, c.minStamp = c.probeMin, c.probeStamp
		}
	case "proposal":
		expired = e.now > c.minStamp+10000
		if e.rtt <= c.minRTT || expired {
			if expired {
				c.savedCap = bytesPerMS * c.minRTT / 2
			}
			c.minRTT, c.minStamp = e.rtt, e.now
		}
	case "quiche":
		if e.rtt < c.minRTT {
			c.minRTT, c.minStamp = e.rtt, e.now
		}
		expired = !c.probing && e.downExit && e.now >= c.minStamp+10000
		if expired {
			c.minRTT, c.minStamp = e.rtt, e.now
		}
	}
	if !c.probing && expired && !c.idleRestart {
		c.probing = true
		c.entered = e.now
		c.entries++
		c.deadline = 0
		c.roundDone = false
		note = "enter"
	}
	if c.probing {
		if c.deadline == 0 && e.flight <= c.target() {
			c.deadline = e.now + 200
			c.roundBoundary = e.delivered
			c.roundDone = false
			note += " timer-start"
		} else if c.deadline != 0 {
			// Draft rounds use delivered-at-send >= the delivered boundary at StartRound.
			if e.deliveredAtSend >= c.roundBoundary {
				c.roundDone = true
			}
			if e.now > c.deadline && (c.variant == "quiche" || c.roundDone) {
				c.probing = false
				c.exits++
				c.probeMS += e.now - c.entered
				switch c.variant {
				case "draft06":
					c.probeStamp = e.now
				case "proposal":
					c.minStamp = e.now
				}
				note += " exit"
			}
		}
	}
	if c.idleRestart {
		note += " idle-entry-suppressed"
	}
	c.idleRestart = false // Every supplied event newly delivers at least one packet.
	return c, note
}

type scenario struct {
	name, description                                                                 string
	start, end, seedRTT, seedStamp, probeSeed, probeStamp, baseRTT, burst, queueDelay int
	riseAt, riseTo, feedbackDelay, downPeriod, idleFrom, idleTo                       int
}

func scenarios() []scenario {
	base := scenario{start: 1000, end: 15000, seedRTT: 101, seedStamp: 1000, probeSeed: 101, probeStamp: 1000, baseRTT: 100, burst: 400, downPeriod: 1000}
	makeCase := func(name, description string) scenario {
		s := base
		s.name = name
		s.description = description
		return s
	}
	a := makeCase("ordinary-queue", "Fresh 101ms seed; initial 400-packet burst; persistent application backlog.")
	b := makeCase("expired-queue", "Boundary snapshot: aged filters, 400ms preloaded queue, 400-packet burst, persistent backlog and no idle.")
	b.start = 12000
	b.end = 16000
	b.probeStamp = 6000
	b.probeSeed = 501
	b.queueDelay = 400
	c := makeCase("rising-base", "Base RTT rises from 100 to 400ms at 2s; all other inputs unchanged.")
	c.riseAt = 2000
	c.riseTo = 400
	c.end = 25000
	d := makeCase("long-rtt", "Base RTT 400ms, fresh 401ms minimum; initial 400-packet burst.")
	d.baseRTT = 400
	d.seedRTT = 401
	d.probeSeed = 401
	d.end = 16000
	e := d
	e.name = "delayed-feedback"
	e.description = "Long-RTT case plus 150ms receiver/return-path feedback delay; no ACK-delay subtraction."
	e.feedbackDelay = 150
	f := makeCase("idle-restart", "Send supply stops at 2s, resumes at 14s only after all flight drains.")
	f.idleFrom = 2000
	f.idleTo = 14000
	f.end = 18000
	g := b
	g.name = "late-down-exit"
	g.description = "Expired-queue snapshot, but QUICHE Down-exit opportunities every 5s instead of 1s."
	g.downPeriod = 5000
	g.end = 16000
	h := b
	h.name = "falling-base"
	h.description = "Expired snapshot with old 401ms minimum; current base RTT 100ms, showing saved cap can shrink."
	h.seedRTT = 401
	h.probeSeed = 501
	i := b
	i.name = "expired-no-backlog"
	i.description = "Expired-queue snapshot with zero preloaded background queue; all other inputs unchanged."
	i.queueDelay = 0
	return []scenario{a, b, i, c, d, e, f, g, h}
}

type packet struct{ sent, ack, deliveredAtSend int }
type row struct {
	scenario, variant, state, note                                           string
	now, base, rtt, estimate, target, flight, queue, deadline, roundBoundary int
	minStamp, probeMin, probeStamp, savedCap                                 int
	roundDone                                                                bool
	entries, exits, duration                                                 int
}

// simulate is an isolated packet/ACK harness. It does not model the other BBR states.
func simulate(s scenario, variant string) []row {
	c := controller{variant: variant, minRTT: s.seedRTT, minStamp: s.seedStamp, probeMin: s.probeSeed, probeStamp: s.probeStamp, savedCap: bytesPerMS * s.seedRTT / 2}
	var pending []packet
	delivered, lastDeparture, lastDown := 0, s.start+s.queueDelay, s.start/s.downPeriod*s.downPeriod
	flight, cwnd := 0, max(s.burst*packetBytes, 2*bytesPerMS*s.seedRTT)
	normalCwnd := cwnd
	idleStart, lastRTT := 0, 0
	var rows []row
	appendRow := func(now, base int, note string) {
		state := "ProbeBW"
		duration := c.probeMS
		if c.probing {
			state = "ProbeRTT"
			duration += now - c.entered
		}
		rows = append(rows, row{s.name, variant, state, note, now, base, lastRTT, c.minRTT, c.target(), flight, max(0, lastDeparture-now) * packetBytes, c.deadline, c.roundBoundary, c.minStamp, c.probeMin, c.probeStamp, c.savedCap, c.roundDone, c.entries, c.exits, duration})
	}
	send := func(now, base int) {
		lastDeparture = max(now, lastDeparture) + 1
		pending = append(pending, packet{now, lastDeparture + base + s.feedbackDelay, delivered})
		flight += packetBytes
	}
	// Explicit initial burst is synthetic queued traffic. None is marked idle/app-limited.
	for i := 0; i < s.burst; i++ {
		send(s.start, s.baseRTT)
	}
	appendRow(s.start, s.baseRTT, "initial")
	for now := s.start + 1; now <= s.end; now++ {
		base := s.baseRTT
		if s.riseAt > 0 && now >= s.riseAt {
			base = s.riseTo
		}
		note := ""
		// Every packet has one immediate ACK; scan handles reordering after RTT changes.
		remaining := pending[:0]
		for _, p := range pending {
			if p.ack > now {
				remaining = append(remaining, p)
				continue
			}
			flight -= packetBytes
			delivered += packetBytes
			lastRTT = now - p.sent
			dueDown := now >= lastDown+s.downPeriod
			if dueDown {
				lastDown = now / s.downPeriod * s.downPeriod
			}
			before := c
			var n string
			c, n = c.step(event{now: now, rtt: lastRTT, flight: flight, deliveredAtSend: p.deliveredAtSend, delivered: delivered, downExit: dueDown})
			if n != "" {
				note += n + "; "
			}
			if before.minRTT != c.minRTT && note == "" {
				note = "minimum changed; "
			}
			if before.probing && !c.probing && variant != "quiche" {
				cwnd = normalCwnd
			}
			if c.probing {
				cwnd = min(cwnd, c.target())
			} else {
				cwnd = min(cwnd+packetBytes, normalCwnd)
			}
		}
		pending = remaining
		idle := s.idleFrom > 0 && now >= s.idleFrom && now < s.idleTo
		if idle && flight == 0 && idleStart == 0 {
			idleStart = now
		}
		if !idle && idleStart > 0 {
			c, _ = c.step(event{idleDuration: now - idleStart, now: now})
			note += "idle restart; "
			idleStart = 0
		}
		// Send at B, at most one packet/ms, without accumulating pacing credit.
		if !idle && flight+packetBytes <= cwnd {
			send(now, base)
		}
		if note != "" || (now-s.start)%100 == 0 || now == s.end {
			appendRow(now, base, note)
		}
	}
	return rows
}
func summary(rows []row) string {
	last := rows[len(rows)-1]
	return fmt.Sprintf("%-18s %-8s entries=%d exits=%d probe=%4dms final min=%3dms (base=%3dms) queue=%6dB", last.scenario, last.variant, last.entries, last.exits, last.duration, last.estimate, last.base, last.queue)
}
