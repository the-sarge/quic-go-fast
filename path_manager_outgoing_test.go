package quic

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"

	"github.com/stretchr/testify/require"
)

func TestPathManagerOutgoingPathProbing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connIDs := []protocol.ConnectionID{
			protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8}),
		}
		pm := newPathManagerOutgoing(
			func(id pathID) (protocol.ConnectionID, bool) {
				connID := connIDs[0]
				connIDs = connIDs[1:]
				return connID, true
			},
			func(id pathID) { t.Fatal("didn't expect any connection ID to be retired") },
			func() {},
		)

		_, _, _, ok := pm.NextPathToProbe()
		require.False(t, ok)

		tr1 := &Transport{}
		var enabled bool
		p := pm.NewPath(tr1, time.Second, func() { enabled = true })
		require.ErrorIs(t, p.Switch(), ErrPathNotValidated)

		errChan := make(chan error, 1)
		go func() { errChan <- p.Probe(context.Background()) }()

		// wait for the path to be queued for probing
		synctest.Wait()

		require.False(t, enabled)
		connID, f, tr, ok := pm.NextPathToProbe()
		require.True(t, ok)
		require.Equal(t, tr1, tr)
		require.Equal(t, protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8}), connID)
		require.IsType(t, &wire.PathChallengeFrame{}, f.Frame)
		pc := f.Frame.(*wire.PathChallengeFrame)
		require.True(t, enabled)

		_, _, _, ok = pm.NextPathToProbe()
		require.False(t, ok)

		select {
		case <-errChan:
			t.Fatal("should still be probing")
		default:
		}

		// acking the frame doesn't complete path validation...
		f.Handler.OnAcked(f.Frame)
		select {
		case <-errChan:
			t.Fatal("should still be probing")
		default:
		}

		require.ErrorIs(t, p.Switch(), ErrPathNotValidated)
		_, ok = pm.ShouldSwitchPath()
		require.False(t, ok)

		// ... neither does receiving a random PATH_RESPONSE...
		pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: [8]byte{'f', 'o', 'o', 'f', 'o', 'o'}})
		f.Handler.OnAcked(f.Frame) // doesn't do anything
		f.Handler.OnLost(f.Frame)  // doesn't do anything
		select {
		case <-errChan:
			t.Fatal("should still be probing")
		default:
		}

		// ... only receiving the corresponding PATH_RESPONSE does
		pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: pc.Data})

		synctest.Wait()

		select {
		case err := <-errChan:
			require.NoError(t, err)
		default:
			t.Fatal("timeout")
		}

		// receiving it multiple times is ok
		pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: pc.Data})

		// now switch to the other path
		_, ok = pm.ShouldSwitchPath()
		require.False(t, ok)
		require.NoError(t, p.Switch())
		// the active path can't be closed
		require.EqualError(t, p.Close(), "cannot close active path")
		switchToTransport, ok := pm.ShouldSwitchPath()
		require.True(t, ok)
		require.Equal(t, tr1, switchToTransport)
	})
}

func TestPathManagerOutgoingRepeatedSuccessfulProbes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pm := newPathManagerOutgoing(
			func(pathID) (protocol.ConnectionID, bool) {
				return protocol.ParseConnectionID([]byte{1, 2, 3, 4}), true
			},
			func(pathID) { t.Fatal("didn't expect any connection ID to be retired") },
			func() {},
		)
		tr := &Transport{}
		p := pm.NewPath(tr, time.Second, func() {})
		var previousResponse *wire.PathResponseFrame
		for attempt := range 2 {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			errChan := make(chan error, 1)
			go func() { errChan <- p.Probe(ctx) }()
			synctest.Wait()

			_, f, _, ok := pm.NextPathToProbe()
			require.True(t, ok)
			if attempt > 0 {
				// A previous success still permits switching during a new probe.
				require.NoError(t, p.Switch())
				pm.HandlePathResponseFrame(previousResponse)
			}
			synctest.Wait()
			select {
			case err := <-errChan:
				t.Fatalf("probe %d completed before its response: %v", attempt+1, err)
			default:
			}

			response := &wire.PathResponseFrame{Data: f.Frame.(*wire.PathChallengeFrame).Data}
			pm.HandlePathResponseFrame(response)
			pm.HandlePathResponseFrame(response) // duplicate responses are harmless
			synctest.Wait()
			select {
			case err := <-errChan:
				require.NoError(t, err)
			default:
				t.Fatalf("probe %d did not complete after its response", attempt+1)
			}
			require.NoError(t, p.Switch())
			switchToTransport, ok := pm.ShouldSwitchPath()
			require.True(t, ok)
			require.Same(t, tr, switchToTransport)
			require.EqualError(t, p.Close(), "cannot close active path")
			previousResponse = response
		}
	})
}

