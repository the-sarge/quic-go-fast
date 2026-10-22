package quic

import (
	"crypto/rand"
	"math"
	"net"
	"slices"
	"time"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

type pathID int64

const invalidPathID pathID = -1

// Maximum number of candidate paths. At capacity, existing candidates still
// receive traffic, while a new candidate needs an expired or idle slot.
const maxPaths = 3

// Idle eviction interval for successful candidates and the minimum lifetime of
// each incomplete validation stage. Ordinary traffic cannot renew a stage.
const pathTimeout = 5 * time.Second

// Challenge records exist only after emission reserves an actual datagram.
const maxPathChallenges = 16

type pathChallengeRecord struct {
	data   [8]byte
	padded bool
}

type path struct {
	id                 pathID
	addr               net.Addr
	lastPacketTime     monotime.Time
	validated          bool
	mtuValidated       bool
	bytesReceived      protocol.ByteCount
	bytesSent          protocol.ByteCount
	rcvdNonProbing     bool
	challenges         [maxPathChallenges]pathChallengeRecord
	numChallenges      int
	baseProbeInterval  time.Duration
	probeInterval      time.Duration
	validationTimeout  time.Duration
	nextProbeTime      monotime.Time
	validationDeadline monotime.Time
}

// Saturate policy times instead of wrapping a duration or monotonic deadline.
func pathDeadline(now monotime.Time, delay time.Duration) monotime.Time {
	if int64(now) > math.MaxInt64-int64(delay) {
		return monotime.Time(math.MaxInt64)
	}
	return now.Add(delay)
}

func (p *path) canProbe(now monotime.Time) bool {
	return !p.mtuValidated && now.Before(p.validationDeadline) && !now.Before(p.nextProbeTime) && p.numChallenges < maxPathChallenges
}

// Called by emission after reservation, before registration and socket I/O.
// Neither loss nor a local write failure rolls back this commitment.
func (p *path) commitChallenge(data [8]byte, size protocol.ByteCount, now monotime.Time) {
	p.challenges[p.numChallenges] = pathChallengeRecord{data: data, padded: size >= protocol.MinInitialPacketSize}
	p.numChallenges++
	// Keep the retry schedule independent of the current stage deadline: a
	// compact response may extend the stage while a padded probe is outstanding.
	p.nextProbeTime = pathDeadline(now, p.probeInterval)
	p.probeInterval = min(time.Duration(math.MaxInt64/2), p.probeInterval) * 2
}

func (p *path) clearChallenges() {
	clear(p.challenges[:])
	p.numChallenges = 0
	p.nextProbeTime = 0
	p.validationDeadline = 0
}

func (p *path) sendAllowance() protocol.ByteCount {
	if p.validated {
		return protocol.MaxByteCount
	}
	return 3*p.bytesReceived - p.bytesSent
}

// Reserve the actual datagram size before recovery registration or socket I/O.
// Failed local writes retain the reservation, conservatively limiting retries.
func (p *path) reserveProbe(size protocol.ByteCount) bool {
	if size > p.sendAllowance() {
		return false
	}
	p.bytesSent += size
	return true
}

type pathManager struct {
	nextPathID pathID
	// ordered by lastPacketTime, with the most recently used path at the end
	paths []*path

	getConnID    func(pathID) (_ protocol.ConnectionID, ok bool)
	retireConnID func(pathID)
	pto          func() time.Duration

	logger utils.Logger
}

func newPathManager(
	getConnID func(pathID) (_ protocol.ConnectionID, ok bool),
	retireConnID func(pathID),
	pto func() time.Duration,
	logger utils.Logger,
) *pathManager {
	return &pathManager{
		paths:        make([]*path, 0, maxPaths+1),
		getConnID:    getConnID,
		retireConnID: retireConnID,
		pto:          pto,
		logger:       logger,
	}
}

// Returns a path challenge frame if one should be sent.
// May return nil.
func (pm *pathManager) HandlePacket(
	remoteAddr net.Addr,
	t monotime.Time,
	size protocol.ByteCount,
	pathChallenge *wire.PathChallengeFrame, // may be nil if the packet didn't contain a PATH_CHALLENGE
	isNonProbing bool,
) (_ protocol.ConnectionID, _ []ackhandler.Frame, shouldSwitch bool) {
	pm.Expire(t)
	var p *path
	for i, path := range pm.paths {
		if addrsEqual(path.addr, remoteAddr) {
			p = path
			p.lastPacketTime = t
			p.bytesReceived += size
			if isNonProbing {
				path.rcvdNonProbing = true
			}
			if pm.logger.Debug() {
				pm.logger.Debugf("received packet for path %s that was already probed, validated: %t", remoteAddr, path.validated)
			}
			shouldSwitch = path.validated && path.mtuValidated && path.rcvdNonProbing
			if i != len(pm.paths)-1 {
				// move the path to the end of the list
				pm.paths = slices.Delete(pm.paths, i, i+1)
				pm.paths = append(pm.paths, p)
			}
			if pathChallenge == nil && !path.canProbe(t) {
				return protocol.ConnectionID{}, nil, shouldSwitch
			}
			break
		}
	}

	if p == nil && len(pm.paths) >= maxPaths {
		// Incomplete attempts have their own absolute deadline. Only successful,
		// idle candidates are eligible for the historical idle eviction rule.
		evict := -1
		for i, candidate := range pm.paths {
			if candidate.mtuValidated && !candidate.lastPacketTime.Add(pathTimeout).After(t) {
				evict = i
				break
			}
		}
		if evict < 0 {
			return protocol.ConnectionID{}, nil, false
		}
		pm.retireConnID(pm.paths[evict].id)
		pm.paths = slices.Delete(pm.paths, evict, evict+1)
	}

	var pathID pathID
	if p != nil {
		pathID = p.id
	} else {
		pathID = pm.nextPathID
	}

	// previously unseen path, initiate path validation by sending a PATH_CHALLENGE
	connID, ok := pm.getConnID(pathID)
	if !ok {
		pm.logger.Debugf("skipping validation of new path %s since no connection ID is available", remoteAddr)
		return protocol.ConnectionID{}, nil, shouldSwitch
	}

	frames := make([]ackhandler.Frame, 0, 2)
	if p == nil {
		base := max(2*utils.DefaultInitialRTT, pm.pto())
		timeout := time.Duration(math.MaxInt64)
		if base <= math.MaxInt64/3 {
			timeout = max(pathTimeout, 3*base)
		}
		p = &path{
			id: pm.nextPathID, addr: remoteAddr, lastPacketTime: t,
			bytesReceived: size, rcvdNonProbing: isNonProbing,
			baseProbeInterval: base, probeInterval: base, validationTimeout: timeout,
			nextProbeTime: t, validationDeadline: pathDeadline(t, timeout),
		}
		pm.nextPathID++
		pm.paths = append(pm.paths, p)
	}
	if p.canProbe(t) {
		var data [8]byte
		rand.Read(data[:])
		frames = append(frames, ackhandler.Frame{
			Frame: &wire.PathChallengeFrame{Data: data}, Handler: emptyHandler{},
		})
	}

	if pathChallenge != nil {
		frames = append(frames, ackhandler.Frame{
			Frame:   &wire.PathResponseFrame{Data: pathChallenge.Data},
			Handler: emptyHandler{},
		})
	}
	return connID, frames, shouldSwitch
}

func (pm *pathManager) HandlePathResponseFrame(f *wire.PathResponseFrame, now monotime.Time) {
	pm.Expire(now)
	for _, p := range pm.paths {
		for _, challenge := range p.challenges[:p.numChallenges] {
			if challenge.data != f.Data {
				continue
			}
			if challenge.padded {
				p.validated = true
				p.mtuValidated = true
				p.clearChallenges()
			} else if !p.validated {
				p.validated = true
				p.validationDeadline = pathDeadline(now, p.validationTimeout)
				hasPadded := slices.ContainsFunc(p.challenges[:p.numChallenges], func(c pathChallengeRecord) bool { return c.padded })
				if !hasPadded {
					p.nextProbeTime = now
					p.probeInterval = p.baseProbeInterval
				}
			}
			return
		}
	}
}

// Expiry is independent of received traffic and send eligibility.
func (pm *pathManager) Expire(now monotime.Time) {
	for i := 0; i < len(pm.paths); {
		p := pm.paths[i]
		if !p.mtuValidated && !now.Before(p.validationDeadline) {
			pm.retireConnID(p.id)
			p.clearChallenges()
			pm.paths = slices.Delete(pm.paths, i, i+1)
		} else {
			i++
		}
	}
}

func (pm *pathManager) NextExpiry() monotime.Time {
	var expiry monotime.Time
	for _, p := range pm.paths {
		if !p.mtuValidated && (expiry.IsZero() || p.validationDeadline.Before(expiry)) {
			expiry = p.validationDeadline
		}
	}
	return expiry
}

// The connection-ID owner handles retirement on terminal connection cleanup.
func (pm *pathManager) Close() {
	for _, p := range pm.paths {
		p.clearChallenges()
	}
	clear(pm.paths)
	pm.paths = pm.paths[:0]
}

func (pm *pathManager) getPath(addr net.Addr) *path {
	for _, p := range pm.paths {
		if addrsEqual(p.addr, addr) {
			return p
		}
	}
	return nil
}

// SwitchToPath is called when the connection switches to a new path
func (pm *pathManager) SwitchToPath(addr net.Addr) {
	// retire all other paths
	for _, path := range pm.paths {
		path.clearChallenges()
		if addrsEqual(path.addr, addr) {
			pm.logger.Debugf("switching to path %d (%s)", path.id, addr)
			continue
		}
		pm.retireConnID(path.id)
	}
	clear(pm.paths)
	pm.paths = pm.paths[:0]
}

func addrsEqual(addr1, addr2 net.Addr) bool {
	if addr1 == nil || addr2 == nil {
		return false
	}
	a1, ok1 := addr1.(*net.UDPAddr)
	a2, ok2 := addr2.(*net.UDPAddr)
	if ok1 && ok2 {
		return a1.IP.Equal(a2.IP) && a1.Port == a2.Port
	}
	return addr1.String() == addr2.String()
}
