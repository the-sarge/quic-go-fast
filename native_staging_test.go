//go:build darwin || linux || freebsd

package quic

import (
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

func TestNativeConstructionReadinessIsLocal(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc := newEmissionTestConnection(t, false)
		e := startNativeEndpoint(endpointTestNative{}, 100*time.Millisecond, nil)
		require.NoError(t, tc.conn.useNativeOrdinary(e, netip.MustParseAddrPort("1.2.3.4:4321"), packetInfo{}))
		sender := tc.conn.emission.local.(*nativeOrdinarySender)
		defer func() { sender.Close(); <-e.stop() }()
		require.Nil(t, tc.conn.emission.capacity())
		var count int
		var demand bool
		e.do(func(s *endpointState) { count, demand = s.count, sender.participant.wait[0].pending })
		require.Zero(t, count, "construction readiness must reserve only connection-local capacity")
		require.False(t, demand, "a readiness query cannot advertise guessed demand")
	})
}

func TestNativeStagedRefusalAndExactTransfer(t *testing.T) {
	for _, finish := range []string{"admit", "lease expiry", "expire", "Retry", "drain", "revoke"} {
		t.Run(finish, func(t *testing.T) {
			nativeOrdinaryTest(t, func(t *testing.T) {
				tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
				c, e := tc.conn, sender.endpoint
				socket.hold = nativeProbeHoldAll
				for range 4 {
					require.NoError(t, e.offer(sender.participant, nativeProbeLane, endpointTestRequest(sender.participant.id, "occupy native")))
				}
				synctest.Wait()
				require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte("frozen local datagram")}))
				result := c.emission.send(monotime.Now(), true)
				require.NoError(t, result.err)
				require.True(t, result.progress, "local capacity permits construction before endpoint admission")
				require.Nil(t, c.datagramQueue.Peek(), "route is frozen before destructive extraction")
				var staged *ordinaryLocalRecord
				for i := range sender.records {
					if sender.records[i].state == ordinaryStaged {
						staged = &sender.records[i]
						break
					}
				}
				require.NotNil(t, staged)
				buffer, deadline := staged.request.members[0].buffer, staged.deadline
				require.Equal(t, 1, buffer.refCount)
				var retained, demandBytes int
				e.do(func(_ *endpointState) {
					retained = sender.participant.retained
					demandBytes = sender.participant.wait[0].bytes
				})
				require.Equal(t, 4, retained, "refusal transfers no extra record")
				require.EqualValues(t, buffer.Cap(), demandBytes, "only exact backing capacity is advertised")
				require.Equal(t, deadline, sender.localDeadline())
				switch finish {
				case "admit", "lease expiry":
					socket.hold = nil
					socket.release()
					synctest.Wait()
					if finish == "lease expiry" {
						_, err := e.exchange(sender.participant, nil)
						require.NoError(t, err)
						time.Sleep(endpointLease + 2*endpointRetry)
					}
					require.NoError(t, sender.consume())
					synctest.Wait()
					require.NoError(t, sender.consume())
					synctest.Wait()
					require.NoError(t, sender.consume())
					require.Zero(t, buffer.refCount)
					require.Len(t, socket.writes, 5)
					require.Contains(t, string(socket.writes[4].data), "frozen local datagram")
					require.Zero(t, sender.droppedDatagrams)
				case "expire":
					c.timer = time.NewTimer(time.Hour)
					defer c.timer.Stop()
					c.handshakeComplete = true
					c.lastPacketReceivedTime = monotime.Now()
					c.idleTimeout = time.Second
					c.blocked = blockModeHardBlocked
					c.maybeResetTimer()
					<-c.timer.C
					require.Equal(t, deadline, monotime.Now(), "the local deadline precedes hard-blocked protocol alarms")
					require.NoError(t, sender.consume())
					require.Zero(t, buffer.refCount)
					require.EqualValues(t, 1, sender.droppedDatagrams)
					require.Zero(t, sender.localDeadline())
				case "Retry":
					sender.reset()
					require.Zero(t, buffer.refCount)
				case "drain":
					sender.drain()
					require.Zero(t, buffer.refCount)
				case "revoke":
					epoch := &responseEpoch{}
					staged.request.epoch = epoch
					epoch.revoked.Store(true)
					require.NoError(t, sender.consume())
					require.Zero(t, buffer.refCount)
				}
				if finish != "admit" && finish != "lease expiry" {
					socket.hold = nil
					socket.release()
					synctest.Wait()
					for _, write := range socket.writes {
						require.NotContains(t, string(write.data), "frozen local datagram", "canceled local payload never reaches the native writer")
					}
				}
			})
		})
	}
}

