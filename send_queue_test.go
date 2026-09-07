package quic

import (
	"net"
	"net/netip"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func getPacketWithContents(b []byte) *packetBuffer {
	buf := getPacketBuffer()
	buf.Data = buf.Data[:len(b)]
	copy(buf.Data, b)
	return buf
}

func TestSendQueueSendOnePacket(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		c := NewMockSendConn(mockCtrl)
		q := newSendQueue(c, nil)

		written := make(chan struct{})
		c.EXPECT().Write([]byte("foobar"), uint16(10), protocol.ECT1).Do(
			func([]byte, uint16, protocol.ECN) error { close(written); return nil },
		)

		done := make(chan struct{})
		go func() {
			q.Run()
			close(done)
		}()

		buf := getPacketWithContents([]byte("foobar"))
		q.Send(buf, 10, protocol.ECT1, sendMetadata{})
		synctest.Wait()

		select {
		case <-written:
		default:
			t.Fatal("write should have returned")
		}

		q.Close()
		synctest.Wait()
		require.Zero(t, buf.refCount)

		select {
		case <-done:
		default:
			t.Fatal("Run should have returned")
		}
	})
}

func TestSendQueueBlocking(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		c := NewMockSendConn(mockCtrl)
		q := newSendQueue(c, nil)

		blockWrite := make(chan struct{})
		written := make(chan struct{}, 1)
		c.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Do(
			func([]byte, uint16, protocol.ECN) error {
				select {
				case written <- struct{}{}:
				default:
				}
				<-blockWrite
				return nil
			},
		).AnyTimes()

		done := make(chan struct{})
		go func() {
			q.Run()
			close(done)
		}()

		// Allocate all buffers before release so pool reuse cannot mask lifetime checks.
		buffers := make([]*packetBuffer, sendQueueCapacity+1)
		for i := range buffers {
			buffers[i] = getPacketWithContents([]byte("foobar"))
		}
		// +1, since one packet will be queued in the Write call
		for i := range sendQueueCapacity + 1 {
			require.False(t, q.WouldBlock())
			q.Send(buffers[i], 10, protocol.ECT1, sendMetadata{})
			// make sure that the first packet is actually enqueued in the Write call
			if i == 0 {
				select {
				case <-written:
				case <-time.After(time.Second):
					t.Fatal("timeout")
				}
			}
		}
		require.True(t, q.WouldBlock())
		select {
		case <-q.Available():
			t.Fatal("should not be available")
		default:
		}
		overflow := getPacketWithContents([]byte("overflow"))
		require.Panics(t, func() { q.Send(overflow, 10, protocol.ECT1, sendMetadata{}) })
		overflow.Release() // The caller violated the capacity precondition; no handoff occurred.

		// allow one packet to be sent
		blockWrite <- struct{}{}
		select {
		case <-written:
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
		select {
		case <-q.Available():
			require.False(t, q.WouldBlock())
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}

		// when calling Close, all packets are first sent out
		closed := make(chan struct{})
		go func() {
			q.Close()
			close(closed)
		}()

		synctest.Wait()

		select {
		case <-closed:
			t.Fatal("Close should have blocked")
		default:
		}
		for _, buf := range buffers[1:] {
			require.Equal(t, 1, buf.refCount, "Close must not release storage still owned by the worker")
		}

		for range sendQueueCapacity {
			blockWrite <- struct{}{}
		}
		synctest.Wait()

		select {
		case <-closed:
		default:
			t.Fatal("Close should have returned")
		}
		select {
		case <-done:
		default:
			t.Fatal("Run should have returned")
		}
		for _, buf := range buffers {
			require.Zero(t, buf.refCount, "graceful close must release every sent buffer")
		}
	})
}

func TestSendQueueWriteError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		c := NewMockSendConn(mockCtrl)
		q := newSendQueue(c, nil)

		c.EXPECT().Write(gomock.Any(), gomock.Any(), gomock.Any()).Return(assert.AnError)
		buf := getPacketWithContents([]byte("foobar"))
		q.Send(buf, 6, protocol.ECNNon, sendMetadata{})

		errChan := make(chan error, 1)
		go func() { errChan <- q.Run() }()

		synctest.Wait()

		select {
		case err := <-errChan:
			require.ErrorIs(t, err, assert.AnError)
		default:
			t.Fatal("Run should have returned")
		}
		require.Zero(t, buf.refCount, "a fatal write must release its active buffer")

		// further calls to Send should not block
		sent := make(chan struct{})
		go func() {
			defer close(sent)
			for range 2 * sendQueueCapacity {
				q.Send(getPacketWithContents([]byte("raboof")), 6, protocol.ECNNon, sendMetadata{})
			}
		}()

		synctest.Wait()

		select {
		case <-sent:
		default:
			t.Fatal("Send should have returned")
		}
		q.Close()
	})
}

func TestSendQueueStoppedSubmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := NewMockSendConn(gomock.NewController(t))
		q := newSendQueue(c, nil)
		active := getPacketWithContents([]byte("active"))
		pending := make([]*packetBuffer, sendQueueCapacity)
		for i := range pending {
			pending[i] = getPacketWithContents([]byte("pending"))
		}
		rejected := getPacketWithContents([]byte("rejected"))
		finishWrite := make(chan struct{})
		c.EXPECT().Write(active.Data, uint16(0), protocol.ECNNon).DoAndReturn(
			func([]byte, uint16, protocol.ECN) error {
				<-finishWrite
				return assert.AnError
			},
		)
		q.Send(active, 0, protocol.ECNNon, sendMetadata{})
		result := make(chan error, 1)
		go func() { result <- q.Run() }()
		synctest.Wait()
		for _, buf := range pending {
			q.Send(buf, 0, protocol.ECNNon, sendMetadata{})
		}
		require.True(t, q.WouldBlock())
		close(finishWrite)
		require.ErrorIs(t, <-result, assert.AnError)
		// A full queue forces the stopped-submission branch after the worker exits.
		q.Send(rejected, 0, protocol.ECNNon, sendMetadata{})
		require.Zero(t, rejected.refCount, "stopped submission must consume its buffer")
		q.Close()
		for _, buf := range pending {
			require.Zero(t, buf.refCount, "Close must release buffers left by a failed worker")
		}
	})
}

func TestSendQueueEnqueueAtWorkerExit(t *testing.T) {
	for _, timing := range []string{"before", "concurrent", "after"} {
		t.Run(timing, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := NewMockSendConn(gomock.NewController(t))
				q := newSendQueue(c, nil)
				active := getPacketWithContents([]byte("active"))
				pending := getPacketWithContents([]byte("pending"))
				finishWrite := make(chan struct{})
				c.EXPECT().Write(active.Data, uint16(0), protocol.ECNNon).DoAndReturn(
					func([]byte, uint16, protocol.ECN) error {
						<-finishWrite
						return assert.AnError
					},
				)
				q.Send(active, 0, protocol.ECNNon, sendMetadata{})
				result := make(chan error, 1)
				go func() { result <- q.Run() }()
				synctest.Wait()
				switch timing {
				case "before":
					q.Send(pending, 0, protocol.ECNNon, sendMetadata{})
					closed := make(chan struct{})
					go func() { q.Close(); close(closed) }()
					synctest.Wait()
					select {
					case <-closed:
						t.Fatal("Close returned while the writer still owns storage")
					default:
					}
					require.Equal(t, 1, active.refCount)
					require.Equal(t, 1, pending.refCount)
					close(finishWrite)
					<-closed
				case "concurrent":
					go func() { close(finishWrite) }()
					q.Send(pending, 0, protocol.ECNNon, sendMetadata{})
					q.Close()
				case "after":
					close(finishWrite)
					synctest.Wait()
					q.Send(pending, 0, protocol.ECNNon, sendMetadata{})
					q.Close()
				}
				require.ErrorIs(t, <-result, assert.AnError)
				require.Zero(t, active.refCount)
				require.Zero(t, pending.refCount, "both stopped rejection and late enqueue must be reclaimed")
			})
		})
	}
}

func TestSendQueueSendProbe(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	c := NewMockSendConn(mockCtrl)
	q := newSendQueue(c, nil)

	addr := &net.UDPAddr{IP: net.IPv4(42, 42, 42, 42), Port: 42}
	localAddr := netip.MustParseAddr("43.43.43.43")
	c.EXPECT().WriteTo([]byte("foobar"), addr, packetInfo{
		addr: localAddr,
	})
	q.SendProbe(getPacketWithContents([]byte("foobar")), addr, packetInfo{
		addr: localAddr,
	})
}
