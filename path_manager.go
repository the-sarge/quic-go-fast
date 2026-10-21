package quic

import (
	"crypto/rand"
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

// Maximum number of paths to keep track of.
// If the peer probes another path (before the pathTimeout of an existing path expires),
// this probing attempt is ignored.
const maxPaths = 3

// If no packet is received for a path for pathTimeout,
// the path can be evicted when the peer probes another path.
// This prevents an attacker from churning through paths by duplicating packets and
// sending them with spoofed source addresses.
const pathTimeout = 5 * time.Second

type path struct {
	id              pathID
	addr            net.Addr
	lastPacketTime  monotime.Time
	pathChallenge   [8]byte
	validated       bool
	mtuValidated    bool
	challengeSent   bool
	challengePadded bool
	bytesReceived   protocol.ByteCount
	bytesSent       protocol.ByteCount
	rcvdNonProbing  bool
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

	logger utils.Logger
}

func newPathManager(
	getConnID func(pathID) (_ protocol.ConnectionID, ok bool),
	retireConnID func(pathID),
	logger utils.Logger,
) *pathManager {
	return &pathManager{
		paths:        make([]*path, 0, maxPaths+1),
		getConnID:    getConnID,
		retireConnID: retireConnID,
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
	var p *path
	for i, path := range pm.paths {
		if addrsEqual(path.addr, remoteAddr) {
			p = path
			p.lastPacketTime = t
			p.bytesReceived += size
			// already sent a PATH_CHALLENGE for this path
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
			if pathChallenge == nil && path.challengeSent && (!path.validated || path.challengePadded) {
				return protocol.ConnectionID{}, nil, shouldSwitch
			}
			break
		}
	}

	if p == nil && len(pm.paths) >= maxPaths {
		if pm.paths[0].lastPacketTime.Add(pathTimeout).After(t) {
			if pm.logger.Debug() {
				pm.logger.Debugf("received packet for previously unseen path %s, but already have %d paths", remoteAddr, len(pm.paths))
			}
			return protocol.ConnectionID{}, nil, shouldSwitch
		}
		// evict the oldest path, if the last packet was received more than pathTimeout ago
		pm.retireConnID(pm.paths[0].id)
		pm.paths = pm.paths[1:]
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
		var pathChallengeData [8]byte
		rand.Read(pathChallengeData[:])
		p = &path{
			id:             pm.nextPathID,
			addr:           remoteAddr,
			lastPacketTime: t,
			bytesReceived:  size,
			rcvdNonProbing: isNonProbing,
			pathChallenge:  pathChallengeData,
		}
		pm.nextPathID++
		pm.paths = append(pm.paths, p)
	}
	// A compact challenge validates reachability, but RFC 9000 section 8.2.3
	// requires another, expanded challenge before the path MTU is validated.
	if p.validated && !p.challengePadded && p.challengeSent {
		rand.Read(p.pathChallenge[:])
		p.challengeSent = false
	}
	if !p.challengeSent {
		frames = append(frames, ackhandler.Frame{
			Frame:   &wire.PathChallengeFrame{Data: p.pathChallenge},
			Handler: (*pathManagerAckHandler)(pm),
		})
		p.challengeSent = true
		p.challengePadded = p.sendAllowance() >= protocol.MinInitialPacketSize
		pm.logger.Debugf("enqueueing PATH_CHALLENGE for new path %s", remoteAddr)
	}
	if pathChallenge != nil {
		frames = append(frames, ackhandler.Frame{
			Frame:   &wire.PathResponseFrame{Data: pathChallenge.Data},
			Handler: (*pathManagerAckHandler)(pm),
		})
	}
	return connID, frames, shouldSwitch
}

func (pm *pathManager) HandlePathResponseFrame(f *wire.PathResponseFrame) {
	for _, p := range pm.paths {
		if p.challengeSent && f.Data == p.pathChallenge {
			// path validated
			p.validated = true
			p.mtuValidated = p.challengePadded
			pm.logger.Debugf("path %s validated", p.addr)
			break
		}
	}
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
		if addrsEqual(path.addr, addr) {
			pm.logger.Debugf("switching to path %d (%s)", path.id, addr)
			continue
		}
		pm.retireConnID(path.id)
	}
	clear(pm.paths)
	pm.paths = pm.paths[:0]
}

type pathManagerAckHandler pathManager

var _ ackhandler.FrameHandler = &pathManagerAckHandler{}

// Acknowledging the frame doesn't validate the path, only receiving the PATH_RESPONSE does.
func (pm *pathManagerAckHandler) OnAcked(f wire.Frame) {}

func (pm *pathManagerAckHandler) OnLost(f wire.Frame) {
	pc, ok := f.(*wire.PathChallengeFrame)
	if !ok {
		return
	}
	for i, path := range pm.paths {
		if path.pathChallenge == pc.Data {
			pm.paths = slices.Delete(pm.paths, i, i+1)
			pm.retireConnID(path.id)
			break
		}
	}
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
