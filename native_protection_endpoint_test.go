//go:build darwin || linux || freebsd

package quic

import (
	"bytes"
	"os"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func nativeProtectionEndpointRequest(t *testing.T, sealer handshake.ShortHeaderSealer, packets ...int) (endpointRequest, []*nativeProtectionPlan, [][]byte) {
	t.Helper()
	request := endpointTestRequest(1)
	request.deadline = time.Now().Add(time.Second)
	var plans []*nativeProtectionPlan
	var expected [][]byte
	var pn protocol.PacketNumber
	for _, count := range packets {
		buffer := getPacketBuffer()
		if count > 1 {
			buffer.Release()
			buffer = getLargePacketBuffer()
		}
		plan := newNativeProtectionPlan(buffer)
		var ciphertext []byte
		for range count {
			ciphertext = append(ciphertext, appendNativeProtectionTestPacket(t, plan, sealer, pn, 2, 16)...)
			pn++
		}
		require.True(t, plan.freeze())
		member := endpointMember{buffer: buffer, protection: plan, writeState: &endpointWriteState{}}
		if count > 1 {
			member.gsoSize = uint16(len(ciphertext) / count)
		}
		request.members = append(request.members, member)
		plans = append(plans, plan)
		expected = append(expected, ciphertext)
	}
	return request, plans, expected
}

func TestNativeProtectionEndpointRejectsMalformedOwnership(t *testing.T) {
	sealer := nativeProtectionTestSealer(t)
	for _, tc := range []struct {
		name   string
		mutate func(*endpointRequest)
	}{
		{"not frozen", func(r *endpointRequest) { r.members[0].protection.frozen = false }},
		{"wrong buffer", func(r *endpointRequest) {
			r.members[0].buffer = &packetBuffer{Data: r.members[0].buffer.Data, refCount: 1}
		}},
		{"changed length", func(r *endpointRequest) {
			r.members[0].buffer.Data = r.members[0].buffer.Data[:len(r.members[0].buffer.Data)-1]
		}},
		{"nonexclusive", func(r *endpointRequest) { r.members[0].buffer.Split() }},
		{"recipe count", func(r *endpointRequest) { r.members[0].gsoSize = 0 }},
		{"recipe boundary", func(r *endpointRequest) { r.members[0].gsoSize++ }},
		{"duplicate plan", func(r *endpointRequest) { r.members[1].protection = r.members[0].protection }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, plans, _ := nativeProtectionEndpointRequest(t, sealer, 2, 1)
			buffers := []*packetBuffer{plans[0].buffer, plans[1].buffer}
			original := [][]byte{buffers[0].Data, buffers[1].Data}
			defer func() {
				for i, p := range plans {
					buffers[i].Data = original[i]
					buffers[i].refCount = 1
					p.release()
					buffers[i].Release()
				}
			}()
			tc.mutate(&request)
			_, valid := request.checkedSize(endpointData)
			require.False(t, valid)
			for _, p := range plans {
				require.NotNil(t, p.buffer)
				require.Zero(t, p.next)
				require.NotZero(t, p.entries[0].protection)
			}
		})
	}
}

func TestNativeProtectionEndpointRetriesReuseCiphertext(t *testing.T) {
	for _, mode := range []string{"first EPERM", "GSO EIO", "revoked before transform", "revoked after transform"} {
		t.Run(mode, func(t *testing.T) {
			count := 1
			if mode == "GSO EIO" {
				count = 2
			}
			request, plans, expected := nativeProtectionEndpointRequest(t, nativeProtectionTestSealer(t), count)
			defer request.releasePayload()
			plan, buffer := plans[0], plans[0].buffer
			request.members[0].nativeGSO = mode == "GSO EIO"
			token := &endpointToken{request: &request}
			if mode == "revoked after transform" {
				var scratch handshake.PacketProtectionScratch
				plan.protectAll(&scratch)
			}
			revoked := mode == "revoked before transform" || mode == "revoked after transform"
			if revoked {
				token.canceled.Store(true)
			}
			var writes [][]byte
			result := writeEndpointRequest(endpointTestNative{send: func(data []byte) (int, error) {
				writes = append(writes, bytes.Clone(data))
				if len(writes) == 1 {
					cause := syscall.EPERM
					if mode == "GSO EIO" {
						cause = syscall.EIO
					}
					return 0, &os.SyscallError{Syscall: "sendmsg", Err: cause}
				}
				return len(data), nil
			}}, token)
			member := result.members[0]
			if revoked {
				require.ErrorIs(t, member.err, errEndpointRevoked)
				require.Equal(t, count, member.unsubmitted)
				require.Zero(t, member.uncertain)
				require.Empty(t, writes)
				if mode == "revoked before transform" {
					require.Zero(t, plan.next)
				} else {
					require.EqualValues(t, count, plan.next)
				}
				return
			}
			require.NoError(t, result.failure())
			require.Equal(t, count, member.accepted)
			require.Equal(t, expected[0], buffer.Data)
			require.EqualValues(t, count, plan.next)
			for _, r := range plan.entries {
				require.Zero(t, r.protection)
			}
			if mode == "first EPERM" {
				require.Equal(t, [][]byte{expected[0], expected[0]}, writes)
			} else {
				segment := int(request.members[0].gsoSize)
				require.Equal(t, [][]byte{expected[0], expected[0][:segment], expected[0][segment:]}, writes)
				require.True(t, request.members[0].writeState.gsoDisabled.Load())
			}
		})
	}
}