func TestPathManagerOutgoingReprobeDiscardsOldRetransmissions(t *testing.T) {
	for _, state := range []string{"queued", "sent"} {
		t.Run(state, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pm := newPathManagerOutgoing(
					func(pathID) (protocol.ConnectionID, bool) {
						return protocol.ParseConnectionID([]byte{1, 2, 3, 4}), true
					},
					func(pathID) { t.Fatal("didn't expect any connection ID to be retired") },
					func() {},
				)
				p := pm.NewPath(&Transport{}, time.Second, func() {})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				errChan := make(chan error, 1)
				go func() { errChan <- p.Probe(ctx) }()
				synctest.Wait()
				_, f, _, ok := pm.NextPathToProbe()
				require.True(t, ok)
				synctest.Wait()

				// Queue a retransmission just before the first probe succeeds.
				time.Sleep(time.Second)
				synctest.Wait()
				pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: f.Frame.(*wire.PathChallengeFrame).Data})
				synctest.Wait()
				require.NoError(t, <-errChan)

				var oldResponse *wire.PathResponseFrame
				if state == "sent" {
					_, retry, _, ok := pm.NextPathToProbe()
					require.True(t, ok)
					oldResponse = &wire.PathResponseFrame{Data: retry.Frame.(*wire.PathChallengeFrame).Data}
				}

				go func() { errChan <- p.Probe(ctx) }()
				synctest.Wait()
				_, f, _, ok = pm.NextPathToProbe()
				require.True(t, ok)
				_, _, _, ok = pm.NextPathToProbe()
				require.False(t, ok, "only the new probe should remain queued")
				if oldResponse != nil {
					pm.HandlePathResponseFrame(oldResponse)
				}
				synctest.Wait()
				select {
				case err := <-errChan:
					t.Fatalf("old retransmission completed the new probe: %v", err)
				default:
				}
				pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: f.Frame.(*wire.PathChallengeFrame).Data})
				synctest.Wait()
				require.NoError(t, <-errChan)
			})
		})
	}
}

