package quic

import (
	"math"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func newRetryPathManager(t *testing.T, pto time.Duration) (*pathManager, *[]pathID) {
	t.Helper()
	retired := new([]pathID)
	return newPathManager(
		func(id pathID) (protocol.ConnectionID, bool) {
			return protocol.ParseConnectionID([]byte{byte(id + 1)}), true
		},
		func(id pathID) { *retired = append(*retired, id) },
		func() time.Duration { return pto }, utils.DefaultLogger,
	), retired
}

func TestPathManagerRetryTimingAndLoss(t *testing.T) {
	pm, retired := newRetryPathManager(t, time.Millisecond)
	addr := &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 4242}
	now := monotime.Now()
	var tokens [][8]byte
	for _, packet := range []struct {
		at    time.Duration
		probe bool
	}{
		{0, true},
		{199 * time.Millisecond, false},
		{200 * time.Millisecond, true},
		{599 * time.Millisecond, false},
		{600 * time.Millisecond, true},
		{1700 * time.Millisecond, true}, // late: one probe, next due at 3.3s
		{3299 * time.Millisecond, false},
		{3300 * time.Millisecond, true},
	} {
		_, frames, _ := handlePathPacket(t, pm, addr, now.Add(packet.at), 1200, nil, true)
		if !packet.probe {
			require.Empty(t, frames, "at %s", packet.at)
			continue
		}
		require.Len(t, frames, 1)
		token := frames[0].Frame.(*wire.PathChallengeFrame).Data
		require.NotContains(t, tokens, token)
		tokens = append(tokens, token)
		// Later active-path timing changes do not alter this admitted attempt.
		pm.pto = func() time.Duration { return 20 * time.Second }
		frames[0].Handler.OnAcked(frames[0].Frame)
		frames[0].Handler.OnLost(frames[0].Frame)
		require.Empty(t, *retired)
		require.Equal(t, now.Add(5*time.Second), pm.NextExpiry())
	}
	pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: tokens[0]}, now.Add(4*time.Second))
	_, frames, switchPath := pm.HandlePacket(addr, now.Add(4*time.Second), 1200, nil, true)
	require.Empty(t, frames)
	require.True(t, switchPath, "a response to the first challenge survives loss of later probes")
	require.Zero(t, pm.NextExpiry())
}

func TestPathManagerUnsentAndExpiredResponses(t *testing.T) {
	pm, retired := newRetryPathManager(t, 200*time.Millisecond)
	addr := &net.UDPAddr{Port: 4242}
	now := monotime.Now()
	_, frames, _ := pm.HandlePacket(addr, now, 100, nil, true)
	unsent := frames[0].Frame.(*wire.PathChallengeFrame).Data
	pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: unsent}, now)
	require.False(t, pm.getPath(addr).validated)
	_, frames, _ = handlePathPacket(t, pm, addr, now.Add(time.Millisecond), 1200, nil, true)
	token := frames[0].Frame.(*wire.PathChallengeFrame).Data
	firstID := pm.getPath(addr).id
	pm.Expire(now.Add(5*time.Second - time.Nanosecond))
	require.Empty(t, *retired)
	pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: token}, now.Add(5*time.Second))
	require.Nil(t, pm.getPath(addr), "expiry is checked even on the active-path response entrypoint")
	require.Equal(t, []pathID{firstID}, *retired)
	_, frames, _ = pm.HandlePacket(addr, now.Add(5*time.Second), 100, nil, true)
	require.Len(t, frames, 1)
	require.NotEqual(t, firstID, pm.getPath(addr).id)
	require.EqualValues(t, 100, pm.getPath(addr).bytesReceived)
	pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: token}, now.Add(5*time.Second))
	require.False(t, pm.getPath(addr).validated)
	pm.Close()
	require.Empty(t, pm.paths)
	require.Equal(t, []pathID{firstID}, *retired, "terminal connection-ID cleanup belongs to its owner")
}

func TestPathManagerCompactAndPaddedResponses(t *testing.T) {
	for _, paddedBeforeResponse := range []bool{false, true} {
		t.Run(map[bool]string{false: "start MTU stage", true: "keep outstanding padded schedule"}[paddedBeforeResponse], func(t *testing.T) {
			pm, _ := newRetryPathManager(t, 200*time.Millisecond)
			addr := &net.UDPAddr{Port: 4242}
			now := monotime.Now()
			_, frames, _ := pm.HandlePacket(addr, now, 100, nil, true)
			compact := frames[0].Frame.(*wire.PathChallengeFrame).Data
			p := pm.getPath(addr)
			require.True(t, p.reserveProbe(300))
			p.commitChallenge(compact, 300, now)
			var padded [8]byte
			if paddedBeforeResponse {
				_, frames, _ = handlePathPacket(t, pm, addr, now.Add(200*time.Millisecond), 400, nil, true)
				padded = frames[0].Frame.(*wire.PathChallengeFrame).Data
			}
			responseTime := now.Add(300 * time.Millisecond)
			pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: compact}, responseTime)
			require.True(t, p.validated)
			require.False(t, p.mtuValidated)
			deadline := now.Add(5300 * time.Millisecond)
			require.Equal(t, deadline, pm.NextExpiry())
			if paddedBeforeResponse {
				_, frames, _ = pm.HandlePacket(addr, responseTime, 100, nil, true)
				require.Empty(t, frames, "do not duplicate an outstanding padded probe immediately")
				require.Equal(t, now.Add(600*time.Millisecond), p.nextProbeTime)
			} else {
				_, frames, _ = handlePathPacket(t, pm, addr, responseTime, 100, nil, true)
				require.Len(t, frames, 1)
				padded = frames[0].Frame.(*wire.PathChallengeFrame).Data
				require.Equal(t, now.Add(500*time.Millisecond), p.nextProbeTime)
			}
			pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: compact}, now.Add(4*time.Second))
			require.Equal(t, deadline, pm.NextExpiry(), "duplicates cannot renew the MTU stage")
			pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: padded}, now.Add(5*time.Second))
			require.True(t, p.mtuValidated)
			require.Zero(t, p.numChallenges)
			pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: compact}, now.Add(5*time.Second))
			require.True(t, p.mtuValidated)
			require.Zero(t, pm.NextExpiry())
		})
	}
}

