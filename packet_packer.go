package quic

import (
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"

	"github.com/quic-go/quic-go/internal/ackhandler"
	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
)

var errNothingToPack = errors.New("nothing to pack")

type sealer interface {
	EncryptHeader(sample []byte, firstByte *byte, pnBytes []byte)
	Overhead() int
}

type payload struct {
	streamFrames []ackhandler.StreamFrame
	frames       []ackhandler.Frame
	ack          *wire.AckFrame
	length       protocol.ByteCount
}

type longHeaderPacket struct {
	context      ackhandler.PacketContext
	header       *wire.ExtendedHeader
	ack          *wire.AckFrame
	frames       []ackhandler.Frame
	streamFrames []ackhandler.StreamFrame // only used for 0-RTT packets

	length protocol.ByteCount
}

type shortHeaderPacket struct {
	Context              ackhandler.PacketContext
	PacketNumber         protocol.PacketNumber
	Frames               []ackhandler.Frame
	StreamFrames         []ackhandler.StreamFrame
	Ack                  *wire.AckFrame
	Length               protocol.ByteCount
	IsPathMTUProbePacket bool
	IsPathProbePacket    bool

	// used for logging
	DestConnID      protocol.ConnectionID
	PacketNumberLen protocol.PacketNumberLen
	KeyPhase        protocol.KeyPhaseBit
}

// shortHeaderSeal contains serialization state without a second packet value.
type shortHeaderSeal struct {
	bufferStart   int
	payloadOffset protocol.ByteCount
	length        protocol.ByteCount
	sealer        sealer
}

// Length includes the authentication tag; buffer storage remains plaintext until
// commitment. Abandoning this value consumes no packet number or AEAD nonce.
type preparedShortHeaderPacket struct {
	shortHeaderPacket
	bufferStart   int
	payloadOffset protocol.ByteCount
	sealer        sealer
}

func (p *shortHeaderPacket) IsAckEliciting() bool { return ackhandler.HasAckElicitingFrames(p.Frames) }

type coalescedPacket struct {
	buffer         *packetBuffer
	longHdrPackets []*longHeaderPacket
	shortHdrPacket *shortHeaderPacket
}

// IsOnlyShortHeaderPacket says if this packet only contains a short header packet (and no long header packets).
func (p *coalescedPacket) IsOnlyShortHeaderPacket() bool {
	return len(p.longHdrPackets) == 0 && p.shortHdrPacket != nil
}

func (p *longHeaderPacket) EncryptionLevel() protocol.EncryptionLevel {
	//nolint:exhaustive // Will never be called for Retry packets (and they don't have encrypted data).
	switch p.header.Type {
	case protocol.PacketTypeInitial:
		return protocol.EncryptionInitial
	case protocol.PacketTypeHandshake:
		return protocol.EncryptionHandshake
	case protocol.PacketType0RTT:
		return protocol.Encryption0RTT
	default:
		panic("can't determine encryption level")
	}
}

func (p *longHeaderPacket) IsAckEliciting() bool { return ackhandler.HasAckElicitingFrames(p.frames) }

type packetNumberManager interface {
	PeekPacketNumber(protocol.EncryptionLevel) (protocol.PacketNumber, protocol.PacketNumberLen)
	PopPacketNumber(protocol.EncryptionLevel) protocol.PacketNumber
}

type sealingManager interface {
	GetInitialSealer() (handshake.LongHeaderSealer, error)
	GetHandshakeSealer() (handshake.LongHeaderSealer, error)
	Get0RTTSealer() (handshake.LongHeaderSealer, error)
	Get1RTTSealer() (handshake.ShortHeaderSealer, error)
}

type frameSource interface {
	HasData() bool
	Append([]ackhandler.Frame, []ackhandler.StreamFrame, protocol.ByteCount, monotime.Time, protocol.Version) ([]ackhandler.Frame, []ackhandler.StreamFrame, protocol.ByteCount)
}

type ackFrameSource interface {
	GetAckFrame(_ protocol.EncryptionLevel, now monotime.Time, onlyIfQueued bool) *wire.AckFrame
}

type packetPacker struct {
	srcConnID          protocol.ConnectionID
	getDestConnID      func() protocol.ConnectionID
	pathZeroGeneration *uint64

	perspective protocol.Perspective
	cryptoSetup sealingManager

	initialStream   *initialCryptoStream
	handshakeStream *cryptoStream

	token []byte

	pnManager     packetNumberManager
	framer        frameSource
	acks          ackFrameSource
	datagramQueue *datagramQueue
	// holdDatagrams keeps a frame that doesn't fit queued, since another path or a
	// fresh packet may carry it. Emission alone drops one once no path can. It is
	// set only with path admission; ordinary connections discard as before.
	holdDatagrams       bool
	retransmissionQueue *retransmissionQueue
	rand                rand.Rand

	// numNonAckElicitingAcks counts path zero's consecutive non-ack-eliciting
	// packets; each nonzero route keeps its own count (see pingCount).
	numNonAckElicitingAcks int
	// multipath is set with path admission. Every carrier there counts its periodic
	// PING by one rule, under which any ack-eliciting packet resets the count.
	multipath bool
}

// pingCount is the periodic-PING count of the packet's carrier. Path zero, the
// nil route, keeps the packer's field, so an ordinary connection's only carrier
// counts as before. Another carrier's packets never touch it.
func (p *packetPacker) pingCount(route *pathRoute) *int {
	if route != nil {
		return &route.numNonAckElicitingAcks
	}
	return &p.numNonAckElicitingAcks
}

// carrierOf is the path a packet on route leaves on.
func carrierOf(route *pathRoute) protocol.PathID {
	if route != nil {
		return route.context.PathID
	}
	return 0
}

func newPacketPacker(
	srcConnID protocol.ConnectionID,
	getDestConnID func() protocol.ConnectionID,
	initialStream *initialCryptoStream,
	handshakeStream *cryptoStream,
	packetNumberManager packetNumberManager,
	retransmissionQueue *retransmissionQueue,
	cryptoSetup sealingManager,
	framer frameSource,
	acks ackFrameSource,
	datagramQueue *datagramQueue,
	perspective protocol.Perspective,
) *packetPacker {
	var b [16]byte
	_, _ = crand.Read(b[:])

	return &packetPacker{
		cryptoSetup:         cryptoSetup,
		getDestConnID:       getDestConnID,
		srcConnID:           srcConnID,
		initialStream:       initialStream,
		handshakeStream:     handshakeStream,
		retransmissionQueue: retransmissionQueue,
		datagramQueue:       datagramQueue,
		perspective:         perspective,
		framer:              framer,
		acks:                acks,
		rand:                *rand.New(rand.NewPCG(binary.BigEndian.Uint64(b[:8]), binary.BigEndian.Uint64(b[8:]))),
		pnManager:           packetNumberManager,
	}
}

// pathZeroContext is the single ordinary-path adapter. The connection owns
// tuple changes; constructed packets retain a value snapshot through registration.
func (p *packetPacker) pathZeroContext() ackhandler.PacketContext {
	var generation uint64
	if p.pathZeroGeneration != nil {
		generation = *p.pathZeroGeneration
	}
	return ackhandler.PacketContext{TupleGeneration: generation}
}

// PackConnectionClose packs a packet that closes the connection with a transport error.
func (p *packetPacker) PackConnectionClose(e *qerr.TransportError, maxPacketSize protocol.ByteCount, v protocol.Version) (*coalescedPacket, error) {
	var reason string
	// don't send details of crypto errors
	if !e.ErrorCode.IsCryptoError() {
		reason = e.ErrorMessage
	}
	return p.packConnectionClose(false, uint64(e.ErrorCode), e.FrameType, reason, maxPacketSize, v)
}

// PackApplicationClose packs a packet that closes the connection with an application error.
func (p *packetPacker) PackApplicationClose(e *qerr.ApplicationError, maxPacketSize protocol.ByteCount, v protocol.Version) (*coalescedPacket, error) {
	return p.packConnectionClose(true, uint64(e.ErrorCode), 0, e.ErrorMessage, maxPacketSize, v)
}

