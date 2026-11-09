//go:build linux

package quic

import (
	"bytes"
	"net/netip"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestE17SharedOOBNativeRetries(t *testing.T) {
	for _, mode := range []string{"first EPERM", "GSO EIO"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				base := packetOOB(nil, netip.MustParseAddrPort("127.0.0.1:1234"), protocol.ECT0)
				gso := appendUDPSegmentSizeMsg(bytes.Clone(base), 2)
				var metadata [][]byte
				e := startNativeEndpoint(endpointResponseTestNative{sendMetadata: func(data, oob []byte, _ netip.AddrPort) (int, int, error) {
					metadata = append(metadata, bytes.Clone(oob))
					if len(metadata) == 1 {
						cause := syscall.EPERM
						if mode == "GSO EIO" {
							cause = syscall.EIO
						}
						return 0, 0, &os.SyscallError{Syscall: "sendmsg", Err: cause}
					}
					return len(data), len(oob), nil
				}}, time.Second, nil)
				defer func() { <-e.stop() }()
				p, err := e.attach(1)
				require.NoError(t, err)
				request := endpointTestRequest(1, "aaaa", "bbbb")
				state := &endpointWriteState{}
				for i := range request.members {
					request.members[i].oob = bytes.Clone(base)
					request.members[i].gsoSize, request.members[i].nativeGSO = 2, true
					request.members[i].writeState = state
				}
				require.NoError(t, e.offer(p, 0, request))
				<-p.completed
				result, ok := e.take(p)
				require.True(t, ok)
				require.NoError(t, result.failure())
				if mode == "first EPERM" {
					require.Equal(t, [][]byte{gso, gso, gso}, metadata)
				} else {
					require.Equal(t, [][]byte{gso, base, base, base, base}, metadata)
				}
				for i, member := range request.members {
					require.Equal(t, base, member.oob)
					require.Equal(t, 2, result.members[i].accepted)
					require.Zero(t, member.buffer.refCount)
				}
			})
		})
	}
}
