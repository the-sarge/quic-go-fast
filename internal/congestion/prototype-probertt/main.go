// PROTOTYPE: disposable terminal viewer; see README.md.
package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	mode := flag.String("mode", "interactive", "interactive, summary, events, or csv")
	name := flag.String("scenario", "ordinary-queue", "scenario name, or all for batch output")
	flag.Parse()
	cases := scenarios()
	var selected []scenario
	for _, s := range cases {
		if s.name == *name || *name == "all" {
			selected = append(selected, s)
		}
	}
	if len(selected) == 0 {
		panic("unknown scenario")
	}
	if *mode == "interactive" {
		interactive(selected[0])
		return
	}
	var w *csv.Writer
	if *mode == "csv" {
		w = csv.NewWriter(os.Stdout)
		defer w.Flush()
		_ = w.Write(strings.Split("scenario,variant,time_ms,state,base_rtt_ms,sample_rtt_ms,min_rtt_ms,probe_cap_bytes,flight_bytes,queue_bytes,deadline_ms,round_boundary_bytes,min_stamp_ms,probe_min_ms,probe_stamp_ms,saved_cap_bytes,round_done,probe_total_ms,event", ","))
	}
	for _, s := range selected {
		for _, v := range []string{"draft06", "proposal", "quiche"} {
			rows := simulate(s, v)
			if *mode == "summary" {
				fmt.Println(summary(rows))
				continue
			}
			for _, r := range rows {
				r.note = strings.TrimSpace(r.note)
				if *mode == "csv" {
					_ = w.Write([]string{r.scenario, r.variant, itoa(r.now), r.state, itoa(r.base), itoa(r.rtt), itoa(r.estimate), itoa(r.target), itoa(r.flight), itoa(r.queue), itoa(r.deadline), itoa(r.roundBoundary), itoa(r.minStamp), itoa(r.probeMin), itoa(r.probeStamp), itoa(r.savedCap), strconv.FormatBool(r.roundDone), itoa(r.duration), r.note})
				} else if strings.Contains(r.note, "enter") || strings.Contains(r.note, "exit") || strings.Contains(r.note, "timer") || strings.Contains(r.note, "idle") {
					fmt.Printf("%-18s %-8s t=%5d %-8s min=%3d cap=%6d flight=%6d queue=%6d round=%t %s\n", r.scenario, r.variant, r.now, r.state, r.estimate, r.target, r.flight, r.queue, r.roundDone, r.note)
				}
			}
		}
	}
}
func itoa(n int) string { return strconv.Itoa(n) }
func interactive(s scenario) {
	variants := []string{"draft06", "proposal", "quiche"}
	traces := make([][]row, 3)
	times := map[int]bool{}
	for i, v := range variants {
		traces[i] = simulate(s, v)
		for _, r := range traces[i] {
			times[r.now] = true
		}
	}
	now := s.start
	reader := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\033[2J\033[H")
		fmt.Printf("PROTOTYPE — %s\n%s\nTime: %dms | fixed 11.68 Mbit/s | packet 1460B\n\n", s.name, s.description, now)
		for i, trace := range traces {
			r := trace[0]
			for _, next := range trace {
				if next.now > now {
					break
				}
				r = next
			}
			fmt.Printf("\033[1m%s\033[0m  %s (snapshot %dms)\n  base/sample/min RTT: %d/%d/%dms  cap: %dB\n  flight: %dB  bottleneck queue: %dB  deadline: %dms\n  round boundary: %dB  complete: %t  ProbeRTT time: %dms\n  min stamp: %dms  probe min/stamp: %d/%dms  saved cap: %dB\n  event: %s\n", variants[i], r.state, r.now, r.base, r.rtt, r.estimate, r.target, r.flight, r.queue, r.deadline, r.roundBoundary, r.roundDone, r.duration, r.minStamp, r.probeMin, r.probeStamp, r.savedCap, r.note)
		}
		fmt.Print("\n[n] next recorded event  [s] +1 second  [r] reset  [q] quit (then Enter)\n> ")
		if !reader.Scan() {
			return
		}
		switch strings.TrimSpace(reader.Text()) {
		case "q":
			return
		case "r":
			now = s.start
		case "s":
			now = min(now+1000, s.end)
		default:
			for now < s.end {
				now++
				if times[now] {
					break
				}
			}
		}
	}
}