// PackPathClose packs a close under a selected path's identity. It is a short header
// packet, because a nonzero path exists only after the handshake.
func (p *packetPacker) PackPathClose(route *pathRoute, frame *wire.ConnectionCloseFrame, maxPacketSize protocol.ByteCount, v protocol.Version) (shortHeaderPacket, *packetBuffer, error) {
	sealer, err := p.cryptoSetup.Get1RTTSealer()
	if err != nil {
		return shortHeaderPacket{}, nil, err
	}
	context, numbers, connID := p.shortHeaderIdentity(route)
	pn, pnLen := numbers.PeekPacketNumber(protocol.Encryption1RTT)
	pl := payload{frames: []ackhandler.Frame{{Frame: frame}}, length: frame.Length(v)}
	if size := p.shortHeaderPacketLength(connID, pnLen, pl) + protocol.ByteCount(sealer.Overhead()); size > maxPacketSize {
		return shortHeaderPacket{}, nil, fmt.Errorf("packet too large (%d bytes, allowed %d bytes)", size, maxPacketSize)
	}
	buffer := getPacketBufferForCeiling(packetCeiling(maxPacketSize))
	packet, err := p.appendShortHeaderPacket(context, buffer, connID, pn, pnLen, sealer.KeyPhase(), pl, 0, maxPacketSize, sealer, false, v)
	if err != nil {
		buffer.Release()
		return shortHeaderPacket{}, nil, err
	}
	return packet, buffer, nil
}

func (p *packetPacker) packConnectionClose(
	isApplicationError bool,
	errorCode uint64,
	frameType uint64,
	reason string,
	maxPacketSize protocol.ByteCount,
	v protocol.Version,
) (*coalescedPacket, error) {
	context := p.pathZeroContext()
	var sealers [4]sealer
	var hdrs [3]*wire.ExtendedHeader
	var payloads [4]payload
	var size protocol.ByteCount
	var connID protocol.ConnectionID
	var oneRTTPacketNumber protocol.PacketNumber
	var oneRTTPacketNumberLen protocol.PacketNumberLen
	var keyPhase protocol.KeyPhaseBit // only set for 1-RTT
	var numLongHdrPackets uint8
	encLevels := [4]protocol.EncryptionLevel{protocol.EncryptionInitial, protocol.EncryptionHandshake, protocol.Encryption0RTT, protocol.Encryption1RTT}
	for i, encLevel := range encLevels {
		if p.perspective == protocol.PerspectiveServer && encLevel == protocol.Encryption0RTT {
			continue
		}
		ccf := &wire.ConnectionCloseFrame{
			IsApplicationError: isApplicationError,
			ErrorCode:          errorCode,
			FrameType:          frameType,
			ReasonPhrase:       reason,
		}
		// don't send application errors in Initial or Handshake packets
		if isApplicationError && (encLevel == protocol.EncryptionInitial || encLevel == protocol.EncryptionHandshake) {
			ccf.IsApplicationError = false
			ccf.ErrorCode = uint64(qerr.ApplicationErrorErrorCode)
			ccf.ReasonPhrase = ""
		}
		pl := payload{
			frames: []ackhandler.Frame{{Frame: ccf}},
			length: ccf.Length(v),
		}

		var sealer sealer
		var err error
		switch encLevel {
		case protocol.EncryptionInitial:
			sealer, err = p.cryptoSetup.GetInitialSealer()
		case protocol.EncryptionHandshake:
			sealer, err = p.cryptoSetup.GetHandshakeSealer()
		case protocol.Encryption0RTT:
			sealer, err = p.cryptoSetup.Get0RTTSealer()
		case protocol.Encryption1RTT:
			var s handshake.ShortHeaderSealer
			s, err = p.cryptoSetup.Get1RTTSealer()
			if err == nil {
				keyPhase = s.KeyPhase()
			}
			sealer = s
		}
		if err == handshake.ErrKeysNotYetAvailable || err == handshake.ErrKeysDropped {
			continue
		}
		if err != nil {
			return nil, err
		}
		sealers[i] = sealer
		var hdr *wire.ExtendedHeader
		if encLevel == protocol.Encryption1RTT {
			connID = p.getDestConnID()
			oneRTTPacketNumber, oneRTTPacketNumberLen = p.pnManager.PeekPacketNumber(protocol.Encryption1RTT)
			size += p.shortHeaderPacketLength(connID, oneRTTPacketNumberLen, pl) + protocol.ByteCount(sealer.Overhead())
		} else {
			hdr = p.getLongHeader(encLevel, v)
			hdrs[i] = hdr
			size += p.longHeaderPacketLength(hdr, pl, v) + protocol.ByteCount(sealer.Overhead())
			numLongHdrPackets++
		}
		payloads[i] = pl
	}
	if size > maxPacketSize {
		return nil, fmt.Errorf("packet too large (%d bytes, allowed %d bytes)", size, maxPacketSize)
	}
	buffer := getPacketBufferForCeiling(packetCeiling(maxPacketSize))
	packet := &coalescedPacket{
		buffer:         buffer,
		longHdrPackets: make([]*longHeaderPacket, 0, numLongHdrPackets),
	}
	for i, encLevel := range encLevels {
		if sealers[i] == nil {
			continue
		}
		if encLevel == protocol.Encryption1RTT {
			shp, err := p.appendShortHeaderPacket(context, buffer, connID, oneRTTPacketNumber, oneRTTPacketNumberLen, keyPhase, payloads[i], 0, maxPacketSize, sealers[i], false, v)
			if err != nil {
				buffer.Release()
				return nil, err
			}
			packet.shortHdrPacket = &shp
		} else {
			var paddingLen protocol.ByteCount
			if encLevel == protocol.EncryptionInitial {
				paddingLen = p.initialPaddingLen(payloads[i].frames, size, maxPacketSize)
			}
			longContext := ackhandler.PacketContext{}
			if encLevel == protocol.Encryption0RTT {
				longContext = context
			}
			longHdrPacket, err := p.appendLongHeaderPacket(longContext, buffer, hdrs[i], payloads[i], paddingLen, encLevel, sealers[i], v)
			if err != nil {
				buffer.Release()
				return nil, err
			}
			packet.longHdrPackets = append(packet.longHdrPackets, longHdrPacket)
		}
	}
	return packet, nil
}

// longHeaderPacketLength calculates the length of a serialized long header packet.
// It takes into account that packets that have a tiny payload need to be padded,
// such that len(payload) + packet number len >= 4 + AEAD overhead
func (p *packetPacker) longHeaderPacketLength(hdr *wire.ExtendedHeader, pl payload, v protocol.Version) protocol.ByteCount {
	var paddingLen protocol.ByteCount
	pnLen := protocol.ByteCount(hdr.PacketNumberLen)
	if pl.length < 4-pnLen {
		paddingLen = 4 - pnLen - pl.length
	}
	return hdr.GetLength(v) + pl.length + paddingLen
}

// shortHeaderPaddingLen reserves enough bytes before the protection tag for
// the header protection sample, including when the packet payload is tiny.
func shortHeaderPaddingLen(pnLen protocol.PacketNumberLen, payloadLen protocol.ByteCount) protocol.ByteCount {
	return max(0, 4-protocol.ByteCount(pnLen)-payloadLen)
}

// shortHeaderPacketLength includes header protection padding and excludes the AEAD tag.
func (p *packetPacker) shortHeaderPacketLength(connID protocol.ConnectionID, pnLen protocol.PacketNumberLen, pl payload) protocol.ByteCount {
	return wire.ShortHeaderLen(connID, pnLen) + pl.length + shortHeaderPaddingLen(pnLen, pl.length)
}

// size is the expected size of the packet, if no padding was applied.
func (p *packetPacker) initialPaddingLen(frames []ackhandler.Frame, currentSize, maxPacketSize protocol.ByteCount) protocol.ByteCount {
	// For the server, only ack-eliciting Initial packets need to be padded.
	if p.perspective == protocol.PerspectiveServer && !ackhandler.HasAckElicitingFrames(frames) {
		return 0
	}
	if currentSize >= maxPacketSize {
		return 0
	}
	return maxPacketSize - currentSize
}

