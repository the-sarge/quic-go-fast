//go:build darwin || linux || freebsd

package quic

import (
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNativeEndpointSparseTraversal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := startNativeEndpoint(endpointTestNative{}, time.Second, nil)
		defer func() { <-e.stop() }()
		var participants []*endpointParticipant
		for id := uint64(1); id <= 6; id++ {
			p, err := e.attach(id)
			require.NoError(t, err)
			participants = append(participants, p)
		}
		for _, i := range []int{0, 2, 4} {
			<-e.detach(participants[i])
		}
		for _, test := range []struct {
			cursor int
			slots  []int
		}{
			{0, []int{1, 3, 5, 4096, 4097}},
			{3, []int{3, 5, 4096, 4097, 1}},
			{4, []int{5, 4096, 4097, 1, 3}},
			{4097, []int{4097, 1, 3, 5, 4096}},
		} {
			e.do(func(s *endpointState) {
				var slots []int
				for p := range s.occupiedParticipants(test.cursor) {
					require.Same(t, s.participants[p.slot], p)
					slots = append(slots, p.slot)
				}
				require.Equal(t, test.slots, slots)
				for p := range s.occupiedParticipants(test.cursor) {
					require.Equal(t, test.slots[0], p.slot)
					break
				}
			})
		}
	})
}

func TestNativeEndpointSparseClassAndPathTurns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		permits := make(chan struct{})
		started := make(chan string, 16)
		e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) {
			started <- string(b)
			<-permits
			return len(b), nil
		}}, time.Second, nil)
		defer func() { close(permits); <-e.stop() }()
		var participants []*endpointParticipant
		for id := uint64(1); id <= 6; id++ {
			p, err := e.attach(id)
			require.NoError(t, err)
			participants = append(participants, p)
		}
		for _, i := range []int{0, 2, 4} {
			<-e.detach(participants[i])
		}
		p, peer, control := participants[1], participants[3], participants[5]
		require.NoError(t, e.offer(p, 0, endpointTestRequest(p.id, "initial")))
		require.Equal(t, "initial", <-started)
		require.NoError(t, e.offer(p, 0, endpointTestRequest(p.id, "path0")))
		require.NoError(t, e.offer(p, 1, endpointTestRequest(p.id, "path1")))
		require.NoError(t, e.offer(peer, 0, endpointTestRequest(peer.id, "peer")))
		for _, data := range []string{"c1", "c2", "c3"} {
			require.NoError(t, e.offer(control, 2, endpointTestRequest(control.id, data)))
		}
		for _, data := range []string{"s1", "s2"} {
			require.NoError(t, e.offer(e.stateless, 0, endpointTestRequest(0, data)))
		}
		for _, data := range []string{"u1", "u2"} {
			require.NoError(t, e.offer(e.public, 0, endpointTestRequest(0, data)))
		}
		for _, expected := range []string{"c1", "s1", "peer", "u1", "c2", "path1", "s2", "u2", "path0", "c3"} {
			permits <- struct{}{}
			require.Equal(t, expected, <-started)
		}
		permits <- struct{}{}
	})
}

func TestNativeEndpointSparseRetryDeadlines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := startNativeEndpoint(endpointTestNative{send: func([]byte) (int, error) {
			return 0, syscall.EAGAIN
		}}, time.Second, nil)
		defer func() { <-e.stop() }()
		var participants []*endpointParticipant
		for id := uint64(1); id <= 5; id++ {
			p, err := e.attach(id)
			require.NoError(t, err)
			participants = append(participants, p)
		}
		for _, i := range []int{0, 2, 3} {
			<-e.detach(participants[i])
		}
		first, later := participants[4], participants[1]
		require.NoError(t, e.offer(first, 1, endpointTestRequest(first.id, "first")))
		synctest.Wait()
		_, ok := e.take(first)
		require.True(t, ok)
		time.Sleep(endpointRetry / 2)
		require.NoError(t, e.offer(later, 0, endpointTestRequest(later.id, "later")))
		synctest.Wait()
		_, ok = e.take(later)
		require.True(t, ok)
		_, err := e.prepare(first, 1, 1)
		require.ErrorIs(t, err, errEndpointCapacity)
		_, err = e.prepare(later, 0, 1)
		require.ErrorIs(t, err, errEndpointCapacity)
		time.Sleep(endpointRetry / 2)
		synctest.Wait()
		select {
		case <-first.ready:
		default:
			t.Fatal("earliest retry must wake without another producer operation")
		}
		select {
		case <-later.ready:
			t.Fatal("later retry woke before its floor")
		default:
		}
		a, err := e.prepare(first, 1, 1)
		require.NoError(t, err)
		e.do(func(s *endpointState) { s.cancelAdmission(a) })
		time.Sleep(endpointRetry / 2)
		synctest.Wait()
		select {
		case <-later.ready:
		default:
			t.Fatal("sparse retry traversal lost the next deadline")
		}
		a, err = e.prepare(later, 0, 1)
		require.NoError(t, err)
		e.do(func(s *endpointState) { s.cancelAdmission(a) })
	})
}

