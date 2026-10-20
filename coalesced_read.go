//go:build darwin || linux || freebsd || windows

package quic

import "github.com/quic-go/quic-go/internal/protocol"

// coalescedReadCursor tracks one coalesced read while ReadPacket copies its
// datagrams out of the reader's buffer, one per call. Only the reader uses it,
// and the buffer is not read into again until the cursor is inactive.
type coalescedReadCursor struct {
	active bool
	// packet carries the read's metadata; its data is the undelivered payload.
	packet      receivedPacket
	segmentSize int
}

// start begins delivering a read whose payload is p.data. A segment size of
// zero means the read is a single datagram.
func (c *coalescedReadCursor) start(p receivedPacket, segmentSize int) {
	if segmentSize == 0 {
		segmentSize = len(p.data)
	}
	*c = coalescedReadCursor{active: true, packet: p, segmentSize: segmentSize}
}

// next copies the next datagram of the cursor's read out of the reader's
// buffer. The read's datagrams share its metadata.
func (c *coalescedReadCursor) next(managed bool) (receivedPacket, bool) {
	if !c.active {
		return receivedPacket{}, false
	}
	p := c.packet
	remaining := p.data
	n := min(c.segmentSize, len(remaining))
	p.buffer = copyCoalescedDatagram(remaining[:n], managed)
	p.data = p.buffer.Data
	if n == len(remaining) {
		*c = coalescedReadCursor{}
	} else {
		c.packet.data = remaining[n:]
	}
	return p, true
}

// copyCoalescedDatagram copies one datagram into the smallest buffer tier
// that fits. A datagram above the large tier is delivered as the reader
// without coalescing would deliver it (#853): a managed endpoint delivers it
// whole, and every other reader reads into the ordinary tier, where only its
// prefix fits.
func copyCoalescedDatagram(datagram []byte, managed bool) *packetBuffer {
	var buffer *packetBuffer
	switch {
	case len(datagram) <= protocol.MaxPacketBufferSize:
		buffer = getPacketBuffer()
	case len(datagram) <= protocol.MaxLargePacketBufferSize:
		buffer = getLargePacketBuffer()
	case managed:
		buffer = getCoalescedPacketBuffer()
	default:
		buffer = getPacketBuffer()
		datagram = datagram[:protocol.MaxPacketBufferSize]
	}
	buffer.Data = append(buffer.Data, datagram...)
	return buffer
}