// PackCoalescedPacket packs a new packet.
// It packs an Initial / Handshake if there is data to send in these packet number spaces.
// It should only be called before the handshake is confirmed.
func (p *packetPacker) PackCoalescedPacket(onlyAck bool, maxSize protocol.ByteCount, now monotime.Time, v protocol.Version) (*coalescedPacket, error) {
	context := p.pathZeroContext()
	var (
		initialHdr, handshakeHdr, zeroRTTHdr                            *wire.ExtendedHeader
		initialPayload, handshakePayload, zeroRTTPayload, oneRTTPayload payload
		oneRTTPacketNumber                                              protocol.PacketNumber
		oneRTTPacketNumberLen                                           protocol.PacketNumberLen
	)
	// Try packing an Initial packet.
	initialSealer, err := p.cryptoSetup.GetInitialSealer()
	if err != nil && err != handshake.ErrKeysDropped {
		return nil, err
	}
	var size protocol.ByteCount
	if initialSealer != nil {
		initialHdr, initialPayload = p.maybeGetCryptoPacket(
			maxSize-protocol.ByteCount(initialSealer.Overhead()),
			protocol.EncryptionInitial,
			now,
			false,
			onlyAck,
			v,
		)
		if initialPayload.length > 0 {
			size += p.longHeaderPacketLength(initialHdr, initialPayload, v) + protocol.ByteCount(initialSealer.Overhead())
		}
	}

	// Add a Handshake packet.
	var handshakeSealer sealer
	if (onlyAck && size == 0) || (!onlyAck && size < maxSize-protocol.MinCoalescedPacketSize) {
		var err error
		handshakeSealer, err = p.cryptoSetup.GetHandshakeSealer()
		if err != nil && err != handshake.ErrKeysDropped && err != handshake.ErrKeysNotYetAvailable {
			return nil, err
		}
		if handshakeSealer != nil {
			handshakeHdr, handshakePayload = p.maybeGetCryptoPacket(
				maxSize-size-protocol.ByteCount(handshakeSealer.Overhead()),
				protocol.EncryptionHandshake,
				now,
				false,
				onlyAck,
				v,
			)
			if handshakePayload.length > 0 {
				s := p.longHeaderPacketLength(handshakeHdr, handshakePayload, v) + protocol.ByteCount(handshakeSealer.Overhead())
				size += s
			}
		}
	}

	// Add a 0-RTT / 1-RTT packet.
	var zeroRTTSealer sealer
	var oneRTTSealer handshake.ShortHeaderSealer
	var connID protocol.ConnectionID
	var kp protocol.KeyPhaseBit
	if (onlyAck && size == 0) || (!onlyAck && size < maxSize-protocol.MinCoalescedPacketSize) {
		var err error
		oneRTTSealer, err = p.cryptoSetup.Get1RTTSealer()
		if err != nil && err != handshake.ErrKeysDropped && err != handshake.ErrKeysNotYetAvailable {
			return nil, err
		}
		if err == nil { // 1-RTT
			kp = oneRTTSealer.KeyPhase()
			connID = p.getDestConnID()
			oneRTTPacketNumber, oneRTTPacketNumberLen = p.pnManager.PeekPacketNumber(protocol.Encryption1RTT)
			hdrLen := wire.ShortHeaderLen(connID, oneRTTPacketNumberLen)
			oneRTTPayload = p.maybeGetShortHeaderPacket(nil, ackhandler.AckCarriage{}, oneRTTSealer, hdrLen, maxSize-size, onlyAck, now, v, nil)
			if oneRTTPayload.length > 0 {
				size += p.shortHeaderPacketLength(connID, oneRTTPacketNumberLen, oneRTTPayload) + protocol.ByteCount(oneRTTSealer.Overhead())
			}
		} else if p.perspective == protocol.PerspectiveClient && !onlyAck { // 0-RTT packets can't contain ACK frames
			var err error
			zeroRTTSealer, err = p.cryptoSetup.Get0RTTSealer()
			if err != nil && err != handshake.ErrKeysDropped && err != handshake.ErrKeysNotYetAvailable {
				return nil, err
			}
			if zeroRTTSealer != nil {
				zeroRTTHdr, zeroRTTPayload = p.maybeGetAppDataPacketFor0RTT(zeroRTTSealer, maxSize-size, now, v)
				if zeroRTTPayload.length > 0 {
					size += p.longHeaderPacketLength(zeroRTTHdr, zeroRTTPayload, v) + protocol.ByteCount(zeroRTTSealer.Overhead())
				}
			}
		}
	}

	if initialPayload.length == 0 && handshakePayload.length == 0 && zeroRTTPayload.length == 0 && oneRTTPayload.length == 0 {
		return nil, nil
	}

	if size > maxSize {
		return nil, fmt.Errorf("packet too large (%d bytes, allowed %d bytes)", size, maxSize)
	}
	buffer := getPacketBufferForCeiling(packetCeiling(maxSize))
	packet := &coalescedPacket{
		buffer:         buffer,
		longHdrPackets: make([]*longHeaderPacket, 0, 3),
	}
	if initialPayload.length > 0 {
		padding := p.initialPaddingLen(initialPayload.frames, size, maxSize)
		cont, err := p.appendLongHeaderPacket(ackhandler.PacketContext{}, buffer, initialHdr, initialPayload, padding, protocol.EncryptionInitial, initialSealer, v)
		if err != nil {
			buffer.Release()
			return nil, err
		}
		packet.longHdrPackets = append(packet.longHdrPackets, cont)
	}
	if handshakePayload.length > 0 {
		cont, err := p.appendLongHeaderPacket(ackhandler.PacketContext{}, buffer, handshakeHdr, handshakePayload, 0, protocol.EncryptionHandshake, handshakeSealer, v)
		if err != nil {
			buffer.Release()
			return nil, err
		}
		packet.longHdrPackets = append(packet.longHdrPackets, cont)
	}
	if zeroRTTPayload.length > 0 {
		longHdrPacket, err := p.appendLongHeaderPacket(context, buffer, zeroRTTHdr, zeroRTTPayload, 0, protocol.Encryption0RTT, zeroRTTSealer, v)
		if err != nil {
			buffer.Release()
			return nil, err
		}
		packet.longHdrPackets = append(packet.longHdrPackets, longHdrPacket)
	} else if oneRTTPayload.length > 0 {
		shp, err := p.appendShortHeaderPacket(context, buffer, connID, oneRTTPacketNumber, oneRTTPacketNumberLen, kp, oneRTTPayload, 0, maxSize, oneRTTSealer, false, v)
		if err != nil {
			buffer.Release()
			return nil, err
		}
		packet.shortHdrPacket = &shp
	}
	return packet, nil
}

// PackAckOnlyPacket packs a packet containing only an ACK in the application data packet number space.
// It should be called after the handshake is confirmed.
func (p *packetPacker) PackAckOnlyPacket(maxSize protocol.ByteCount, now monotime.Time, v protocol.Version) (shortHeaderPacket, *packetBuffer, error) {
	return p.packAckOnlyPacket(nil, ackhandler.AckCarriage{}, maxSize, now, v)
}

func (p *packetPacker) packAckOnlyPacket(route *pathRoute, acks ackhandler.AckCarriage, maxSize protocol.ByteCount, now monotime.Time, v protocol.Version) (shortHeaderPacket, *packetBuffer, error) {
	buf := getPacketBufferForCeiling(packetCeiling(maxSize))
	packet, err := p.appendPacket(route, acks, buf, true, maxSize, now, v)
	if err != nil {
		buf.Release()
		return shortHeaderPacket{}, nil, err
	}
	return packet, buf, nil
}

// AppendPacket packs a packet in the application data packet number space.
// It should be called after the handshake is confirmed.
func (p *packetPacker) AppendPacket(buf *packetBuffer, maxSize protocol.ByteCount, now monotime.Time, v protocol.Version) (shortHeaderPacket, error) {
	return p.appendPacket(nil, ackhandler.AckCarriage{}, buf, false, maxSize, now, v)
}