func TestNativeEndpointSparseRetirementReuse(t *testing.T) {
	for _, mode := range []string{"revoke", "drain"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) {
					if string(b) == "current" {
						<-release
					}
					return len(b), nil
				}}, time.Second, nil)
				defer func() { <-e.stop() }()
				p, err := e.attach(1)
				require.NoError(t, err)
				require.NoError(t, e.offer(p, 0, endpointTestRequest(1, "terminal")))
				synctest.Wait() // keep the terminal result unconsumed
				require.NoError(t, e.offer(p, 1, endpointTestRequest(1, "current")))
				synctest.Wait()
				if mode == "drain" {
					e.drain(p)
				}
				retired := e.detach(p)
				e.do(func(s *endpointState) {
					var members []*endpointParticipant
					for member := range s.occupiedParticipants(0) {
						members = append(members, member)
					}
					require.Equal(t, []*endpointParticipant{p, e.stateless, e.public}, members)
					require.Equal(t, 1, p.retained, "only the actual native borrow remains")
				})
				_, err = e.attach(1)
				require.ErrorIs(t, err, errEndpointCapacity)
				close(release)
				<-retired
				fresh, err := e.attach(1)
				require.NoError(t, err)
				require.Equal(t, p.slot, fresh.slot)
				e.do(func(s *endpointState) {
					var members []*endpointParticipant
					for member := range s.occupiedParticipants(0) {
						members = append(members, member)
					}
					require.Equal(t, []*endpointParticipant{fresh, e.stateless, e.public}, members)
				})
				_, err = e.prepare(p, 0, 1)
				require.Error(t, err, "released identity cannot use the reused slot")
				a, err := e.prepare(fresh, 0, 1)
				require.NoError(t, err)
				e.do(func(s *endpointState) { s.cancelAdmission(a) })
				<-e.detach(fresh)
			})
		})
	}
}

func TestNativeEndpointSparseSealCleanup(t *testing.T) {
	for _, mode := range []string{"seal", "fault"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				release := make(chan struct{})
				e := startNativeEndpoint(endpointTestNative{send: func(b []byte) (int, error) {
					<-release
					return len(b), nil
				}}, time.Millisecond, nil)
				var remaining []*endpointParticipant
				e.do(func(s *endpointState) {
					e.stopped = func() {
						for p := range s.occupiedParticipants(0) {
							remaining = append(remaining, p)
						}
					}
				})
				var participants []*endpointParticipant
				for id := uint64(1); id <= 4; id++ {
					p, err := e.attach(id)
					require.NoError(t, err)
					participants = append(participants, p)
				}
				<-e.detach(participants[0])
				current := participants[3]
				require.NoError(t, e.offer(current, 0, endpointTestRequest(current.id, "current")))
				synctest.Wait()
				require.NoError(t, e.offer(participants[1], 0, endpointTestRequest(participants[1].id, "queued")))
				if mode == "fault" {
					time.Sleep(time.Millisecond + endpointTolerance)
					<-e.fault
				}
				e.seal()
				e.do(func(s *endpointState) {
					var members []*endpointParticipant
					for p := range s.occupiedParticipants(0) {
						members = append(members, p)
					}
					require.Equal(t, []*endpointParticipant{current}, members, "seal retires every slot except the native borrow")
				})
				close(release)
				<-e.done
				require.Empty(t, remaining, "native return must remove the final canonical membership")
			})
		})
	}
}
