//go:build darwin || linux || freebsd

package quic

import (
	"fmt"
	"net"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEndpointAuxiliaryAdmissionPreservesGrant(t *testing.T) {
	for _, lane := range []int{0, 2} {
		t.Run(fmt.Sprintf("lane-%d", lane), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
				defer func() { <-e.stop() }()
				p, err := e.attach(1)
				require.NoError(t, err)
				for range 4 {
					require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "retained result")))
					synctest.Wait()
				}
				pending := endpointTestRequest(1, "persistent demand")
				defer pending.buffers[0].Release()
				require.ErrorIs(t, e.offer(p, lane, pending), errEndpointCapacity)
				_, ok := e.take(p)
				require.True(t, ok)
				var grant endpointGrant
				e.do(func(s *endpointState) { grant = *s.grant })
				require.Same(t, p, grant.participant)
				require.False(t, grant.expires.IsZero())

				responses := newEndpointResponses(e)
				transport := &Transport{responses: responses}
				address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
				written, err := transport.WriteTo([]byte("public"), address)
				require.NoError(t, err)
				require.Equal(t, 6, written)
				require.NoError(t, responses.send(nil, nil, []byte("stateless"), address, packetInfo{}))
				synctest.Wait()
				var after endpointGrant
				e.do(func(s *endpointState) { after = *s.grant })
				require.Equal(t, grant, after, "one-shot work must neither consume nor renew the persistent grant")
				require.Len(t, p.ready, 1, "one-shot work must not drain its notification")
				require.False(t, e.public.wait[0].pending)
				require.False(t, e.stateless.wait[0].pending)
			})
		})
	}
}

// Fill through real endpoint operations: these records remain used even though
// the writer has returned every payload and the request queue is empty.
func retainEndpointCompletionPool(t *testing.T, e *nativeEndpoint) *endpointParticipant {
	t.Helper()
	var first *endpointParticipant
	for id := uint64(1); id <= 64; id++ {
		p, err := e.attach(id)
		require.NoError(t, err)
		if first == nil {
			first = p
		}
		for range 4 {
			require.NoError(t, e.offer(p, 0, endpointTestRequest(id, "retained")))
			synctest.Wait()
		}
	}
	return first
}

func TestEndpointAuxiliaryAdmissionProtectsFinalRecord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
		defer func() { <-e.stop() }()
		first := retainEndpointCompletionPool(t, e)
		p, err := e.attach(65)
		require.NoError(t, err)
		_, err = e.prepare(p, 0, 1)
		require.ErrorIs(t, err, errEndpointCapacity) // demand exists before a record becomes free
		responses := newEndpointResponses(e)
		transport := &Transport{responses: responses}
		address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
		for free := range 2 {
			written, err := transport.WriteTo([]byte("refused"), address)
			require.Zero(t, written)
			require.ErrorIs(t, err, ErrMultipathBackpressureV1, "free records: %d", free)
			require.ErrorIs(t, responses.send(nil, nil, []byte("refused"), address, packetInfo{}), errEndpointCapacity)
			_, ok := e.take(first)
			require.True(t, ok)
		}
		// Two records allow one-shot work without consuming the selected turn.
		written, err := transport.WriteTo(nil, address)
		require.NoError(t, err)
		require.Zero(t, written)
		require.NoError(t, responses.send(nil, nil, []byte("surplus"), address, packetInfo{}))
		synctest.Wait()
		reservation, err := e.prepare(p, 0, 1)
		require.NoError(t, err)
		// The materialized reservation counts once: the last free record is usable.
		written, err = transport.WriteTo([]byte("last free record"), address)
		require.NoError(t, err)
		require.Equal(t, 16, written)
		require.NoError(t, responses.send(nil, nil, []byte("last free record"), address, packetInfo{}))
		synctest.Wait()
		other, err := e.prepare(p, 1, 1)
		require.NoError(t, err)
		written, err = transport.WriteTo(nil, address)
		require.Zero(t, written)
		require.ErrorIs(t, err, ErrMultipathBackpressureV1, "prepared reservations use completion records")
		e.cancelAdmission(other)
		e.cancelAdmission(reservation)
	})
}

