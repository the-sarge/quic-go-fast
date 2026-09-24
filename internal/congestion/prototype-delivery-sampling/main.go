// Disposable viewer/exporter. Run: go run ./internal/congestion/prototype-delivery-sampling
package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func csvFile(path string) (*os.File, *csv.Writer) {
	f, err := os.Create(path)
	must(err)
	return f, csv.NewWriter(f)
}
func row(w *csv.Writer, values ...any) {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fmt.Sprint(v)
	}
	must(w.Write(out))
}
func finish(f *os.File, w *csv.Writer) { w.Flush(); must(w.Error()); must(f.Close()) }

func export(dir string) {
	must(os.MkdirAll(dir, 0755))
	f, w := csvFile(filepath.Join(dir, "summary.csv"))
	defer finish(f, w)
	row(w, "scenario", "quantum_packets", "policy", "packets", "max_reg_mbps", "max_accept_mbps", "max_depart_mbps", "max_paired_rate_gap_fraction_capacity", "min_reg_rtt_ms", "min_accept_rtt_ms", "min_depart_rtt_ms", "max_reg_depart_rtt_bias_ms", "max_submission_bytes_per_worker_wakeup", "max_entry_bytes", "max_queue_entries", "max_queued_bytes", "queue_blocked_ticks", "credit_blocked_ticks", "reg_peak_live", "accept_peak_live", "depart_peak_live", "reg_final_live", "reg_delivered", "accept_delivered", "depart_delivered", "invalid_reg", "invalid_accept", "invalid_depart", "limited_reg", "limited_accept", "limited_depart", "host_contiguous_burst_bytes", "last_ack_us", "fault_triggered")
	pf, pw := csvFile(filepath.Join(dir, "packets.csv"))
	defer finish(pf, pw)
	sf, sw := csvFile(filepath.Join(dir, "samples.csv"))
	defer finish(sf, sw)
	row(pw, "run", "packet", "datagram", "entry", "bytes", "registration_us", "submission_us", "departure_us", "receive_us", "ack_us", "retire_us", "outcome")
	row(sw, "run", "time_us", "packet", "boundary", "delivered_bytes", "live_records", "first_send_us", "delivered_time_us", "app_marker", "sample_bytes", "send_elapsed_us", "ack_elapsed_us", "interval_us", "rtt_us", "rate_bytes_per_sec", "valid", "app_limited")
	for _, c := range scenarios() {
		for _, q := range []int{1, 4, 16} {
			for _, credit := range []bool{false, true} {
				r := simulate(c, q, credit)
				m := summarize(r)
				row(w, c.Name, q, credit, len(r.Packets), m.MaxRate[0]*8/1e6, m.MaxRate[1]*8/1e6, m.MaxRate[2]*8/1e6, m.MaxRateGap, float64(m.MinRTT[0])/1000, float64(m.MinRTT[1])/1000, float64(m.MinRTT[2])/1000, m.MaxRTTBias, r.Burst, r.EntryPeak, r.QueuePeak, r.LocalBytesPeak, r.QueueStops, r.QuantumStops, r.Peak[0], r.Peak[1], r.Peak[2], r.Final[0], r.Delivered[0], r.Delivered[1], r.Delivered[2], m.Invalid[0], m.Invalid[1], m.Invalid[2], m.Limited[0], m.Limited[1], m.Limited[2], r.HostBurst, r.Duration, r.Fault)
				// Keep a small representative trace set; the viewer can replay every run.
				keep := q == 4 && (c.Name == "ordinary" || c.Name == "delay-40ms" || c.Name == "stall-compress-gso" || c.Name == "idle-restart" || c.Name == "partial-size-error" || c.Name == "unknown-progress")
				if !keep {
					continue
				}
				n := name(r, credit)
				for _, p := range r.Packets {
					row(pw, n, p.ID, p.Datagram, p.Entry, p.Bytes, p.Reg, p.Submit, p.Depart, p.Receive, p.Ack, p.Retire, p.Outcome)
				}
				for _, e := range r.Frames {
					if e.Event != "ack" {
						continue
					}
					for i, s := range e.Samples {
						row(sw, n, e.Time, e.Packet, []string{"registration", "acceptance", "departure"}[i], e.Delivered[i], e.Outstanding[i], e.First[i], e.DeliveredAt[i], e.Marker[i], s.Bytes, s.SendElapsed, s.AckElapsed, s.Interval, s.RTT, s.Rate, s.Valid, s.Limited)
					}
				}
			}
		}
	}
}

