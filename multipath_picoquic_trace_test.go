//go:build picoquic

package quic

import (
	"context"
	"sync"

	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
)

type matrixPathTraffic struct {
	Stream, Datagram, Packets uint64
	LastPN                    int64
}

type matrixReplayEvidence struct {
	Decoded, Duplicate, UnknownCID, KeysUnavailable uint64
}

// The existing qlog event owner supplies decoded identities. This recorder
// observes those events; it does not parse or infer QUIC from packet bytes.
type matrixTrace struct {
	mu             sync.Mutex
	sent, received map[uint64]matrixPathTraffic
	ackZero        [2]uint64 // ordinary ACK, explicit PATH_ACK(0), received in 1-RTT
	keyUpdates     uint64
	keyPhaseSeen   bool
	keyPhase       qlog.KeyPhaseBit
	acks           map[uint64]uint64
	replay         map[int]matrixReplayEvidence
	changed        chan struct{}
}

func newMatrixTrace() *matrixTrace {
	return &matrixTrace{sent: make(map[uint64]matrixPathTraffic), received: make(map[uint64]matrixPathTraffic), acks: make(map[uint64]uint64), replay: make(map[int]matrixReplayEvidence), changed: make(chan struct{}, 1)}
}

func (m *matrixTrace) AddProducer() qlogwriter.Recorder { return m }
func (m *matrixTrace) SupportsSchemas(string) bool      { return true }
func (m *matrixTrace) Close() error                     { return nil }
func (m *matrixTrace) tracer(context.Context, bool, ConnectionID) qlogwriter.Trace {
	return m
}

func (m *matrixTrace) RecordEvent(event qlogwriter.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var path *qlog.PathIdentity
	var header qlog.PacketHeader
	var frames []qlog.Frame
	target := m.sent
	switch e := event.(type) {
	case qlog.PacketSent:
		path, header, frames = e.Path, e.Header, e.Frames
	case qlog.PacketReceived:
		path, header, frames, target = e.Path, e.Header, e.Frames, m.received
		// Authenticated packet headers expose remote multipath rotation even
		// when that key owner does not emit a KeyUpdated event.
		if path != nil && path.PathID == 0 && header.PacketType == qlog.PacketType1RTT &&
			(header.KeyPhaseBit == qlog.KeyPhaseZero || header.KeyPhaseBit == qlog.KeyPhaseOne) {
			if m.keyPhaseSeen && header.KeyPhaseBit != m.keyPhase {
				m.keyUpdates++
			}
			m.keyPhaseSeen, m.keyPhase = true, header.KeyPhaseBit
		}
		if path != nil && path.PathID == 1 && header.PacketType == qlog.PacketType1RTT {
			observed := m.replay[e.Raw.Length]
			observed.Decoded++
			m.replay[e.Raw.Length] = observed
		}
		if header.PacketType == qlog.PacketType1RTT {
			for _, frame := range frames {
				if ack, ok := frame.Frame.(*qlog.AckFrame); ok {
					m.acks[uint64(ack.PathID)]++
					if ack.PathID != 0 {
						continue
					}
					if ack.HasPathID {
						m.ackZero[1]++
					} else {
						m.ackZero[0]++
					}
				}
			}
		}
	case qlog.PacketDropped:
		if e.Header.PacketType == qlog.PacketType1RTT {
			observed := m.replay[e.Raw.Length]
			switch e.Trigger {
			case qlog.PacketDropDuplicate:
				observed.Duplicate++
			case qlog.PacketDropUnknownConnectionID:
				observed.UnknownCID++
			case qlog.PacketDropKeyUnavailable:
				observed.KeysUnavailable++
			}
			m.replay[e.Raw.Length] = observed
		}
	}
	if path != nil && header.PacketType == qlog.PacketType1RTT {
		traffic := target[path.PathID]
		traffic.Packets++
		traffic.LastPN = int64(header.PacketNumber)
		for _, frame := range frames {
			switch f := frame.Frame.(type) {
			case *qlog.StreamFrame:
				traffic.Stream += uint64(f.Length)
			case *qlog.DatagramFrame:
				traffic.Datagram += uint64(f.Length)
			}
		}
		target[path.PathID] = traffic
	}
	select {
	case m.changed <- struct{}{}:
	default:
	}
}

func (m *matrixTrace) traffic(id uint64) (matrixPathTraffic, matrixPathTraffic) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sent[id], m.received[id]
}

func (m *matrixTrace) usefulTrafficReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range []uint64{0, 1} {
		sent, received := m.sent[id], m.received[id]
		if sent.Stream == 0 || sent.Datagram == 0 || received.Stream == 0 || received.Datagram == 0 {
			return false
		}
	}
	return true
}

func (m *matrixTrace) wait(ctx context.Context, ready func() bool) error {
	for !ready() {
		select {
		case <-m.changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (m *matrixTrace) replayEvidence(length int) matrixReplayEvidence {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.replay[length]
}

func (m *matrixTrace) replayDisposition(length int, before matrixReplayEvidence) string {
	after := m.replayEvidence(length)
	switch {
	case after.Decoded > before.Decoded:
		return "decoded"
	case after.Duplicate > before.Duplicate:
		return "duplicate"
	case after.UnknownCID > before.UnknownCID:
		return "unknown_connection_id"
	case after.KeysUnavailable > before.KeysUnavailable:
		return "key_unavailable"
	default:
		return ""
	}
}

func (m *matrixTrace) ackCount(id uint64) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acks[id]
}

// The caller owns the finder. The operational fixture calls this only inside
// an existing connection-owner command; unit evidence owns its isolated finder.
func matrixMTUState(finder *mtuFinder) (uint32, bool) {
	if finder == nil {
		return 0, false
	}
	return uint32(finder.CurrentSize()), finder.done()
}
