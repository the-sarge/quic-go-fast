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
