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

func TestE07WriterOOBScratch(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		info := packetInfo{addr: netip.MustParseAddr("192.0.2.1")}
		ipv6 := packetInfo{addr: netip.MustParseAddr("2001:db8::1")}
		bases := [][]byte{
			appendIPv4ECNMsg(info.OOB(), protocol.ECT0),
			bytes.Repeat(info.OOB(), 6), // valid CMSG framing exceeding private scratch
			appendIPv6ECNMsg(ipv6.OOB(), protocol.ECT1),
			nil,
		}
		request := endpointTestRequest(1, "aaaa", "bbbb", "cccc", "dddd")
		var expected [][]byte
		for i := range request.members {
			request.members[i].oob = bytes.Clone(bases[i])
			request.members[i].gsoSize = 2
			request.members[i].nativeGSO = true
			request.members[i].writeState = &endpointWriteState{}
			expected = append(expected, appendUDPSegmentSizeMsg(bytes.Clone(bases[i]), 2))
		}
		entered, release := make(chan struct{}), make(chan struct{})
		var borrowed []byte
		var addresses []*byte
		calls := 0
		e := startNativeEndpoint(ordinaryTestNative{
			observe: func(oob []byte, _ netip.AddrPort) {
				require.Equal(t, expected[calls], oob, "successive lengths cannot leak a stale tail")
				addresses = append(addresses, &oob[0])
				calls++
				if calls == 1 {
					borrowed = oob
					close(entered)
				}
			},
			endpointTestNative: endpointTestNative{send: func(data []byte) (int, error) {
				if calls == 1 {
					<-release
					require.Equal(t, expected[0], borrowed, "scratch stays stable through actual native return")
				}
				return len(data), nil
			}},
		}, time.Second, nil)
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
			<-e.stop()
		}()
		p, err := e.attach(1)
		require.NoError(t, err)
		require.NoError(t, e.offer(p, 0, request))
		<-entered
		synctest.Wait()
		require.Equal(t, 1, calls, "a later member cannot overwrite a live OOB borrow")
		require.Equal(t, expected[0], borrowed)
		for i, member := range request.members {
			require.Equal(t, bases[i], member.oob, "admitted OOB input remains immutable")
			require.Equal(t, 1, member.buffer.refCount)
		}
		close(release)
		<-p.completed
		result, ok := e.take(p)
		require.True(t, ok)
		require.NoError(t, result.failure())
		require.Equal(t, 4, calls)
		require.Same(t, addresses[0], addresses[2])
		require.Same(t, addresses[0], addresses[3], "oversize backing is not retained")
		require.NotSame(t, addresses[0], addresses[1])
		for i, member := range request.members {
			require.Equal(t, bases[i], member.oob)
			require.Zero(t, member.buffer.refCount)
			require.Equal(t, 2, result.members[i].accepted)
			require.Zero(t, result.members[i].uncertain)
			require.Zero(t, result.members[i].unsubmitted)
		}
	})
}