// AppendDatagramPacket packs a packet carrying only the next queued DATAGRAM
// frame, with maxSize as its limit. Unlike AppendPacket, it never discards a
// frame that doesn't fit: it returns errNothingToPack and leaves the frame
// queued, since a later packet with a larger limit can carry it.
// It should be called after the handshake is confirmed.
func (p *packetPacker) AppendDatagramPacket(route *pathRoute, buf *packetBuffer, maxSize protocol.ByteCount, v protocol.Version) (shortHeaderPacket, error) {
	if p.datagramQueue == nil {
		return shortHeaderPacket{}, errNothingToPack
	}
	sealer, err := p.cryptoSetup.Get1RTTSealer()
	if err != nil {
		return shortHeaderPacket{}, err
	}
	f := p.datagramQueue.Peek()
	if f == nil {
		return shortHeaderPacket{}, errNothingToPack
	}
	context, numbers, connID := p.shortHeaderIdentity(route)
	pn, pnLen := numbers.PeekPacketNumber(protocol.Encryption1RTT)
	pl, err := p.datagramPayload(route, f, maxSize, connID, pnLen, sealer, v)
	if err != nil {
		return shortHeaderPacket{}, err
	}

	return p.appendShortHeaderPacket(context, buf, connID, pn, pnLen, sealer.KeyPhase(), pl, 0, maxSize, sealer, false, v)
}

// datagramPayload is shared by native preparation and value construction. It
// leaves a non-fitting DATAGRAM queued and commits extraction only on success.
func (p *packetPacker) datagramPayload(route *pathRoute, f *wire.DatagramFrame, maxSize protocol.ByteCount, connID protocol.ConnectionID, pnLen protocol.PacketNumberLen, sealer sealer, v protocol.Version) (payload, error) {
	size := f.Length(v)
	wireSize := p.shortHeaderPacketLength(connID, pnLen, payload{length: size}) + protocol.ByteCount(sealer.Overhead())
	if wireSize > maxSize {
		return payload{}, errNothingToPack
	}
	p.datagramQueue.Pop()
	*p.pingCount(route) = 0
	return payload{frames: []ackhandler.Frame{{Frame: f}}, length: size}, nil
}

// datagramRoom is the frame length a fresh packet on route can carry at maxSize. It
// reports false before 1-RTT keys exist. It reads the same inputs as packing.
func (p *packetPacker) datagramRoom(route *pathRoute, maxSize protocol.ByteCount) (protocol.ByteCount, bool) {
	sealer, err := p.cryptoSetup.Get1RTTSealer()
	if err != nil {
		return 0, false
	}
	_, numbers, connID := p.shortHeaderIdentity(route)
	_, pnLen := numbers.PeekPacketNumber(protocol.Encryption1RTT)
	return maxSize - wire.ShortHeaderLen(connID, pnLen) - protocol.ByteCount(sealer.Overhead()), true
}

// shortHeaderIdentity is the one seam between a selected route and packing: context,
// packet numbers and destination CID. The sealer follows the context.
func (p *packetPacker) shortHeaderIdentity(route *pathRoute) (ackhandler.PacketContext, packetNumberManager, protocol.ConnectionID) {
	if route != nil {
		return route.context, route.recovery, route.connID
	}
	return p.pathZeroContext(), p.pnManager, p.getDestConnID()
}

func (p *packetPacker) appendPacket(
	route *pathRoute,
	acks ackhandler.AckCarriage,
	buf *packetBuffer,
	onlyAck bool,
	maxPacketSize protocol.ByteCount,
	now monotime.Time,
	v protocol.Version,
) (shortHeaderPacket, error) {
	sealer, err := p.cryptoSetup.Get1RTTSealer()
	if err != nil {
		return shortHeaderPacket{}, err
	}
	context, numbers, connID := p.shortHeaderIdentity(route)
	pn, pnLen := numbers.PeekPacketNumber(protocol.Encryption1RTT)
	hdrLen := wire.ShortHeaderLen(connID, pnLen)
	pl := p.maybeGetShortHeaderPacket(route, acks, sealer, hdrLen, maxPacketSize, onlyAck, now, v, nil)
	if pl.length == 0 {
		return shortHeaderPacket{}, errNothingToPack
	}
	kp := sealer.KeyPhase()

	return p.appendShortHeaderPacket(context, buf, connID, pn, pnLen, kp, pl, 0, maxPacketSize, sealer, false, v)
}

// appendPreparedPacket writes ordinary native facts into the sole preparation.
// Header facts remain stack-owned by emission and expire before registration.
func (p *packetPacker) appendPreparedPacket(route *pathRoute, acks ackhandler.AckCarriage, buf *packetBuffer, maxPacketSize protocol.ByteCount, datagramOnly bool, now monotime.Time, v protocol.Version, prepared *ackhandler.PacketPreparation, header *nativeHeaderFacts) error {
	return p.appendPreparedPacketProtected(route, acks, buf, maxPacketSize, datagramOnly, now, v, prepared, header, nil)
}

func (p *packetPacker) appendPreparedPacketProtected(route *pathRoute, acks ackhandler.AckCarriage, buf *packetBuffer, maxPacketSize protocol.ByteCount, datagramOnly bool, now monotime.Time, v protocol.Version, prepared *ackhandler.PacketPreparation, header *nativeHeaderFacts, protection *nativeProtectionConstruction) error {
	if datagramOnly && p.datagramQueue == nil {
		return errNothingToPack
	}
	sealer, err := p.cryptoSetup.Get1RTTSealer()
	if err != nil {
		return err
	}
	protection.observeSealer(sealer)
	var datagram *wire.DatagramFrame
	if datagramOnly {
		datagram = p.datagramQueue.Peek()
		if datagram == nil {
			return errNothingToPack
		}
	}
	context, numbers, connID := p.shortHeaderIdentity(route)
	pn, pnLen := numbers.PeekPacketNumber(protocol.Encryption1RTT)
	hdrLen := wire.ShortHeaderLen(connID, pnLen)
	var pl payload
	if datagramOnly {
		pl, err = p.datagramPayload(route, datagram, maxPacketSize, connID, pnLen, sealer, v)
		if err != nil {
			return err
		}
	} else {
		pl = p.maybeGetShortHeaderPacket(route, acks, sealer, hdrLen, maxPacketSize, false, now, v, prepared.StreamStorage())
		if pl.length == 0 {
			return errNothingToPack
		}
	}
	kp := sealer.KeyPhase()
	seal, err := p.prepareShortHeaderPayload(context, buf, connID, pn, pnLen, kp, pl, 0, maxPacketSize, sealer, false, v)
	if err != nil {
		return err
	}
	prepared.SetContext(context)
	prepared.SetFrames(pl.frames, pl.streamFrames)
	prepared.SetLength(seal.length)
	header.ack, header.packetNumber = pl.ack, pn
	header.destConnID, header.packetNumberLen, header.keyPhase = connID, pnLen, kp
	// Preserve the existing irreversible seal / PN commitment sequence.
	if generation, ok := seal.sealer.(interface{ KeyGeneration() protocol.KeyPhase }); ok {
		prepared.SetKeyGeneration(generation.KeyGeneration())
	}
	deferred, err := protection.reserve(seal.sealer, pn, seal.bufferStart, int(seal.payloadOffset), pnLen, int(seal.length))
	if err != nil {
		return err
	}
	if !deferred {
		raw, err := p.encryptPacket(buf.Data[seal.bufferStart:], seal.sealer, pn, seal.payloadOffset, protocol.ByteCount(pnLen))
		if err != nil {
			return err
		}
		buf.Data = buf.Data[:seal.bufferStart+len(raw)]
	}

	numbers, err = p.packetNumbersOn(context.PathID)
	if err != nil {
		return err
	}
	if newPN := numbers.PopPacketNumber(protocol.Encryption1RTT); newPN != pn {
		return fmt.Errorf("packetPacker BUG: Peeked and Popped packet numbers do not match: expected %d, got %d", pn, newPN)
	}
	return nil
}

