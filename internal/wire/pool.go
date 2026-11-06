package wire

import (
	"sync"

	"github.com/quic-go/quic-go/internal/protocol"
)

const maxStreamFrameBufferSize = 8972

var (
	pool                 sync.Pool
	jumboStreamFramePool sync.Pool
)

func init() {
	pool.New = func() any {
		return &StreamFrame{
			Data:     make([]byte, 0, protocol.MaxPacketBufferSize),
			fromPool: true,
		}
	}
	jumboStreamFramePool.New = func() any {
		return &StreamFrame{
			Data:     make([]byte, 0, maxStreamFrameBufferSize),
			fromPool: true,
		}
	}
}

func GetStreamFrame() *StreamFrame {
	f := pool.Get().(*StreamFrame)
	return f
}

// GetStreamFrameForSize obtains a frame whose buffer fits the payload.
func GetStreamFrameForSize(size protocol.ByteCount) *StreamFrame {
	if size <= protocol.MaxPacketBufferSize {
		return GetStreamFrame()
	}
	return jumboStreamFramePool.Get().(*StreamFrame)
}

// TruncateData keeps the first n bytes, releasing jumbo storage when the
// retained payload fits the standard tier.
func (f *StreamFrame) TruncateData(n protocol.ByteCount) {
	f.Data = f.Data[:n]
	if n > protocol.MaxPacketBufferSize || cap(f.Data) <= int(protocol.MaxPacketBufferSize) {
		return
	}
	standard := GetStreamFrame()
	standard.Data = standard.Data[:n]
	copy(standard.Data, f.Data)
	// The temporary frame returns the previous storage with its original ownership.
	f.Data, standard.Data = standard.Data, f.Data
	f.fromPool, standard.fromPool = standard.fromPool, f.fromPool
	standard.PutBack()
}

func putStreamFrame(f *StreamFrame) {
	if !f.fromPool {
		return
	}
	switch cap(f.Data) {
	case int(protocol.MaxPacketBufferSize):
		pool.Put(f)
	case maxStreamFrameBufferSize:
		jumboStreamFramePool.Put(f)
	default:
		panic("wire.PutStreamFrame called with packet of wrong size!")
	}
}