func TestPathManagerOutgoingRetransmissions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connIDs := []protocol.ConnectionID{
			protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8}),
			protocol.ParseConnectionID([]byte{2, 3, 4, 5, 6, 7, 8, 9}),
		}
		var retiredConnIDs []protocol.ConnectionID
		scheduledSending := make(chan struct{}, 20)
		pm := newPathManagerOutgoing(
			func(id pathID) (protocol.ConnectionID, bool) { return connIDs[id], true },
			func(id pathID) { retiredConnIDs = append(retiredConnIDs, connIDs[id]) },
			func() { scheduledSending <- struct{}{} },
		)

		_, _, _, ok := pm.NextPathToProbe()
		require.False(t, ok)

		tr1 := &Transport{}
		const initialRTT = 5 * time.Millisecond
		p := pm.NewPath(tr1, initialRTT, func() {})

		pathChallengeChan := make(chan [8]byte)
		done := make(chan struct{})
		defer close(done)
		go func() {
			for {
				select {
				case <-scheduledSending:
				case <-done:
					return
				}
				_, f, _, ok := pm.NextPathToProbe()
				if !ok {
					// should never happen
					pathChallengeChan <- [8]byte{}
					continue
				}
				pathChallengeChan <- f.Frame.(*wire.PathChallengeFrame).Data
			}
		}()

		errChan := make(chan error, 1)
		go func() { errChan <- p.Probe(context.Background()) }()

		start := time.Now()
		type result struct {
			pc   *[8]byte
			took time.Duration
		}
		var results []result
		for range 4 {
			select {
			case <-errChan:
				t.Fatal("probing should not have completed")
			case pc := <-pathChallengeChan:
				results = append(results, result{pc: &pc, took: time.Since(start)})
			case <-time.After(time.Second):
				t.Fatal("timeout")
			}
		}

		for i, r1 := range results {
			require.NotNil(t, r1.pc)
			if i > 0 {
				took := r1.took - results[i-1].took
				t.Log("took", took)
				require.Equal(t, took, initialRTT<<(i-1))
			}
			for j, r2 := range results {
				if i == j {
					continue
				}
				require.NotEqual(t, r1.pc, r2.pc)
			}
		}

		// receiving a PATH_RESPONSE for any of the PATH_CHALLENGES completes path validation
		pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: *results[2].pc})

		synctest.Wait()

		select {
		case err := <-errChan:
			require.NoError(t, err)
		default:
			t.Fatal("probing should have completed")
		}

		// It is valid to probe again
		results = results[:0]
		ctx, cancel := context.WithCancel(context.Background())
		go func() { errChan <- p.Probe(ctx) }()

		synctest.Wait()

		for range 2 {
			select {
			case err := <-errChan:
				require.NoError(t, err)
			case pc := <-pathChallengeChan:
				results = append(results, result{pc: &pc, took: time.Since(start)})
			case <-time.After(time.Second):
				t.Fatal("should have received a path challenge")
			}
		}
		// this time, don't receive a PATH_RESPONSE
		cancel()
		synctest.Wait()
		select {
		case err := <-errChan:
			require.ErrorIs(t, err, context.Canceled)
		default:
			t.Fatal("should have received a context canceled error")
		}
	})
}

func TestPathManagerOutgoingCloseBeforeProbe(t *testing.T) {
	var connIDRequests, scheduledSends int
	pm := newPathManagerOutgoing(
		func(pathID) (protocol.ConnectionID, bool) {
			connIDRequests++
			return protocol.ParseConnectionID([]byte{1, 2, 3, 4}), true
		},
		func(pathID) { t.Fatal("didn't expect any connection ID to be retired") },
		func() { scheduledSends++ },
	)
	var enabled bool
	p := pm.NewPath(&Transport{}, time.Second, func() { enabled = true })

	require.NoError(t, p.Close())
	scheduledAfterClose := scheduledSends
	require.ErrorIs(t, p.Probe(context.Background()), ErrPathClosed)
	require.Equal(t, scheduledAfterClose, scheduledSends, "probing a closed path must not schedule sending")
	_, _, _, ok := pm.NextPathToProbe()
	require.False(t, ok)
	require.Zero(t, connIDRequests)
	require.False(t, enabled)
}

func TestPathManagerOutgoingConcurrentClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		retireStarted := make(chan struct{})
		allowRetire := make(chan struct{})
		var retireCount int
		pm := newPathManagerOutgoing(
			func(pathID) (protocol.ConnectionID, bool) {
				return protocol.ParseConnectionID([]byte{1, 2, 3, 4}), true
			},
			func(pathID) {
				retireCount++
				close(retireStarted)
				<-allowRetire
			},
			func() {},
		)
		p := pm.NewPath(&Transport{}, time.Second, func() {})
		probeResult := make(chan error, 1)
		go func() { probeResult <- p.Probe(context.Background()) }()
		synctest.Wait()

		type closeOutcome struct {
			err       error
			recovered any
		}
		closeResult := make(chan closeOutcome, 2)
		closePath := func() {
			var outcome closeOutcome
			defer func() {
				outcome.recovered = recover()
				closeResult <- outcome
			}()
			outcome.err = p.Close()
		}
		go closePath()
		<-retireStarted
		secondStarted := make(chan struct{})
		go func() {
			close(secondStarted)
			closePath()
		}()
		<-secondStarted
		runtime.Gosched()
		close(allowRetire)
		synctest.Wait()

		for range 2 {
			outcome := <-closeResult
			require.Nil(t, outcome.recovered, "Close panicked")
			require.NoError(t, outcome.err)
		}
		require.Equal(t, 1, retireCount)
		require.ErrorIs(t, <-probeResult, ErrPathClosed)
	})
}