func (p *packetPacker) maybeGetCryptoPacket(
	maxPacketSize protocol.ByteCount,
	encLevel protocol.EncryptionLevel,
	now monotime.Time,
	addPingIfEmpty bool,
	onlyAck bool,
	v protocol.Version,
) (*wire.ExtendedHeader, payload) {
	if onlyAck {
		if ack := p.acks.GetAckFrame(encLevel, now, true); ack != nil {
			hdr := p.getLongHeader(encLevel, v)
			maxPacketSize -= hdr.GetLength(v)
			ack.Truncate(maxPacketSize, v)
			return hdr, payload{ack: ack, length: ack.Length(v)}
		}
		return nil, payload{length: 0}
	}

	var hasCryptoData func() bool
	var popCryptoFrame func(maxLen protocol.ByteCount) *wire.CryptoFrame
	//nolint:exhaustive // Initial and Handshake are the only two encryption levels here.
	switch encLevel {
	case protocol.EncryptionInitial:
		hasCryptoData = p.initialStream.HasData
		popCryptoFrame = p.initialStream.PopCryptoFrame
	case protocol.EncryptionHandshake:
		hasCryptoData = p.handshakeStream.HasData
		popCryptoFrame = p.handshakeStream.PopCryptoFrame
	}
	handler := p.retransmissionQueue.AckHandler(encLevel)
	hasRetransmission := p.retransmissionQueue.HasData(encLevel)

	ack := p.acks.GetAckFrame(encLevel, now, !hasRetransmission && !hasCryptoData())
	var pl payload
	if !hasCryptoData() && !hasRetransmission && ack == nil {
		if !addPingIfEmpty {
			// nothing to send
			return nil, payload{}
		}
		ping := &wire.PingFrame{}
		pl.frames = append(pl.frames, ackhandler.Frame{Frame: ping, Handler: emptyHandler{}})
		pl.length += ping.Length(v)
	}

	hdr := p.getLongHeader(encLevel, v)
	maxPacketSize -= hdr.GetLength(v)

	if ack != nil {
		ack.Truncate(maxPacketSize, v)
		pl.ack = ack
		pl.length = ack.Length(v)
		maxPacketSize -= pl.length
	}
	if hasRetransmission {
		for {
			frame := p.retransmissionQueue.GetFrame(encLevel, maxPacketSize, v)
			if frame == nil {
				break
			}
			pl.frames = append(pl.frames, ackhandler.Frame{
				Frame:   frame,
				Handler: p.retransmissionQueue.AckHandler(encLevel),
			})
			frameLen := frame.Length(v)
			pl.length += frameLen
			maxPacketSize -= frameLen
		}
		return hdr, pl
	} else {
		for hasCryptoData() {
			cf := popCryptoFrame(maxPacketSize)
			if cf == nil {
				break
			}
			pl.frames = append(pl.frames, ackhandler.Frame{Frame: cf, Handler: handler})
			pl.length += cf.Length(v)
			maxPacketSize -= cf.Length(v)
		}
	}
	return hdr, pl
}

func (p *packetPacker) maybeGetAppDataPacketFor0RTT(sealer sealer, maxSize protocol.ByteCount, now monotime.Time, v protocol.Version) (*wire.ExtendedHeader, payload) {
	if p.perspective != protocol.PerspectiveClient {
		return nil, payload{}
	}

	hdr := p.getLongHeader(protocol.Encryption0RTT, v)
	maxPayloadSize := maxSize - hdr.GetLength(v) - protocol.ByteCount(sealer.Overhead())
	return hdr, p.maybeGetAppDataPacket(nil, ackhandler.AckCarriage{}, maxPayloadSize, false, false, now, v, nil)
}

func (p *packetPacker) maybeGetShortHeaderPacket(
	route *pathRoute,
	acks ackhandler.AckCarriage,
	sealer handshake.ShortHeaderSealer,
	hdrLen, maxPacketSize protocol.ByteCount,
	onlyAck bool,
	now monotime.Time,
	v protocol.Version,
	streamStorage []ackhandler.StreamFrame,
) payload {
	maxPayloadSize := maxPacketSize - hdrLen - protocol.ByteCount(sealer.Overhead())
	return p.maybeGetAppDataPacket(route, acks, maxPayloadSize, onlyAck, true, now, v, streamStorage)
}

func (p *packetPacker) maybeGetAppDataPacket(
	route *pathRoute,
	acks ackhandler.AckCarriage,
	maxPayloadSize protocol.ByteCount,
	onlyAck, ackAllowed bool,
	now monotime.Time,
	v protocol.Version,
	streamStorage []ackhandler.StreamFrame,
) payload {
	pl := p.composeNextPacket(route, acks, maxPayloadSize, onlyAck, ackAllowed, now, v, streamStorage)

	// check if we have anything to send
	count := p.pingCount(route)
	if !ackhandler.HasAckElicitingFrames(pl.frames) && len(pl.streamFrames) == 0 {
		if pl.ack == nil && len(pl.frames) == 0 {
			return payload{}
		}
		// the packet only contains an ACK
		if *count >= protocol.MaxNonAckElicitingAcks {
			ping := &wire.PingFrame{}
			pl.frames = append(pl.frames, ackhandler.Frame{Frame: ping})
			pl.length += ping.Length(v)
			*count = 0
		} else {
			*count++
		}
	} else {
		*count = 0
	}
	return pl
}

func (p *packetPacker) composeNextPacket(
	route *pathRoute,
	acks ackhandler.AckCarriage,
	maxPayloadSize protocol.ByteCount,
	onlyAck, ackAllowed bool,
	now monotime.Time,
	v protocol.Version,
	streamStorage []ackhandler.StreamFrame,
) payload {
	// Only ACK selection reserves the periodic PING byte. Application data
	// is itself ACK-eliciting and can use the full payload capacity.
	maxAckSize := maxPayloadSize
	if ackAllowed && *p.pingCount(route) >= protocol.MaxNonAckElicitingAcks {
		maxAckSize--
	}
	carrier := carrierOf(route)
	if onlyAck {
		if ack := p.appDataAck(acks, carrier, now, true); ack != nil {
			ack.Truncate(maxAckSize, v)
			pl := payload{ack: ack, length: ack.Length(v)}
			p.appendPathAcks(&pl, acks, carrier, maxAckSize, now, true, v)
			return pl
		}
		var pl payload
		p.appendPathAcks(&pl, acks, carrier, maxAckSize, now, true, v)
		return pl
	}

	hasData := p.framer.HasData()
	hasRetransmission := p.retransmissionQueue.HasData(protocol.Encryption1RTT)

	pl := payload{streamFrames: streamStorage}
	if ackAllowed {
		if ack := p.appDataAck(acks, carrier, now, !hasRetransmission && !hasData); ack != nil {
			ack.Truncate(maxAckSize, v)
			pl.ack = ack
			pl.length += ack.Length(v)
		}
	}

	if ackAllowed {
		p.appendPathAcks(&pl, acks, carrier, maxAckSize, now, !hasRetransmission && !hasData, v)
	}
	if p.datagramQueue != nil {
		if f := p.datagramQueue.Peek(); f != nil {
			size := f.Length(v)
			if size <= maxPayloadSize-pl.length { // DATAGRAM frame fits
				pl.frames = append(pl.frames, ackhandler.Frame{Frame: f})
				pl.length += size
				p.datagramQueue.Pop()
			} else if !p.holdDatagrams && pl.ack == nil && len(pl.frames) == 0 {
				// The DATAGRAM frame doesn't fit, and the packet doesn't contain an ACK.
				// Discard this frame. There's no point in retrying this in the next packet,
				// as it's unlikely that the available packet size will increase.
				p.datagramQueue.Pop()
			}
			// If the DATAGRAM frame was too large and the packet contained an ACK, we'll try to send it out later.
		}
	}

	if (pl.ack != nil || len(pl.frames) > 0) && !hasData && !hasRetransmission {
		return pl
	}

	if hasRetransmission {
		for {
			remainingLen := maxPayloadSize - pl.length
			if remainingLen < protocol.MinStreamFrameSize {
				break
			}
			f := p.retransmissionQueue.GetFrame(protocol.Encryption1RTT, remainingLen, v)
			if f == nil {
				break
			}
			pl.frames = append(pl.frames, ackhandler.Frame{Frame: f, Handler: p.retransmissionQueue.AckHandler(protocol.Encryption1RTT)})
			pl.length += f.Length(v)
		}
	}

	if hasData {
		var lengthAdded protocol.ByteCount
		startLen := len(pl.frames)
		pl.frames, pl.streamFrames, lengthAdded = p.framer.Append(pl.frames, pl.streamFrames, maxPayloadSize-pl.length, now, v)
		pl.length += lengthAdded
		// add handlers for the control frames that were added
		for i := startLen; i < len(pl.frames); i++ {
			if pl.frames[i].Handler != nil {
				continue
			}
			switch pl.frames[i].Frame.(type) {
			case *wire.PathChallengeFrame, *wire.PathResponseFrame:
				// Path probing is currently not supported, therefore we don't need to set the OnAcked callback yet.
				// PATH_CHALLENGE and PATH_RESPONSE are never retransmitted.
			default:
				// we might be packing a 0-RTT packet, but we need to use the 1-RTT ack handler anyway
				pl.frames[i].Handler = p.retransmissionQueue.AckHandler(protocol.Encryption1RTT)
			}
		}
	}
	return pl
}

