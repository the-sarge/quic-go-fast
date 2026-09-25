// Q1-only routed packet adapter. Requires exclusively owned campaign interfaces,
// native offloads disabled, and kernel ip_forward=0. It never changes host state.
package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/gopacket/gopacket/afpacket"
	"golang.org/x/net/bpf"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
	"q1campaign/model"
)

type side struct {
	Interface  string
	PeerMAC    string
	Focal      string
	Competitor string
}
type config struct {
	A, B           side
	Scenario       string
	StartUnixNS    int64
	MeasureSeconds int
	Phase          int
	Seed           uint64
}
type sample struct {
	UnixNS int64
	Stats  model.Stats
}
type observation struct {
	Direction                                          string
	Error                                              string
	ReadPackets, NonCanonical, SocketDrops, SendErrors uint64
	MaxIngressLagNS, MaxEgressLagNS                    int64
	MaxIngressAtNS, MaxEgressAtNS                      int64
	MissingTimestamps                                  uint64
	Samples                                            []sample
	Stats                                              model.Stats
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	input := flag.String("config", "", "frozen adapter JSON")
	output := flag.String("output", "", "observed counters JSON")
	flag.Parse()
	b, e := os.ReadFile(*input)
	if e != nil {
		return e
	}
	var c config
	if e = json.Unmarshal(b, &c); e != nil {
		return e
	}
	if c.MeasureSeconds < 1 || c.MeasureSeconds > 300 || time.Until(time.Unix(0, c.StartUnixNS)) > 2*time.Minute || *output == "" {
		return fmt.Errorf("invalid bounded configuration")
	}
	forwarding, e := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if e != nil || string(forwarding) != "0\n" {
		return fmt.Errorf("kernel forwarding must be disabled by lifecycle owner")
	}
	runtime.GOMAXPROCS(4)
	measured := time.Now().Add(time.Until(time.Unix(0, c.StartUnixNS)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithDeadline(ctx, measured.Add(time.Duration(c.MeasureSeconds)*time.Second+3*time.Second))
	defer cancel()
	results := make(chan observation, 2)
	var wg sync.WaitGroup
	for i, pair := range [][2]side{{c.A, c.B}, {c.B, c.A}} {
		q, err := model.Scenario(c.Scenario, i == 0, c.Phase, c.Seed+uint64(i), 1460)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func(index int, from, to side, q *model.Queue) {
			defer wg.Done()
			r := forward(ctx, measured, index, from, to, q)
			if r.Error != "" {
				cancel()
			}
			results <- r
		}(i, pair[0], pair[1], q)
	}
	wg.Wait()
	close(results)
	var observations []observation
	failed := false
	for r := range results {
		observations = append(observations, r)
		if r.Error != "" || r.SocketDrops > 0 || r.NonCanonical > 0 || r.SendErrors > 0 || r.Stats.PropagationOverflow > 0 || r.MissingTimestamps > 0 || r.MaxIngressLagNS > int64(5*time.Millisecond) || r.MaxEgressLagNS > int64(5*time.Millisecond) {
			failed = true
		}
	}
	data, e := json.MarshalIndent(struct {
		Config       config
		Observations []observation
	}{c, observations}, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(*output, append(data, '\n'), 0600); e != nil {
		return e
	}
	if failed {
		return fmt.Errorf("adapter limitations observed; retain output and reject calibration")
	}
	return nil
}
func socket(name string) (int, *net.Interface, error) {
	nic, e := net.InterfaceByName(name)
	if e != nil {
		return -1, nil, e
	}
	fd, e := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, 0)
	if e != nil {
		return -1, nil, e
	}
	fail := func(err error) (int, *net.Interface, error) { unix.Close(fd); return -1, nil, err }
	if e = unix.Bind(fd, &unix.SockaddrLinklayer{Ifindex: nic.Index}); e != nil {
		return fail(e)
	}
	return fd, nic, nil
}
func htons(n uint16) uint16 { return n<<8 | n>>8 }
func forward(ctx context.Context, epoch time.Time, index int, from, to side, q *model.Queue) observation {
	o := observation{Direction: fmt.Sprintf("%s->%s", from.Interface, to.Interface)}
	rx, e := afpacket.NewTPacket(afpacket.OptInterface(from.Interface), afpacket.OptProtocol(unix.ETH_P_IP), afpacket.TPacketVersion3, afpacket.OptFrameSize(2048), afpacket.OptBlockSize(65536), afpacket.OptNumBlocks(128), afpacket.OptBlockTimeout(time.Millisecond), afpacket.OptPollTimeout(100*time.Millisecond))
	if e != nil {
		o.Error = e.Error()
		return o
	}
	defer rx.Close()
	filter, e := bpf.Assemble([]bpf.Instruction{bpf.LoadExtension{Num: bpf.ExtType}, bpf.JumpIf{Cond: bpf.JumpEqual, Val: unix.PACKET_OUTGOING, SkipTrue: 1}, bpf.RetConstant{Val: 65535}, bpf.RetConstant{Val: 0}})
	if e == nil {
		e = rx.SetBPF(filter)
	}
	if e != nil {
		o.Error = e.Error()
		return o
	}
	tx, nic, e := socket(to.Interface)
	if e != nil {
		o.Error = e.Error()
		return o
	}
	defer unix.Close(tx)
	peer, e := net.ParseMAC(to.PeerMAC)
	if e != nil || len(peer) != 6 {
		o.Error = "invalid next-hop MAC"
		return o
	}
	fromIP, toIP := net.ParseIP(from.Focal), net.ParseIP(to.Focal)
	fromComp, toComp := net.ParseIP(from.Competitor), net.ParseIP(to.Competitor)
	if fromIP == nil || toIP == nil {
		o.Error = "invalid focal addresses"
		return o
	}
	type arrival struct {
		frame    []byte
		kernelNS int64
	}
	incoming := make(chan arrival, 256)
	readDone := make(chan struct{})
	localCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer close(readDone)
		for {
			if localCtx.Err() != nil {
				return
			}
			data, capture, err := rx.ZeroCopyReadPacketData()
			if err != nil {
				if localCtx.Err() != nil {
					return
				}
				if errors.Is(err, afpacket.ErrTimeout) || errors.Is(err, unix.EINTR) {
					continue
				}
				select {
				case incoming <- arrival{nil, -1}:
				case <-localCtx.Done():
				}
				return
			}
			// The ring owns this view until the next read. Copy before publishing it.
			frame := append([]byte(nil), data...)
			stamp := capture.Timestamp.UnixNano()
			select {
			case incoming <- arrival{frame, stamp}:
			case <-localCtx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(100 * time.Microsecond)
	defer ticker.Stop()
	samples := time.NewTicker(time.Second)
	defer samples.Stop()
	send := func(packets []model.Packet) {
		for _, p := range packets {
			if lag := int64(time.Since(epoch) - p.ScheduledAt); lag > o.MaxEgressLagNS {
				o.MaxEgressLagNS = lag
				o.MaxEgressAtNS = time.Now().UnixNano()
			}
			frame := make([]byte, 14+len(p.Bytes))
			copy(frame, peer)
			copy(frame[6:], nic.HardwareAddr)
			binary.BigEndian.PutUint16(frame[12:14], unix.ETH_P_IP)
			copy(frame[14:], p.Bytes)
			ip := frame[14:]
			ip[1] = ip[1]&0xfc | p.ECN
			ip[8]--
			ip[10], ip[11] = 0, 0
			var sum uint32
			for i := 0; i < 20; i += 2 {
				sum += uint32(binary.BigEndian.Uint16(ip[i : i+2]))
			}
			for sum>>16 != 0 {
				sum = (sum & 0xffff) + (sum >> 16)
			}
			binary.BigEndian.PutUint16(ip[10:12], ^uint16(sum))
			if e := unix.Sendto(tx, frame, 0, &unix.SockaddrLinklayer{Protocol: htons(unix.ETH_P_IP), Ifindex: nic.Index, Halen: 6, Addr: [8]uint8{peer[0], peer[1], peer[2], peer[3], peer[4], peer[5]}}); e != nil {
				o.SendErrors++
			}
		}
	}
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			send(q.Advance(time.Since(epoch)))
		case now := <-samples.C:
			o.Samples = append(o.Samples, sample{now.UnixNano(), q.Stats})
		case a := <-incoming:
			if a.frame == nil {
				o.Error = "packet receive failed"
				break loop
			}
			o.ReadPackets++
			if len(a.frame) < 34 {
				continue
			}
			h, err := ipv4.ParseHeader(a.frame[14:])
			if err != nil {
				continue
			}
			srcOK := h.Src.Equal(fromIP) || (fromComp != nil && h.Src.Equal(fromComp))
			dstOK := h.Dst.Equal(toIP) || (toComp != nil && h.Dst.Equal(toComp))
			if !srcOK || !dstOK {
				continue
			}
			if h.Len != 20 || h.TotalLen > 1460 || h.TotalLen < 20 || h.TotalLen+14 > len(a.frame) || h.FragOff != 0 || h.Flags&ipv4.MoreFragments != 0 || h.TTL < 2 || (h.Protocol != 1 && h.Protocol != 6 && h.Protocol != 17) {
				o.NonCanonical++
				continue
			}
			if a.kernelNS == 0 {
				o.MissingTimestamps++
			} else {
				if lag := time.Now().UnixNano() - a.kernelNS; lag > o.MaxIngressLagNS {
					o.MaxIngressLagNS = lag
					o.MaxIngressAtNS = time.Now().UnixNano()
				}
			}
			at := time.Since(epoch)
			send(q.Advance(at))
			q.Admit(at, model.Packet{Bytes: a.frame[14 : 14+h.TotalLen], ECN: uint8(h.TOS & 3), Competitor: (fromComp != nil && h.Src.Equal(fromComp)) || (toComp != nil && h.Dst.Equal(toComp))})
		}
	}
	cancel()
	<-readDone
	_, stats, err := rx.SocketStats()
	if err != nil {
		o.Error = err.Error()
	} else {
		o.SocketDrops = uint64(stats.Drops())
	}
	o.Stats = q.Stats
	return o
}
