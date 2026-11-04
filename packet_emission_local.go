package quic

import (
	"net"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

type localPacketSender interface {
	sender
	register(ackhandler.LocalCompletionToken, bool)
	registerMTUProbe(*mtuFinderAckHandler)
	probeTargetReady(*Transport) (bool, error)
	sendProbe(*packetBuffer, *Transport, net.Addr, packetInfo, *responseEpoch) error
	probesPending(*Transport, *responseEpoch) bool
	useRoute(*pathRoute)
	holdReadiness()
	releaseReadiness()
	routeUsable(*Transport) bool
	finish()
	drain()
	sendClose(*packetBuffer, protocol.ECN) error
	sendPathClose(*packetBuffer, protocol.ECN, *pathRoute) error
	closeUsable(*Transport) bool
	routeInfo() packetInfo
	consume() error
	reset()
	completed() <-chan struct{}
	faulted() <-chan struct{}
	reject()
	replacePath(*Transport, sendConn) sendConn
	moveZero(*Transport, net.Addr, packetInfo, sendConn) sendConn
	rebindZero(net.Addr, packetInfo, sendConn)
	resetWriteState(*pathRoute)
	detachReady(*Transport) bool
	detachTransport(*Transport)
}

// localRouteConn marks a send connection that writes only through its endpoint.
type localRouteConn interface {
	sendConn
	localRoute()
}

func (e *packetEmission) register(context ackhandler.PacketContext, now monotime.Time, pn, largestAcked protocol.PacketNumber, streams []ackhandler.StreamFrame, frames []ackhandler.Frame, level protocol.EncryptionLevel, ecn protocol.ECN, size protocol.ByteCount, mtu, probe bool) error {
	if e.local == nil {
		(*e.recovery).SentPacketWithContext(context, now, pn, largestAcked, streams, frames, level, ecn, size, mtu, probe)
		return nil
	}
	recovery := (*e.recovery).(ackhandler.LocalSentPacketHandler)
	if probe {
		return recovery.RegisterLocalPathProbe(context, now, pn, frames, size)
	}
	var token ackhandler.LocalCompletionToken
	var err error
	if mtu {
		token, err = recovery.RegisterLocalMTUProbe(context, now, pn, frames, ecn, size)
	} else {
		token, err = recovery.RegisterLocalPacket(context, now, pn, largestAcked, streams, frames, level, ecn, size)
	}
	if err != nil {
		return err
	}
	e.local.register(token, carriesDatagram(frames))
	return nil
}

func (e *packetEmission) localWakeup() <-chan struct{} {
	if e.local != nil {
		return e.local.completed()
	}
	return nil
}

func (e *packetEmission) consumeLocal() error {
	if e.local != nil {
		return e.local.consume()
	}
	return nil
}

func (e *packetEmission) localFault() <-chan struct{} {
	if e.local != nil {
		return e.local.faulted()
	}
	return nil
}

func (e *packetEmission) resetLocal() {
	if e.local != nil {
		e.local.reset()
	}
}