func (p *packetPacker) PackPTOProbePacket(
	encLevel protocol.EncryptionLevel,
	maxPacketSize protocol.ByteCount,
	addPingIfEmpty bool,
	now monotime.Time,
	v protocol.Version,
) (*coalescedPacket, error) {
	return p.packPTOProbePacket(nil, ackhandler.AckCarriage{}, encLevel, maxPacketSize, addPingIfEmpty, now, v)
}

func (p *packetPacker) packPTOProbePacket(
	route *pathRoute,
	acks ackhandler.AckCarriage,
	encLevel protocol.EncryptionLevel,
	maxPacketSize protocol.ByteCount,
	addPingIfEmpty bool,
	now monotime.Time,
	v protocol.Version,
) (*coalescedPacket, error) {
	if encLevel == protocol.Encryption1RTT {
		return p.packPTOProbePacket1RTT(route, acks, maxPacketSize, addPingIfEmpty, now, v)
	}

	var sealer handshake.LongHeaderSealer
	//nolint:exhaustive // Probe packets are never sent for 0-RTT.
	switch encLevel {
	case protocol.EncryptionInitial:
		var err error
		sealer, err = p.cryptoSetup.GetInitialSealer()
		if err != nil {
			return nil, err
		}
	case protocol.EncryptionHandshake:
		var err error
		sealer, err = p.cryptoSetup.GetHandshakeSealer()
		if err != nil {
			return nil, err
		}
	default:
		panic("unknown encryption level")
	}
	hdr, pl := p.maybeGetCryptoPacket(
		maxPacketSize-protocol.ByteCount(sealer.Overhead()),
		encLevel,
		now,
		addPingIfEmpty,
		false,
		v,
	)
	if pl.length == 0 {
		return nil, nil
	}
	size := p.longHeaderPacketLength(hdr, pl, v) + protocol.ByteCount(sealer.Overhead())
	if size > maxPacketSize {
		return nil, fmt.Errorf("packet too large (%d bytes, allowed %d bytes)", size, maxPacketSize)
	}
	buffer := getPacketBufferForCeiling(packetCeiling(maxPacketSize))
	packet := &coalescedPacket{buffer: buffer}
	var padding protocol.ByteCount
	if encLevel == protocol.EncryptionInitial {
		padding = p.initialPaddingLen(pl.frames, size, maxPacketSize)
	}

	longHdrPacket, err := p.appendLongHeaderPacket(ackhandler.PacketContext{}, buffer, hdr, pl, padding, encLevel, sealer, v)
	if err != nil {
		buffer.Release()
		return nil, err
	}
	packet.longHdrPackets = []*longHeaderPacket{longHdrPacket}
	return packet, nil
}

func (p *packetPacker) packPTOProbePacket1RTT(route *pathRoute, acks ackhandler.AckCarriage, maxPacketSize protocol.ByteCount, addPingIfEmpty bool, now monotime.Time, v protocol.Version) (*coalescedPacket, error) {
	s, err := p.cryptoSetup.Get1RTTSealer()
	if err != nil {
		return nil, err
	}
	kp := s.KeyPhase()
	context, numbers, connID := p.shortHeaderIdentity(route)
	pn, pnLen := numbers.PeekPacketNumber(protocol.Encryption1RTT)
	hdrLen := wire.ShortHeaderLen(connID, pnLen)
	pl := p.maybeGetAppDataPacket(route, acks, maxPacketSize-protocol.ByteCount(s.Overhead())-hdrLen, false, true, now, v, nil)
	if pl.length == 0 {
		if !addPingIfEmpty {
			return nil, nil
		}
		ping := &wire.PingFrame{}
		pl.frames = append(pl.frames, ackhandler.Frame{Frame: ping, Handler: emptyHandler{}})
		pl.length += ping.Length(v)
		// An ordinary connection keeps its inherited count here.
		if route != nil || p.multipath {
			*p.pingCount(route) = 0
		}
	}
	buffer := getPacketBufferForCeiling(packetCeiling(maxPacketSize))
	packet := &coalescedPacket{buffer: buffer}
	shp, err := p.appendShortHeaderPacket(context, buffer, connID, pn, pnLen, kp, pl, 0, maxPacketSize, s, false, v)
	if err != nil {
		buffer.Release()
		return nil, err
	}
	packet.shortHdrPacket = &shp
	return packet, nil
}

// packPingPacket1RTT packs an ack-eliciting PING on route with any queued ACKs and
// nothing else: the framer, retransmission and DATAGRAM queues keep their frames.
func (p *packetPacker) packPingPacket1RTT(route *pathRoute, acks ackhandler.AckCarriage, maxPacketSize protocol.ByteCount, now monotime.Time, v protocol.Version) (*coalescedPacket, error) {
	s, err := p.cryptoSetup.Get1RTTSealer()
	if err != nil {
		return nil, err
	}
	kp := s.KeyPhase()
	context, numbers, connID := p.shortHeaderIdentity(route)
	pn, pnLen := numbers.PeekPacketNumber(protocol.Encryption1RTT)
	hdrLen := wire.ShortHeaderLen(connID, pnLen)
	ping := &wire.PingFrame{}
	pl := p.composeNextPacket(route, acks, maxPacketSize-protocol.ByteCount(s.Overhead())-hdrLen-ping.Length(v), true, true, now, v, nil)
	pl.frames = append(pl.frames, ackhandler.Frame{Frame: ping, Handler: emptyHandler{}})
	pl.length += ping.Length(v)
	*p.pingCount(route) = 0
	buffer := getPacketBufferForCeiling(packetCeiling(maxPacketSize))
	packet := &coalescedPacket{buffer: buffer}
	shp, err := p.appendShortHeaderPacket(context, buffer, connID, pn, pnLen, kp, pl, 0, maxPacketSize, s, false, v)
	if err != nil {
		buffer.Release()
		return nil, err
	}
	packet.shortHdrPacket = &shp
	return packet, nil
}

func (p *packetPacker) PackMTUProbePacket(ping ackhandler.Frame, size protocol.ByteCount, v protocol.Version) (shortHeaderPacket, *packetBuffer, error) {
	return p.packMTUProbePacket(nil, ping, size, v)
}

func (p *packetPacker) packMTUProbePacket(route *pathRoute, ping ackhandler.Frame, size protocol.ByteCount, v protocol.Version) (shortHeaderPacket, *packetBuffer, error) {
	pl := payload{
		frames: []ackhandler.Frame{ping},
		length: ping.Frame.Length(v),
	}
	buffer := getPacketBufferForCeiling(packetCeiling(size))
	s, err := p.cryptoSetup.Get1RTTSealer()
	if err != nil {
		buffer.Release()
		return shortHeaderPacket{}, nil, err
	}
	context, numbers, connID := p.shortHeaderIdentity(route)
	pn, pnLen := numbers.PeekPacketNumber(protocol.Encryption1RTT)
	padding := size - p.shortHeaderPacketLength(connID, pnLen, pl) - protocol.ByteCount(s.Overhead())
	kp := s.KeyPhase()
	packet, err := p.appendShortHeaderPacket(context, buffer, connID, pn, pnLen, kp, pl, padding, size, s, true, v)
	if err != nil {
		buffer.Release()
		return shortHeaderPacket{}, nil, err
	}
	return packet, buffer, nil
}