func TestEndpointAuxiliaryAdmissionReleasesProtection(t *testing.T) {
	for _, release := range []string{"withdrawal", "lease expiry", "teardown"} {
		t.Run(release, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				e := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
				defer func() { <-e.stop() }()
				first := retainEndpointCompletionPool(t, e)
				p, err := e.attach(65)
				require.NoError(t, err)
				successor, err := e.attach(66)
				require.NoError(t, err)
				for _, producer := range []*endpointParticipant{p, successor} {
					_, err := e.prepare(producer, 0, 1)
					require.ErrorIs(t, err, errEndpointCapacity)
				}
				_, ok := e.take(first)
				require.True(t, ok)
				<-p.ready
				switch release {
				case "withdrawal":
					e.cancelReadiness(p, 0)
				case "lease expiry":
					time.Sleep(endpointLease)
				case "teardown":
					<-e.detach(p)
				}
				synctest.Wait()
				require.Len(t, successor.ready, 1, "released protection must transfer to the registered successor")
				responses := newEndpointResponses(e)
				address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
				require.ErrorIs(t, responses.send(nil, nil, []byte("protected"), address, packetInfo{}), errEndpointCapacity)
				e.cancelReadiness(successor, 0)
				require.NoError(t, responses.send(nil, nil, []byte("released"), address, packetInfo{}))
				synctest.Wait()
				require.Empty(t, p.ready)
				require.Empty(t, successor.ready)
			})
		})
	}
}

func TestEndpointAuxiliaryAdmissionConcurrentBounds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var sent []string
		e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			<-release
			sent = append(sent, string(data))
			return len(data), nil
		}}, time.Second, nil)
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
			<-e.stop()
		}()
		responses := newEndpointResponses(e)
		transport := &Transport{responses: responses}
		address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
		type outcome struct {
			payload string
			written int
			err     error
		}
		public := make(chan outcome, 9)
		stateless := make(chan outcome, 9)
		for i := range 9 {
			go func() {
				payload := fmt.Sprintf("public-%d", i)
				n, err := transport.WriteTo([]byte(payload), address)
				public <- outcome{payload, n, err}
			}()
			go func() {
				payload := fmt.Sprintf("stateless-%d", i)
				err := responses.send(nil, nil, []byte(payload), address, packetInfo{})
				stateless <- outcome{payload: payload, err: err}
			}()
		}
		synctest.Wait()
		require.Len(t, public, 1, "eight callers retain their borrow; one is immediately refused")
		refused := <-public
		require.Zero(t, refused.written)
		require.ErrorIs(t, refused.err, ErrMultipathBackpressureV1)
		var expected []string
		refusals := 0
		for range 9 {
			result := <-stateless
			if result.err != nil {
				require.ErrorIs(t, result.err, errEndpointCapacity)
				refusals++
			} else {
				expected = append(expected, result.payload)
			}
		}
		require.Equal(t, 1, refusals)
		close(release)
		for range 8 {
			result := <-public
			require.NoError(t, result.err)
			require.Equal(t, len(result.payload), result.written)
			expected = append(expected, result.payload)
		}
		synctest.Wait()
		require.ElementsMatch(t, expected, sent)
		require.Empty(t, public, "every admitted public call completes once")
	})
}

func TestEndpointAuxiliaryAdmissionRetryFloor(t *testing.T) {
	for _, class := range []string{"public", "stateless"} {
		t.Run(class, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				writes := 0
				e := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
					writes++
					if writes == 1 {
						return 0, syscall.EAGAIN
					}
					return len(data), nil
				}}, time.Second, nil)
				defer func() { <-e.stop() }()
				responses := newEndpointResponses(e)
				transport := &Transport{responses: responses}
				address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
				submit := func() error {
					if class == "stateless" {
						return responses.send(nil, nil, []byte("retry"), address, packetInfo{})
					}
					_, err := transport.WriteTo([]byte("retry"), address)
					return err
				}
				err := submit()
				if class == "public" {
					require.ErrorIs(t, err, syscall.EAGAIN)
				} else {
					require.NoError(t, err)
				}
				synctest.Wait()
				require.ErrorIs(t, submit(), errEndpointCapacity)
				require.Equal(t, 1, writes)
				time.Sleep(endpointRetry)
				require.NoError(t, submit())
				synctest.Wait()
				require.Equal(t, 2, writes)
			})
		})
	}
}