func TestNativeProtectionEndpointBorrowUntilActualReturn(t *testing.T) {
	for _, phase := range []string{"setup", "write", "reset"} {
		t.Run(phase, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				sealer := nativeProtectionTestSealer(t)
				entered, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				blocked := false
				wait := func() {
					if !blocked {
						blocked = true
						close(entered)
						<-release
					}
				}
				writes := 0
				endpoint := startNativeEndpoint(endpointTestNative{deadline: func(d time.Time) error {
					if phase == "setup" && !d.IsZero() || phase == "reset" && d.IsZero() {
						wait()
					}
					return nil
				}, send: func(data []byte) (int, error) {
					writes++
					before := bytes.Clone(data)
					if phase == "write" {
						wait()
					}
					if !bytes.Equal(before, data) {
						t.Error("native payload changed while borrowed")
					}
					return len(data), nil
				}}, 10*time.Millisecond, nil)
				defer func() { once.Do(func() { close(release) }); <-endpoint.stop() }()
				p, err := endpoint.attach(1)
				require.NoError(t, err)
				request, plans, expected := nativeProtectionEndpointRequest(t, sealer, 2)
				request.deadline = time.Time{}
				plan, buffer := plans[0], plans[0].buffer
				require.NoError(t, endpoint.offer(p, 0, request))
				<-entered
				synctest.Wait()
				var token *endpointToken
				endpoint.do(func(s *endpointState) { token = s.current })
				require.NotNil(t, token)
				if phase == "setup" {
					require.Zero(t, plan.next)
				} else {
					require.Equal(t, expected[0], buffer.Data)
					require.EqualValues(t, 2, plan.next)
				}
				fence := endpoint.detach(p)
				time.Sleep(40 * time.Millisecond)
				synctest.Wait()
				require.Equal(t, 1, buffer.refCount)
				require.Same(t, buffer, plan.buffer)
				require.Nil(t, token.completion.Load())
				select {
				case <-fence:
					t.Fatal("retirement preceded actual native return")
				default:
				}
				select {
				case <-endpoint.fault:
				default:
					t.Fatal("control did not detect overdue native borrow")
				}
				once.Do(func() { close(release) })
				synctest.Wait()
				require.NotNil(t, token.completion.Load())
				require.Nil(t, plan.buffer)
				require.Empty(t, plan.entries)
				require.Zero(t, buffer.refCount)
				if phase == "setup" {
					require.Zero(t, writes)
				}
				select {
				case <-fence:
				default:
					t.Fatal("actual return did not release fence")
				}
			})
		})
	}
}

func TestNativeProtectionEndpointCapacityRefusalRetainsOwnership(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		release := make(chan struct{})
		endpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) { <-release; return len(data), nil }}, time.Second, nil)
		defer func() { close(release); <-endpoint.stop() }()
		p, err := endpoint.attach(1)
		require.NoError(t, err)
		for range 4 {
			require.NoError(t, endpoint.offer(p, 0, endpointTestRequest(1, "occupied")))
		}
		request, plans, _ := nativeProtectionEndpointRequest(t, nativeProtectionTestSealer(t), 2)
		defer request.releasePayload()
		plan, buffer := plans[0], plans[0].buffer
		before := bytes.Clone(buffer.Data)
		key := plan.entries[0].protection
		require.ErrorIs(t, endpoint.offer(p, 0, request), errEndpointCapacity)
		require.Same(t, buffer, plan.buffer)
		require.Equal(t, 1, buffer.refCount)
		require.Equal(t, before, buffer.Data)
		require.Equal(t, key, plan.entries[0].protection)
		require.Zero(t, plan.next)
	})
}

func TestNativeProtectionEndpointUnclaimedDisposal(t *testing.T) {
	for _, action := range []string{"expiry", "revocation", "setup error"} {
		t.Run(action, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				release := make(chan struct{})
				deadlineCalls := 0
				endpoint := startNativeEndpoint(endpointTestNative{deadline: func(d time.Time) error {
					deadlineCalls++
					if action == "setup error" && !d.IsZero() {
						return os.ErrPermission
					}
					return nil
				}, send: func(data []byte) (int, error) { <-release; return len(data), nil }}, time.Second, nil)
				defer func() { close(release); <-endpoint.stop() }()
				p, err := endpoint.attach(1)
				require.NoError(t, err)
				if action != "setup error" {
					require.NoError(t, endpoint.offer(p, 0, endpointTestRequest(1, "block writer")))
					synctest.Wait()
				}
				request, plans, _ := nativeProtectionEndpointRequest(t, nativeProtectionTestSealer(t), 2)
				plan, buffer := plans[0], plans[0].buffer
				before := bytes.Clone(buffer.Data)
				require.NoError(t, endpoint.offer(p, 0, request))
				switch action {
				case "revocation":
					endpoint.detach(p)
				case "expiry":
					time.Sleep(time.Second)
				}
				synctest.Wait()
				require.Nil(t, plan.buffer)
				require.Empty(t, plan.entries)
				require.Zero(t, buffer.refCount)
				require.Equal(t, before, buffer.Data)
				for _, r := range plan.entries[:cap(plan.entries)] {
					require.Zero(t, r.protection)
				}
				require.Positive(t, deadlineCalls)
			})
		})
	}
}

// Direct writer fixtures supply their own scratch; production keeps one per writer.
func writeEndpointRequest(native endpointNativeIO, token *endpointToken) endpointResult {
	var scratch handshake.PacketProtectionScratch
	var oobScratch [128]byte
	return writeEndpointRequestWithScratch(native, token, &scratch, oobScratch[:0])
}