func TestPathManagerSlowPathAndCapacity(t *testing.T) {
	pm, retired := newRetryPathManager(t, 2*time.Second)
	now := monotime.Now()
	for i := range maxPaths {
		_, frames, _ := handlePathPacket(t, pm, &net.UDPAddr{Port: 4242 + i}, now, 1200, nil, true)
		require.Len(t, frames, 1)
	}
	_, frames, _ := pm.HandlePacket(&net.UDPAddr{Port: 5000}, now.Add(5*time.Second), 1200, nil, true)
	require.Empty(t, frames, "idle eviction cannot preempt the slow-path deadline")
	require.Empty(t, *retired)
	_, frames, _ = handlePathPacket(t, pm, &net.UDPAddr{Port: 4242}, now.Add(5*time.Second), 1200, nil, true)
	require.Len(t, frames, 1, "existing candidates still retry at capacity")
	pm.Expire(now.Add(6 * time.Second))
	require.Len(t, *retired, maxPaths)
	require.Empty(t, pm.paths)
}

func TestPathManagerTokenCapAndOverflow(t *testing.T) {
	pm, _ := newRetryPathManager(t, time.Duration(math.MaxInt64))
	addr := &net.UDPAddr{Port: 4242}
	now := monotime.Now()
	_, _, _ = pm.HandlePacket(addr, now, 1200, nil, true)
	p := pm.getPath(addr)
	require.Equal(t, monotime.Time(math.MaxInt64), pm.NextExpiry())
	// The chosen timing normally reaches expiry before the cap. Establish the
	// cap precondition directly to exercise its defensive admission boundary.
	p.numChallenges = maxPathChallenges
	p.challenges[maxPathChallenges-1] = pathChallengeRecord{data: [8]byte{1}, padded: true}
	_, frames, _ := pm.HandlePacket(addr, now, 1200, &wire.PathChallengeFrame{Data: [8]byte{2}}, true)
	require.Len(t, frames, 1)
	require.IsType(t, &wire.PathResponseFrame{}, frames[0].Frame)
	pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: [8]byte{1}}, now)
	require.True(t, p.mtuValidated, "cap exhaustion does not evict useful tokens")
	require.Zero(t, pm.NextExpiry())
	// A huge interval and a near-limit timestamp saturate rather than wrap.
	pm2, _ := newRetryPathManager(t, time.Duration(math.MaxInt64))
	_, _, _ = handlePathPacket(t, pm2, addr, monotime.Time(math.MaxInt64-1), 1200, nil, true)
	require.Equal(t, monotime.Time(math.MaxInt64), pm2.getPath(addr).nextProbeTime)
	pm2.Expire(monotime.Time(math.MaxInt64))
	require.Empty(t, pm2.paths)
}

func TestPathManagerSuccessfulCandidateIdleEviction(t *testing.T) {
	pm, retired := newRetryPathManager(t, 2*time.Second)
	now := monotime.Now()
	for i := range maxPaths {
		addr := &net.UDPAddr{Port: 4242 + i}
		_, frames, _ := handlePathPacket(t, pm, addr, now, 1200, nil, false)
		if i == 0 {
			pm.HandlePathResponseFrame(&wire.PathResponseFrame{Data: frames[0].Frame.(*wire.PathChallengeFrame).Data}, now)
		}
	}
	_, frames, _ := pm.HandlePacket(&net.UDPAddr{Port: 5000}, now.Add(5*time.Second), 1200, nil, true)
	require.Len(t, frames, 1)
	require.Equal(t, []pathID{0}, *retired)
	require.Len(t, pm.paths, maxPaths)
}

func TestPathManagerConnectionIDDeferral(t *testing.T) {
	available := false
	pm := newPathManager(func(pathID) (protocol.ConnectionID, bool) { return protocol.ParseConnectionID([]byte{1}), available }, func(pathID) { t.Fatal("unsent path must not retire an ID") }, func() time.Duration { return 200 * time.Millisecond }, utils.DefaultLogger)
	addr := &net.UDPAddr{Port: 4242}
	now := monotime.Now()
	_, frames, _ := pm.HandlePacket(addr, now, 1200, nil, true)
	require.Empty(t, frames)
	require.Empty(t, pm.paths)
	available = true
	_, frames, _ = handlePathPacket(t, pm, addr, now.Add(time.Second), 1200, nil, true)
	require.Len(t, frames, 1)
	require.Equal(t, now.Add(6*time.Second), pm.NextExpiry())
}
