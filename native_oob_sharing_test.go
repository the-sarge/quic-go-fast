//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestE17SnapshotOOBIsolation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		destination := netip.MustParseAddrPort("127.0.0.1:1234")
		a := packetOOB(nil, destination, protocol.ECT0)
		b := packetOOB(nil, destination, protocol.ECT1)
		controls := [][]byte{bytes.Clone(a), bytes.Clone(a), bytes.Clone(b), nil, {}}
		var observed [][]byte
		var nilControl []bool
		release := make(chan struct{})
		released := false
		releaseNative := func() {
			if !released {
				close(release)
				released = true
			}
		}
		e := startNativeEndpoint(ordinaryTestNative{
			endpointTestNative: endpointTestNative{
				deadline: func(deadline time.Time) error {
					if !deadline.IsZero() {
						<-release
					}
					return nil
				},
				send: func(data []byte) (int, error) { return len(data), nil },
			},
			observe: func(oob []byte, _ netip.AddrPort) {
				observed = append(observed, bytes.Clone(oob))
				nilControl = append(nilControl, oob == nil)
			},
		}, time.Second, nil)
		defer func() { releaseNative(); <-e.stop() }()
		p, err := e.attach(1)
		require.NoError(t, err)
		request := endpointTestRequest(1, "a", "b", "c", "d", "e")
		for i := range request.members {
			request.members[i].oob = controls[i]
		}
		require.NoError(t, e.offer(p, 0, request))
		synctest.Wait()
		clear(controls[0])
		clear(controls[1])
		clear(controls[2])
		clear(request.members)
		releaseNative()
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		require.NoError(t, result.failure())
		require.Equal(t, [][]byte{a, a, b, nil, {}}, observed)
		require.Equal(t, []bool{false, false, false, true, false}, nilControl)
	})
}

func TestE17ProducerOOBValues(t *testing.T) {
	ipv4 := netip.MustParseAddrPort("127.0.0.1:1234")
	ipv6 := netip.MustParseAddrPort("[::1]:1234")
	info4 := packetInfo{addr: netip.MustParseAddr("192.0.2.1")}
	other4 := packetInfo{addr: netip.MustParseAddr("192.0.2.2")}
	info6 := packetInfo{addr: netip.MustParseAddr("2001:db8::1")}
	for _, tc := range []struct {
		name          string
		destination   netip.AddrPort
		first, next   []byte
		firstECN, ECN protocol.ECN
	}{
		{"changed ECN", ipv4, nil, nil, protocol.ECT0, protocol.ECT1},
		{"changed base", ipv4, info4.OOB(), other4.OOB(), protocol.ECT0, protocol.ECT0},
		{"equal IPv6", ipv6, info6.OOB(), info6.OOB(), protocol.ECT1, protocol.ECT1},
		{"oversize", ipv4, bytes.Repeat(info4.OOB(), 8), bytes.Repeat(info4.OOB(), 8), protocol.ECT0, protocol.ECT0},
		{"nil and empty", ipv4, nil, []byte{}, protocol.ECNUnsupported, protocol.ECNUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			firstWant := packetOOB(tc.first, tc.destination, tc.firstECN)
			nextWant := packetOOB(tc.next, tc.destination, tc.ECN)
			r := ordinaryLocalRecord{state: ordinaryBuilding}
			first := r.packetOOB(tc.first, tc.destination, tc.firstECN)
			r.request.members = append(r.request.members, endpointMember{oob: first})
			next := r.packetOOB(tc.next, tc.destination, tc.ECN)
			clear(tc.first)
			clear(tc.next)
			require.Equal(t, firstWant, first)
			require.Equal(t, nextWant, next)
		})
	}
}

func TestE17ProducerOOBLifetime(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		socket.hold = nativeProbeHoldAll
		info := packetInfo{addr: netip.MustParseAddr("192.0.2.1")}
		base := info.OOB()
		sender.oob = base
		want := packetOOB(base, sender.destination, protocol.ECNNon)
		first, second := getPacketBuffer(), getPacketBuffer()
		appendNativeGroupMember(t, tc, sender, first, "first member", protocol.ECNNon, sendMetadata{})
		appendNativeGroupMember(t, tc, sender, second, "second member", protocol.ECNNon, sendMetadata{})
		r := sender.pending
		producerFirst, producerSecond := r.request.members[0].oob, r.request.members[1].oob
		sender.finish()
		synctest.Wait()
		require.Empty(t, r.request.members)
		clear(base)
		clear(producerFirst)
		clear(producerSecond)
		sender.endpoint.do(func(state *endpointState) {
			require.NotNil(t, state.current)
			require.Len(t, state.current.request.members, 2)
			for _, member := range state.current.request.members {
				require.Equal(t, want, member.oob, "producer cleanup cannot mutate the native borrow")
			}
		})
		socket.release()
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Zero(t, first.refCount)
		require.Zero(t, second.refCount)
	})
}
