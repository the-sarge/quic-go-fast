package main

import (
	"errors"
	"net"
	"reflect"
	"runtime"
	"testing"
)

type writerFunc func([][]byte, []byte, *net.UDPAddr) (int, error)

func (f writerFunc) WriteBatchV1(b [][]byte, o []byte, a *net.UDPAddr) (int, error) {
	return f(b, o, a)
}

func TestStageAForwarding(t *testing.T) {
	sentinel := errors.New("writer result")
	for _, tc := range []struct {
		name string
		n    int
		err  error
	}{
		{"full", 3, nil}, {"prefix", 1, sentinel}, {"zero", 0, sentinel}, {"unknown", -1, sentinel}, {"invalid", 4, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := [][]byte{{1}, {2}, {3}}
			o := []byte{9}
			a := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 42}
			calls := 0
			c := &batchCounters{}
			n, err := c.forward(writerFunc(func(got [][]byte, ancillary []byte, dst *net.UDPAddr) (int, error) {
				calls++
				if &got[0] != &b[0] || &got[0][0] != &b[0][0] || &ancillary[0] != &o[0] || dst != a {
					t.Fatal("borrowed arguments changed")
				}
				return tc.n, tc.err
			}))(b, o, a)
			if calls != 1 || n != tc.n || err != tc.err {
				t.Fatal("forwarded result changed")
			}
			want := batchSnapshot{Calls: 1, MultiOffers: 1, Offered: 3, LargestOffer: 3, MultiOfferedECN: [5]uint64{0, 0, 0, 0, 3}}
			if tc.err != nil {
				want.Errors = 1
			}
			if tc.n >= 0 && tc.n <= 3 {
				want.Accepted = uint64(tc.n)
				want.MultiAcceptedECN[4] = uint64(tc.n)
				if tc.n < 3 {
					want.ShortPrefixes = 1
				}
				if tc.n > 1 {
					want.MultiAccepted = 1
				}
			} else {
				want.InvalidCounts = 1
			}
			if got := c.snapshot(); got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
	for _, tc := range markingCases() {
		t.Run(tc.name, func(t *testing.T) {
			c := &batchCounters{}
			c.forward(writerFunc(func(_ [][]byte, o []byte, _ *net.UDPAddr) (int, error) {
				if !reflect.DeepEqual(o, tc.oob) {
					t.Fatal("marking changed")
				}
				return 2, nil
			}))([][]byte{{1}, {2}}, tc.oob, &net.UDPAddr{})
			got := c.snapshot()
			if got.MultiOfferedECN[tc.bucket] != 2 || got.MultiAcceptedECN[tc.bucket] != 2 || got.MultiAccepted != 1 {
				t.Fatalf("bad buckets: %+v", got)
			}
		})
	}
}

type markingCase struct {
	name   string
	oob    []byte
	bucket int
}

func TestStageAMetadataAndSeparation(t *testing.T) {
	oldSource, oldDigest := sourceRevision, overlayDigest
	defer func() { sourceRevision, overlayDigest = oldSource, oldDigest }()
	sourceRevision = candidateRevision
	overlayDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	m := metadata()
	if m.LibraryRevision != candidateRevision || m.OverlaySHA256 != overlayDigest || m.PMTU || m.GSO || m.InitialUDPSize != 1400 || m.ACKMode != "ordinary-unset" {
		t.Fatalf("bad metadata: %+v", m)
	}
	r := Run{ID: "M-LAN-linux-amd64-C-Reno-stream-r1-managed-s635599-p0", Controller: "reno", Workload: "stream", WarmupMS: 30000, MeasureMS: 180000, PayloadBytes: 16384, StartUnixNS: 1}
	for _, tc := range []struct {
		name               string
		mutate             func(*Run)
		managed, tcp, want bool
	}{
		{"registered", func(*Run) {}, true, false, runtime.GOOS == "linux" && runtime.GOARCH == "amd64"},
		{"qualification", func(r *Run) { r.ID = "q2-core-1" }, true, false, false},
		{"native", func(*Run) {}, false, false, false},
		{"competitor", func(*Run) {}, true, true, false},
		{"duration", func(r *Run) { r.MeasureMS++ }, true, false, false},
		{"completion", func(r *Run) { r.CompletionBytes = 1 }, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := r
			tc.mutate(&v)
			if got := validateStageA(v, tc.managed, tc.tcp) == nil; got != tc.want {
				t.Fatalf("accepted=%v want=%v", got, tc.want)
			}
		})
	}
}