func (p *packetPacker) PackPathProbePacket(connID protocol.ConnectionID, frames []ackhandler.Frame, v protocol.Version) (shortHeaderPacket, *packetBuffer, error) {
	prepared, buffer, err := p.preparePathProbePacket(connID, frames, protocol.MinInitialPacketSize, v)
	if err != nil || buffer == nil {
		return shortHeaderPacket{}, buffer, err
	}
	packet, err := p.commitShortHeaderPacket(buffer, prepared)
	if err != nil {
		buffer.Release()
		return shortHeaderPacket{}, nil, err
	}
	return packet, buffer, nil
}

// preparePathProbePacket leaves bytes unsealed. Emission commits encryption and
// the packet number after final policy checks and exact byte reservation.
func (p *packetPacker) preparePathProbePacket(connID protocol.ConnectionID, frames []ackhandler.Frame, maxPacketSize protocol.ByteCount, v protocol.Version) (preparedShortHeaderPacket, *packetBuffer, error) {
	return p.preparePathProbePacketOn(p.pathZeroContext(), connID, frames, maxPacketSize, v)
}

// packetNumbersOn resolves only admitted recovery. Path zero keeps the captured manager.
func (p *packetPacker) packetNumbersOn(id protocol.PathID) (packetNumberManager, error) {
	if id == 0 {
		return p.pnManager, nil
	}
	if h, ok := p.pnManager.(interface {
		RecoveryOnPath(protocol.PathID) ackhandler.SentPacketHandler
	}); ok {
		if path := h.RecoveryOnPath(id); path != nil {
			return path, nil
		}
	}
	return nil, fmt.Errorf("packetPacker BUG: no packet numbers for path %d", id)
}

func (p *packetPacker) preparePathProbePacketOn(context ackhandler.PacketContext, connID protocol.ConnectionID, frames []ackhandler.Frame, maxPacketSize protocol.ByteCount, v protocol.Version) (preparedShortHeaderPacket, *packetBuffer, error) {
	numbers, err := p.packetNumbersOn(context.PathID)
	if err != nil {
		return preparedShortHeaderPacket{}, nil, err
	}
	pn, pnLen := numbers.PeekPacketNumber(protocol.Encryption1RTT)
	// Path validation is padded only to the minimum, regardless of amplification allowance.
	buf := getPacketBuffer()
	s, err := p.cryptoSetup.Get1RTTSealer()
	if err != nil {
		buf.Release()
		return preparedShortHeaderPacket{}, nil, err
	}
	var l protocol.ByteCount
	for _, f := range frames {
		l += f.Frame.Length(v)
	}
	payload := payload{
		frames: frames,
		length: l,
	}
	size := min(protocol.MinInitialPacketSize, maxPacketSize)
	packetLen := p.shortHeaderPacketLength(connID, pnLen, payload) + protocol.ByteCount(s.Overhead())
	if packetLen > size {
		buf.Release()
		return preparedShortHeaderPacket{}, nil, nil
	}
	padding := size - packetLen
	packet, err := p.prepareShortHeaderPacket(context, buf, connID, pn, pnLen, s.KeyPhase(), payload, padding, size, s, false, v)
	if err != nil {
		buf.Release()
		return preparedShortHeaderPacket{}, nil, err
	}
	packet.IsPathProbePacket = true
	return packet, buf, err
}

func (p *packetPacker) getLongHeader(encLevel protocol.EncryptionLevel, v protocol.Version) *wire.ExtendedHeader {
	pn, pnLen := p.pnManager.PeekPacketNumber(encLevel)
	hdr := &wire.ExtendedHeader{
		PacketNumber:    pn,
		PacketNumberLen: pnLen,
	}
	hdr.Version = v
	hdr.SrcConnectionID = p.srcConnID
	hdr.DestConnectionID = p.getDestConnID()

	//nolint:exhaustive // 1-RTT packets are not long header packets.
	switch encLevel {
	case protocol.EncryptionInitial:
		hdr.Type = protocol.PacketTypeInitial
		hdr.Token = p.token
	case protocol.EncryptionHandshake:
		hdr.Type = protocol.PacketTypeHandshake
	case protocol.Encryption0RTT:
		hdr.Type = protocol.PacketType0RTT
	}
	return hdr
}

func (p *packetPacker) appendLongHeaderPacket(context ackhandler.PacketContext, buffer *packetBuffer, header *wire.ExtendedHeader, pl payload, padding protocol.ByteCount, encLevel protocol.EncryptionLevel, sealer sealer, v protocol.Version) (*longHeaderPacket, error) {
	var paddingLen protocol.ByteCount
	pnLen := protocol.ByteCount(header.PacketNumberLen)
	if pl.length < 4-pnLen {
		paddingLen = 4 - pnLen - pl.length
	}
	paddingLen += padding
	header.Length = pnLen + protocol.ByteCount(sealer.Overhead()) + pl.length + paddingLen

	startLen := len(buffer.Data)
	raw := buffer.Data[startLen:]
	raw, err := header.Append(raw, v)
	if err != nil {
		return nil, err
	}
	payloadOffset := protocol.ByteCount(len(raw))

	raw, err = p.appendPacketPayload(raw, pl, paddingLen, v)
	if err != nil {
		return nil, err
	}
	raw, err = p.encryptPacket(raw, sealer, header.PacketNumber, payloadOffset, pnLen)
	if err != nil {
		return nil, err
	}
	buffer.Data = buffer.Data[:len(buffer.Data)+len(raw)]

	if pn := p.pnManager.PopPacketNumber(encLevel); pn != header.PacketNumber {
		return nil, fmt.Errorf("packetPacker BUG: Peeked and Popped packet numbers do not match: expected %d, got %d", pn, header.PacketNumber)
	}
	return &longHeaderPacket{
		context:      context,
		header:       header,
		ack:          pl.ack,
		frames:       pl.frames,
		streamFrames: pl.streamFrames,
		length:       protocol.ByteCount(len(raw)),
	}, nil
}

func (p *packetPacker) appendShortHeaderPacket(
	context ackhandler.PacketContext,
	buffer *packetBuffer,
	connID protocol.ConnectionID,
	pn protocol.PacketNumber,
	pnLen protocol.PacketNumberLen,
	kp protocol.KeyPhaseBit,
	pl payload,
	padding, maxPacketSize protocol.ByteCount,
	sealer sealer,
	isMTUProbePacket bool,
	v protocol.Version,
) (shortHeaderPacket, error) {
	prepared, err := p.prepareShortHeaderPacket(context, buffer, connID, pn, pnLen, kp, pl, padding, maxPacketSize, sealer, isMTUProbePacket, v)
	if err != nil {
		return shortHeaderPacket{}, err
	}
	return p.commitShortHeaderPacket(buffer, prepared)
}

// Probe preparation can be abandoned before the emission commitment boundary.
// Other short-header construction retains its existing immediate commitment.
func (p *packetPacker) prepareShortHeaderPacket(
	context ackhandler.PacketContext,
	buffer *packetBuffer,
	connID protocol.ConnectionID,
	pn protocol.PacketNumber,
	pnLen protocol.PacketNumberLen,
	kp protocol.KeyPhaseBit,
	pl payload,
	padding, maxPacketSize protocol.ByteCount,
	sealer sealer,
	isMTUProbePacket bool,
	v protocol.Version,
) (preparedShortHeaderPacket, error) {
	seal, err := p.prepareShortHeaderPayload(context, buffer, connID, pn, pnLen, kp, pl, padding, maxPacketSize, sealer, isMTUProbePacket, v)
	if err != nil {
		return preparedShortHeaderPacket{}, err
	}
	return preparedShortHeaderPacket{
		shortHeaderPacket: shortHeaderPacket{
			Context: context, PacketNumber: pn, PacketNumberLen: pnLen,
			KeyPhase: kp, StreamFrames: pl.streamFrames, Frames: pl.frames,
			Ack: pl.ack, Length: seal.length, DestConnID: connID,
			IsPathMTUProbePacket: isMTUProbePacket,
		},
		bufferStart: seal.bufferStart, payloadOffset: seal.payloadOffset, sealer: seal.sealer,
	}, nil
}