func TestNativeEmptyCompletionDrainStaysQuiet(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		_, sender, _ := nativeProbeRecoveryConnection(t, "reno")
		sender.participant.completed <- struct{}{}
		require.NoError(t, sender.consume())
		require.NoError(t, sender.consume())
		select {
		case <-sender.participant.completed:
			t.Fatal("empty drain manufactured another completion wake")
		default:
		}
		select {
		case <-sender.connection.sendingScheduled:
			t.Fatal("empty drain scheduled another send")
		default:
		}
	})
}

func TestNativeQuietConsumeDoesNotPollEndpoint(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		_, sender, _ := nativeProbeRecoveryConnection(t, "reno")
		entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() { sender.endpoint.do(func(*endpointState) { close(entered); <-release }) }()
		<-entered
		go func() { _ = sender.consume(); close(finished) }()
		synctest.Wait()
		quiet := false
		select {
		case <-finished:
			quiet = true
		default:
		}
		close(release)
		<-finished
		require.True(t, quiet, "a quiet local drain must not wait for endpoint control")
	})
}

func TestNativeStagedFIFOHeadExpiryWithdrawsExactDemand(t *testing.T) {
	nativeOrdinaryTest(t, func(t *testing.T) {
		tc, sender, socket := nativeProbeRecoveryConnection(t, "reno")
		c, e := tc.conn, sender.endpoint
		socket.hold = nativeProbeHoldAll
		for range 4 {
			require.NoError(t, e.offer(sender.participant, nativeProbeLane, endpointTestRequest(sender.participant.id, "occupy native")))
		}
		synctest.Wait()
		var first, second *ordinaryLocalRecord
		for i, payload := range []string{"old frozen head", "new frozen successor"} {
			require.NoError(t, c.datagramQueue.Add(&wire.DatagramFrame{DataLenPresent: true, Data: []byte(payload)}))
			require.NoError(t, c.emission.send(monotime.Now(), true).err)
			for j := range sender.records {
				r := &sender.records[j]
				if r.state == ordinaryStaged && (first == nil || r.identity.registration > first.identity.registration) {
					if i == 0 {
						first = r
					} else {
						second = r
					}
				}
			}
			if i == 0 {
				time.Sleep(time.Millisecond)
			}
		}
		require.NotNil(t, first)
		require.NotNil(t, second)
		oldBuffer, newBuffer := first.request.members[0].buffer, second.request.members[0].buffer
		var sequence uint64
		e.do(func(*endpointState) { sequence = sender.participant.wait[0].sequence })
		require.Equal(t, first.identity.registration, sequence, "only the oldest frozen head advertises demand")
		time.Sleep(monotime.Until(first.deadline))
		require.NoError(t, sender.consume())
		require.Zero(t, oldBuffer.refCount)
		require.Equal(t, 1, newBuffer.refCount)
		e.do(func(*endpointState) { sequence = sender.participant.wait[0].sequence })
		require.Equal(t, second.identity.registration, sequence, "expiry withdraws the old head before replacement")
		time.Sleep(monotime.Until(second.deadline))
		synctest.Wait()
		require.NoError(t, sender.consume())
		require.Zero(t, newBuffer.refCount)
		var pending bool
		e.do(func(*endpointState) { pending = sender.participant.wait[0].pending })
		require.False(t, pending, "a departed head leaves no stale demand")
		require.EqualValues(t, 2, sender.droppedDatagrams)
	})
}