func main() {
	out := flag.String("export", "", "write deterministic evidence CSVs instead of opening viewer")
	which := flag.String("scenario", "stall-compress-gso", "scenario name; -list shows choices")
	quantum := flag.Int("quantum", 4, "quantum in 1200-byte datagrams: 1, 4, or 16")
	credit := flag.Bool("credit", false, "subtract queued bytes from each opportunity's quantum")
	list := flag.Bool("list", false, "list scenarios")
	flag.Parse()
	if *list {
		for _, c := range scenarios() {
			fmt.Println(c.Name)
		}
		return
	}
	if *out != "" {
		export(*out)
		return
	}
	if *quantum != 1 && *quantum != 4 && *quantum != 16 {
		panic("quantum must be 1, 4 or 16")
	}
	var c scenario
	found := false
	for _, x := range scenarios() {
		if x.Name == *which {
			c = x
			found = true
		}
	}
	if !found {
		panic("unknown scenario; use -list")
	}
	r := simulate(c, *quantum, *credit)
	reader := bufio.NewScanner(os.Stdin)
	pos := 0
	for {
		e := r.Frames[pos]
		fmt.Printf("\033[2J\033[H\033[1mDISPOSABLE DELIVERY MODEL — %s\033[0m\n", name(r, *credit))
		fmt.Printf("event %d/%d: %s packet %d at %.3fms\n", pos+1, len(r.Frames), e.Event, e.Packet, float64(e.Time)/1000)
		p := r.Packets[e.Packet]
		fmt.Printf("packet facts (includes future oracle knowledge): %+v\n", p)
		fmt.Println("                            register       accept       depart")
		fmt.Printf("delivered bytes        %12d %12d %12d\n", e.Delivered[0], e.Delivered[1], e.Delivered[2])
		fmt.Printf("live snapshot records  %12d %12d %12d\n", e.Outstanding[0], e.Outstanding[1], e.Outstanding[2])
		fmt.Printf("first send us          %12d %12d %12d\n", e.First[0], e.First[1], e.First[2])
		fmt.Printf("delivery time us       %12d %12d %12d\n", e.DeliveredAt[0], e.DeliveredAt[1], e.DeliveredAt[2])
		fmt.Printf("app-limited marker     %12d %12d %12d\n", e.Marker[0], e.Marker[1], e.Marker[2])
		if e.Event == "ack" {
			for i, s := range e.Samples {
				fmt.Printf("%s sample: %+v\n", []string{"reg", "accept", "depart"}[i], s)
			}
		}
		fmt.Printf("run maxima: queue %d entries / %d bytes; submission burst %d bytes; live %v\n", r.QueuePeak, r.LocalBytesPeak, r.Burst, r.Peak)
		fmt.Println("[Enter] next event  [a] next ACK  [j milliseconds] jump  [r] reset  [q] quit")
		if !reader.Scan() {
			return
		}
		line := reader.Text()
		switch {
		case line == "q":
			return
		case line == "r":
			pos = 0
		case line == "a":
			for pos < len(r.Frames)-1 {
				pos++
				if r.Frames[pos].Event == "ack" {
					break
				}
			}
		case len(line) > 2 && line[:2] == "j ":
			ms, err := strconv.ParseFloat(line[2:], 64)
			if err == nil {
				pos = 0
				for pos < len(r.Frames)-1 && r.Frames[pos].Time < int(ms*1000) {
					pos++
				}
			}
		default:
			pos = min(pos+1, len(r.Frames)-1)
		}
	}
}