func TestPathManagerOutgoingCloseWinsRetryAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		retireStarted := make(chan struct{})
		allowRetire := make(chan struct{})
		var scheduledSends atomic.Int32
		pm := newPathManagerOutgoing(
			func(pathID) (protocol.ConnectionID, bool) {
				return protocol.ParseConnectionID([]byte{1, 2, 3, 4}), true
			},
			func(pathID) {
				close(retireStarted)
				<-allowRetire
			},
			func() { scheduledSends.Add(1) },
		)
		p := pm.NewPath(&Transport{}, time.Second, func() {})
		probeResult := make(chan error, 1)
		go func() { probeResult <- p.Probe(context.Background()) }()
		synctest.Wait()
		_, _, _, ok := pm.NextPathToProbe()
		require.True(t, ok)
		synctest.Wait()

		closeResult := make(chan error, 1)
		go func() { closeResult <- p.Close() }()
		<-retireStarted
		time.Sleep(time.Second)
		runtime.Gosched()
		close(allowRetire)
		synctest.Wait()

		require.NoError(t, <-closeResult)
		require.ErrorIs(t, <-probeResult, ErrPathClosed)
		require.EqualValues(t, 2, scheduledSends.Load(), "a rejected retry must not schedule sending")
		_, _, _, ok = pm.NextPathToProbe()
		require.False(t, ok)
	})
}

func TestPathManagerOutgoingCloseDiscardsAdmittedRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var scheduledSends atomic.Int32
		pm := newPathManagerOutgoing(
			func(pathID) (protocol.ConnectionID, bool) {
				return protocol.ParseConnectionID([]byte{1, 2, 3, 4}), true
			},
			func(pathID) {},
			func() { scheduledSends.Add(1) },
		)
		p := pm.NewPath(&Transport{}, time.Second, func() {})
		probeResult := make(chan error, 1)
		go func() { probeResult <- p.Probe(context.Background()) }()
		synctest.Wait()
		_, _, _, ok := pm.NextPathToProbe()
		require.True(t, ok)
		synctest.Wait()

		time.Sleep(time.Second)
		synctest.Wait()
		require.EqualValues(t, 2, scheduledSends.Load())
		require.NoError(t, p.Close())
		synctest.Wait()

		require.ErrorIs(t, <-probeResult, ErrPathClosed)
		require.EqualValues(t, 3, scheduledSends.Load())
		_, _, _, ok = pm.NextPathToProbe()
		require.False(t, ok)
	})
}

func TestPathManagerOutgoingCloseWakesOverlappingProbes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pm := newPathManagerOutgoing(
			func(pathID) (protocol.ConnectionID, bool) {
				return protocol.ParseConnectionID([]byte{1, 2, 3, 4}), true
			},
			func(pathID) {},
			func() {},
		)
		p := pm.NewPath(&Transport{}, time.Second, func() {})
		probeResults := make(chan error, 2)
		go func() { probeResults <- p.Probe(context.Background()) }()
		synctest.Wait()
		_, _, _, ok := pm.NextPathToProbe()
		require.True(t, ok)
		synctest.Wait()

		go func() { probeResults <- p.Probe(context.Background()) }()
		synctest.Wait()
		_, _, _, ok = pm.NextPathToProbe()
		require.True(t, ok)
		require.NoError(t, p.Close())
		synctest.Wait()

		for range 2 {
			require.ErrorIs(t, <-probeResults, ErrPathClosed)
		}
		_, _, _, ok = pm.NextPathToProbe()
		require.False(t, ok)
	})
}

func TestPathManagerOutgoingCanceledProbeCanBeRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pm := newPathManagerOutgoing(
			func(pathID) (protocol.ConnectionID, bool) {
				return protocol.ParseConnectionID([]byte{1, 2, 3, 4}), true
			},
			func(pathID) { t.Fatal("didn't expect any connection ID to be retired") },
			func() {},
		)
		p := pm.NewPath(&Transport{}, time.Second, func() {})
		probeResults := make(chan error, 1)
		ctx, cancel := context.WithCancel(context.Background())
		go func() { probeResults <- p.Probe(ctx) }()
		synctest.Wait()
		_, _, _, ok := pm.NextPathToProbe()
		require.True(t, ok)
		cancel()
		synctest.Wait()
		require.ErrorIs(t, <-probeResults, context.Canceled)

		go func() { probeResults <- p.Probe(context.Background()) }()
		synctest.Wait()
		_, f, _, ok := pm.NextPathToProbe()
		require.True(t, ok)
		pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: f.Frame.(*wire.PathChallengeFrame).Data})
		synctest.Wait()
		require.NoError(t, <-probeResults)
		require.NoError(t, p.Switch())
	})
}