// shortHeaderSeal contains only serialization state, not a second packet value.

func (p *packetPacker) prepareShortHeaderPayload(
	context ackhandler.PacketContext,
	buffer *packetBuffer,
	connID protocol.ConnectionID,
	pn protocol.PacketNumber,
	pnLen protocol.PacketNumberLen,
	kp protocol.KeyPhaseBit,
	pl payload,
	padding, maxPacketSize protocol.ByteCount,
	sealer sealer,
	isMTUProbePacket bool,
	v protocol.Version,
) (shortHeaderSeal, error) {
	{
		short, ok := sealer.(handshake.ShortHeaderSealer)
		if !ok {
			return shortHeaderSeal{}, handshake.ErrKeysDropped
		}
		selected, err := handshake.ShortHeaderSealerForPath(short, context.PathID)
		if err != nil {
			return shortHeaderSeal{}, err
		}
		sealer = selected
	}
	paddingLen := shortHeaderPaddingLen(pnLen, pl.length) + padding

	startLen := len(buffer.Data)
	raw := buffer.Data[startLen:]
	raw, err := wire.AppendShortHeader(raw, connID, pn, pnLen, kp)
	if err != nil {
		return shortHeaderSeal{}, err
	}
	payloadOffset := protocol.ByteCount(len(raw))

	raw, err = p.appendPacketPayload(raw, pl, paddingLen, v)
	if err != nil {
		return shortHeaderSeal{}, err
	}
	length := protocol.ByteCount(len(raw) + sealer.Overhead())
	if !isMTUProbePacket && length > maxPacketSize {
		return shortHeaderSeal{}, fmt.Errorf("PacketPacker BUG: packet too large (%d bytes, allowed %d bytes)", length, maxPacketSize)
	}
	buffer.Data = buffer.Data[:len(buffer.Data)+len(raw)]

	return shortHeaderSeal{bufferStart: startLen, payloadOffset: payloadOffset, length: length, sealer: sealer}, nil
}

// This is the irreversible commitment boundary. No caller may abandon the
// prepared datagram after sealing without consuming its number and registering it.
func (p *packetPacker) commitShortHeaderPacket(buffer *packetBuffer, prepared preparedShortHeaderPacket) (shortHeaderPacket, error) {
	packet := prepared.shortHeaderPacket
	if generation, ok := prepared.sealer.(interface{ KeyGeneration() protocol.KeyPhase }); ok {
		packet.Context.KeyGeneration = generation.KeyGeneration()
	}
	raw, err := p.encryptPacket(buffer.Data[prepared.bufferStart:], prepared.sealer, packet.PacketNumber, prepared.payloadOffset, protocol.ByteCount(packet.PacketNumberLen))
	if err != nil {
		return shortHeaderPacket{}, err
	}
	buffer.Data = buffer.Data[:prepared.bufferStart+len(raw)]
	numbers, err := p.packetNumbersOn(packet.Context.PathID)
	if err != nil {
		return shortHeaderPacket{}, err
	}
	if newPN := numbers.PopPacketNumber(protocol.Encryption1RTT); newPN != packet.PacketNumber {
		return shortHeaderPacket{}, fmt.Errorf("packetPacker BUG: Peeked and Popped packet numbers do not match: expected %d, got %d", packet.PacketNumber, newPN)
	}
	return packet, nil
}

// appendPacketPayload serializes the payload of a packet into the raw byte slice.
// It modifies the order of payload.frames.
func (p *packetPacker) appendPacketPayload(raw []byte, pl payload, paddingLen protocol.ByteCount, v protocol.Version) ([]byte, error) {
	payloadOffset := len(raw)
	if pl.ack != nil {
		var err error
		raw, err = pl.ack.Append(raw, v)
		if err != nil {
			return nil, err
		}
	}
	if paddingLen > 0 {
		raw = append(raw, make([]byte, paddingLen)...)
	}
	// Randomize the order of the control frames.
	// This makes sure that the receiver doesn't rely on the order in which frames are packed.
	if len(pl.frames) > 1 {
		p.rand.Shuffle(len(pl.frames), func(i, j int) { pl.frames[i], pl.frames[j] = pl.frames[j], pl.frames[i] })
	}
	for _, f := range pl.frames {
		var err error
		raw, err = f.Frame.Append(raw, v)
		if err != nil {
			return nil, err
		}
		if datagram, ok := f.Frame.(*wire.DatagramFrame); ok && p.datagramQueue != nil {
			p.datagramQueue.recycleSendPayload(datagram)
		}
	}
	for _, f := range pl.streamFrames {
		var err error
		raw, err = f.Frame.Append(raw, v)
		if err != nil {
			return nil, err
		}
	}

	if payloadSize := protocol.ByteCount(len(raw)-payloadOffset) - paddingLen; payloadSize != pl.length {
		return nil, fmt.Errorf("PacketPacker BUG: payload size inconsistent (expected %d, got %d bytes)", pl.length, payloadSize)
	}
	return raw, nil
}

func (p *packetPacker) encryptPacket(raw []byte, sealer sealer, pn protocol.PacketNumber, payloadOffset, pnLen protocol.ByteCount) ([]byte, error) {
	if short, ok := sealer.(handshake.ShortHeaderSealer); ok {
		if _, err := short.Seal(raw[payloadOffset:payloadOffset], raw[payloadOffset:], pn, raw[:payloadOffset]); err != nil {
			return nil, err
		}
	} else {
		_ = sealer.(handshake.LongHeaderSealer).Seal(raw[payloadOffset:payloadOffset], raw[payloadOffset:], pn, raw[:payloadOffset])
	}
	raw = raw[:len(raw)+sealer.Overhead()]
	// apply header protection
	pnOffset := payloadOffset - pnLen
	sealer.EncryptHeader(raw[pnOffset+4:pnOffset+4+16], &raw[0], raw[pnOffset:payloadOffset])
	return raw, nil
}

func (p *packetPacker) SetToken(token []byte) {
	p.token = token
}

type emptyHandler struct{}

var _ ackhandler.FrameHandler = emptyHandler{}

func (emptyHandler) OnAcked(wire.Frame) {}
func (emptyHandler) OnLost(wire.Frame)  {}

// appDataAck is path zero's 1-RTT ACK for a packet on carrier. The receive owner
// applies carriage; a source without carriage keeps ordinary selection.
func (p *packetPacker) appDataAck(acks ackhandler.AckCarriage, carrier protocol.PathID, now monotime.Time, onlyIfQueued bool) *wire.AckFrame {
	if source, ok := p.acks.(interface {
		GetAckFrameOn(ackhandler.AckCarriage, protocol.PathID, monotime.Time, bool) *wire.AckFrame
	}); ok {
		return source.GetAckFrameOn(acks, carrier, now, onlyIfQueued)
	}
	return p.acks.GetAckFrame(protocol.Encryption1RTT, now, onlyIfQueued)
}

// appendPathAcks passes the carrier and the call's carriage to the receive owner,
// which alone selects. Sources without path views keep path-zero behavior.
func (p *packetPacker) appendPathAcks(pl *payload, acks ackhandler.AckCarriage, carrier protocol.PathID, maxSize protocol.ByteCount, now monotime.Time, onlyIfQueued bool, v protocol.Version) {
	source, ok := p.acks.(interface {
		AppendPathAckFrames([]ackhandler.Frame, protocol.ByteCount, monotime.Time, bool, protocol.Version, ackhandler.AckCarriage, protocol.PathID) ([]ackhandler.Frame, protocol.ByteCount)
	})
	if !ok {
		return
	}
	var length protocol.ByteCount
	pl.frames, length = source.AppendPathAckFrames(pl.frames, maxSize-pl.length, now, onlyIfQueued, v, acks, carrier)
	pl.length += length
}
