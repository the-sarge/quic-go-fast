package wire

import (
	"errors"
	"io"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/quicvarint"
)

// MaxDatagramSize is the maximum size of a DATAGRAM frame (RFC 9221).
// By setting it to a large value, we allow all datagrams that fit into a QUIC packet.
// The value is chosen such that it can still be encoded as a 2 byte varint.
// This is a var and not a const so it can be set in tests.
var MaxDatagramSize protocol.ByteCount = 16383

// A DatagramFrame is a DATAGRAM frame
type DatagramFrame struct {
	DataLenPresent bool
	// Retain the length of a reusable send copy after its bytes are detached.
	reusableDataLen uint32
	// Data borrows the input buffer for parsed frames. Copy it before retaining
	// it beyond input reuse or modifying it independently of the input.
	Data []byte
}

// SetReusableData attaches a nonempty, library-owned send copy.
func (f *DatagramFrame) SetReusableData(data []byte) {
	f.Data = data
	f.reusableDataLen = uint32(len(data))
}

// DetachReusableData releases a send copy after serialization. The frame keeps
// its payload length for tracing and its type for packet accounting.
func (f *DatagramFrame) DetachReusableData() []byte {
	if f.reusableDataLen == 0 {
		return nil
	}
	data := f.Data
	f.Data = nil
	return data
}

// DataLen reports the payload length, including after serialization.
func (f *DatagramFrame) DataLen() int {
	if f.reusableDataLen != 0 {
		return int(f.reusableDataLen)
	}
	return len(f.Data)
}

func parseDatagramFrame(b []byte, typ FrameType, _ protocol.Version) (*DatagramFrame, int, error) {
	startLen := len(b)
	f := &DatagramFrame{}
	f.DataLenPresent = uint64(typ)&0x1 > 0

	var length uint64
	if f.DataLenPresent {
		var err error
		var l int
		length, l, err = quicvarint.Parse(b)
		if err != nil {
			return nil, 0, replaceUnexpectedEOF(err)
		}
		b = b[l:]
		if length > uint64(len(b)) {
			return nil, 0, io.EOF
		}
	} else {
		length = uint64(len(b))
	}
	f.Data = b[:length:length]
	return f, startLen - len(b) + int(length), nil
}

func (f *DatagramFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	if f.Data == nil && f.reusableDataLen != 0 {
		return nil, errors.New("cannot append a DATAGRAM frame after releasing its payload")
	}
	typ := uint8(0x30)
	if f.DataLenPresent {
		typ ^= 0b1
	}
	b = append(b, typ)
	if f.DataLenPresent {
		b = quicvarint.Append(b, uint64(len(f.Data)))
	}
	b = append(b, f.Data...)
	return b, nil
}

// MaxDataLen returns the maximum data length
func (f *DatagramFrame) MaxDataLen(maxSize protocol.ByteCount, version protocol.Version) protocol.ByteCount {
	headerLen := protocol.ByteCount(1)
	if f.DataLenPresent {
		// pretend that the data size will be 1 bytes
		// if it turns out that varint encoding the length will consume 2 bytes, we need to adjust the data length afterwards
		headerLen++
	}
	if headerLen > maxSize {
		return 0
	}
	maxDataLen := maxSize - headerLen
	if f.DataLenPresent && quicvarint.Len(uint64(maxDataLen)) != 1 {
		maxDataLen--
	}
	return maxDataLen
}

// Length of a written frame
func (f *DatagramFrame) Length(_ protocol.Version) protocol.ByteCount {
	dataLen := f.DataLen()
	length := 1 + protocol.ByteCount(dataLen)
	if f.DataLenPresent {
		length += protocol.ByteCount(quicvarint.Len(uint64(dataLen)))
	}
	return length
}