func TestPathManagerOutgoingAbandonPath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connIDs := []protocol.ConnectionID{
			protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8}),
		}
		var retiredPaths []pathID
		pm := newPathManagerOutgoing(
			func(id pathID) (protocol.ConnectionID, bool) {
				connID := connIDs[0]
				connIDs = connIDs[1:]
				return connID, true
			},
			func(id pathID) { retiredPaths = append(retiredPaths, id) },
			func() {},
		)

		// path abandoned before the PATH_CHALLENGE is sent out
		p1 := pm.NewPath(&Transport{}, time.Second, func() {})
		errChan := make(chan error, 1)
		go func() { errChan <- p1.Probe(context.Background()) }()

		// wait for the path to be queued for probing
		synctest.Wait()

		require.NoError(t, p1.Close())
		// closing the path multiple times is ok
		require.NoError(t, p1.Close())
		require.NoError(t, p1.Close())
		_, _, _, ok := pm.NextPathToProbe()
		require.False(t, ok)

		synctest.Wait()

		select {
		case err := <-errChan:
			require.ErrorIs(t, err, ErrPathClosed)
		default:
			t.Fatal("should have received a path closed error")
		}
		require.Equal(t, []pathID{p1.id}, retiredPaths)

		p2 := pm.NewPath(&Transport{}, time.Second, func() {})
		go func() { errChan <- p2.Probe(context.Background()) }()

		// wait for the path to be queued for probing
		synctest.Wait()

		connID, f, _, ok := pm.NextPathToProbe()
		require.True(t, ok)
		require.Equal(t, protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8}), connID)

		require.NoError(t, p2.Close())
		require.Equal(t, []pathID{p1.id, p2.id}, retiredPaths)
		pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: f.Frame.(*wire.PathChallengeFrame).Data})
		_, _, _, ok = pm.NextPathToProbe()
		require.False(t, ok)
		// it's not possible to switch to an abandoned path
		require.ErrorIs(t, p2.Switch(), ErrPathClosed)
	})
}

// TestPathManagerOutgoingAbandonValidatedPath tests that closing a path that has
// already been validated retires the connection ID allocated to it.
// TestPathManagerOutgoingAbandonPath covers closing a path before validation,
// which is a different case: pathChallenges is cleared once the path validates.
func TestPathManagerOutgoingAbandonValidatedPath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		connIDs := []protocol.ConnectionID{
			protocol.ParseConnectionID([]byte{1, 2, 3, 4, 5, 6, 7, 8}),
		}
		var retiredPaths []pathID
		pm := newPathManagerOutgoing(
			func(id pathID) (protocol.ConnectionID, bool) {
				if len(connIDs) == 0 {
					return protocol.ConnectionID{}, false
				}
				connID := connIDs[0]
				connIDs = connIDs[1:]
				return connID, true
			},
			func(id pathID) { retiredPaths = append(retiredPaths, id) },
			func() {},
		)

		p := pm.NewPath(&Transport{}, time.Second, func() {})
		errChan := make(chan error, 1)
		go func() { errChan <- p.Probe(context.Background()) }()
		synctest.Wait()

		_, f, _, ok := pm.NextPathToProbe()
		require.True(t, ok)

		// Validate the path before abandoning it. This is the ordering an
		// application produces: probe, use the path, and abandon it later.
		pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: f.Frame.(*wire.PathChallengeFrame).Data})
		synctest.Wait()
		require.NoError(t, <-errChan)

		require.NoError(t, p.Close())
		require.Equal(t, []pathID{p.id}, retiredPaths,
			"the connection ID of a validated path must be retired when the path is abandoned")
	})
}
