package self_test

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/ipv4"
)

func TestCorruptionCaptureBatchFinalization(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ipv4.PacketConn.ReadBatch is not implemented on Windows")
	}
	testCorruptionCaptureBatchFinalization(t, 1)
}

func testCorruptionCaptureBatchFinalization(t *testing.T, count int) {
	t.Helper()
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	c := newCorruptionCapture(t, "batch finalization")
	udp, sender := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	require.NoError(t, udp.SetReadDeadline(time.Now().Add(5*time.Second)))
	socket := &corruptionUDPConn{UDPConn: udp, batch: ipv4.NewPacketConn(udp), capture: c, source: "socket controlled"}
	messages := make([]ipv4.Message, count)
	for i := range messages {
		messages[i] = ipv4.Message{Buffers: [][]byte{make([]byte, 128)}, OOB: make([]byte, 128)}
		_, err := sender.WriteTo([]byte(fmt.Sprintf("member-%d", i)), udp.LocalAddr())
		require.NoError(t, err)
	}
	paused, release := make(chan struct{}), make(chan struct{})
	c.afterBatchResult = func() { close(paused); <-release }
	var workers sync.WaitGroup
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		_ = udp.Close()
		workers.Wait()
	})
	var n int
	var readErr error
	workers.Go(func() { n, readErr = socket.ReadBatch(messages, 0) })
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("receive observation did not reach the result boundary")
	}
	available, finished := make(chan bool), make(chan struct{})
	workers.Go(func() {
		// Probe only to choose a deterministic schedule: if finalization can
		// acquire serialization, let it finish before releasing the observer.
		// Otherwise the admitted observation must finish before finalization.
		unlocked := c.mu.TryLock()
		if unlocked {
			c.mu.Unlock()
		}
		available <- unlocked
		c.finish("controlled overlapping finalization")
		close(finished)
	})
	if <-available {
		<-finished
	}
	releaseOnce.Do(func() { close(release) })
	workers.Wait()
	require.NoError(t, readErr)
	require.Equal(t, count, n)
	records := readCorruptionCapture(t, c.path)
	require.Len(t, records, count+3)
	require.Contains(t, records[1].Data, fmt.Sprintf("operation=read_batch batch=1 result_messages=%d", count))
	for i, msg := range messages {
		payload := []byte(fmt.Sprintf("member-%d", i))
		require.Equal(t, payload, msg.Buffers[0][:msg.N])
		require.Contains(t, records[i+2].Source, fmt.Sprintf("batch=1 index=%d count=%d", i, count))
		require.Contains(t, records[i+2].Data, fmt.Sprintf("payload=%x oob=%x", payload, msg.OOB[:msg.NN]))
		require.Contains(t, records[i+2].Data, fmt.Sprintf("result_bytes=%d flags_or_oob_bytes=%d error=<nil>", msg.N, msg.Flags))
	}
	require.Equal(t, "dial_finished", records[len(records)-1].Source)
	require.Contains(t, records[len(records)-1].Data, "dropped_or_truncated=0")
}

func TestCorruptionCaptureBatchAfterFinalization(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ipv4.PacketConn.ReadBatch is not implemented on Windows")
	}
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	c := newCorruptionCapture(t, "batch outside admission")
	udp, sender := newUDPConnLocalhost(t), newUDPConnLocalhost(t)
	require.NoError(t, udp.SetReadDeadline(time.Now().Add(5*time.Second)))
	socket := &corruptionUDPConn{UDPConn: udp, batch: ipv4.NewPacketConn(udp), capture: c, source: "socket controlled"}
	paused, release := make(chan struct{}), make(chan struct{})
	socket.afterBatchRead = func() { close(paused); <-release }
	messages := []ipv4.Message{{Buffers: [][]byte{make([]byte, 128)}, OOB: make([]byte, 128)}}
	var workers sync.WaitGroup
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		_ = udp.Close()
		workers.Wait()
	})
	var n int
	var readErr error
	workers.Go(func() { n, readErr = socket.ReadBatch(messages, 0) })
	_, err := sender.WriteTo([]byte("outside"), udp.LocalAddr())
	require.NoError(t, err)
	select {
	case <-paused:
	case <-time.After(5 * time.Second):
		t.Fatal("socket read did not complete")
	}
	// The socket has returned, but its observation has not been admitted.
	c.finish("before batch admission")
	before, err := os.ReadFile(c.path)
	require.NoError(t, err)
	releaseOnce.Do(func() { close(release) })
	workers.Wait()
	require.NoError(t, readErr)
	require.Equal(t, 1, n)
	require.Equal(t, "outside", string(messages[0].Buffers[0][:messages[0].N]))
	after, err := os.ReadFile(c.path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Len(t, readCorruptionCapture(t, c.path), 2)
}

func TestCorruptionCaptureBatchIncomplete(t *testing.T) {
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	for _, mode := range []string{"event budget", "record truncation", "write failure"} {
		t.Run(mode, func(t *testing.T) {
			c := newCorruptionCapture(t, mode)
			payload := []byte(strings.Repeat("x", 512))
			messages := []ipv4.Message{{Buffers: [][]byte{payload}, N: len(payload)}, {Buffers: [][]byte{[]byte("last")}, N: 4}}
			switch mode {
			case "event budget":
				// Fill the real file, leaving room for the result but not a member.
				for corruptionCaptureBytes-c.written > 1024 {
					c.record(time.Now(), "filler", strings.Repeat("x", min(corruptionEventBytes, corruptionCaptureBytes-c.written-512)))
				}
			case "record truncation":
				// Artificial recorder input, deliberately larger than a UDP datagram.
				messages[0].Buffers = [][]byte{[]byte(strings.Repeat("x", corruptionEventBytes))}
				messages[0].N = corruptionEventBytes
			case "write failure":
				c.afterBatchResult = func() { require.NoError(t, c.file.Close()) }
			}
			c.readBatch("socket controlled", 7, messages, 2, 0, nil, nil)
			c.finish("incomplete batch")
			records := readCorruptionCapture(t, c.path)
			var result, members, limits int
			for _, record := range records {
				if strings.Contains(record.Data, "operation=read_batch batch=7 result_messages=2") {
					result++
				}
				if strings.Contains(record.Source, "batch=7 index=") {
					members++
				}
				if record.Source == "capture_limit" {
					limits++
				}
				require.LessOrEqual(t, len(record.Data), corruptionEventBytes)
			}
			require.Equal(t, 1, result)
			info, err := os.Stat(c.path)
			require.NoError(t, err)
			require.Less(t, info.Size(), int64(corruptionCaptureBytes+4096))
			last := records[len(records)-1]
			if mode == "write failure" {
				require.Error(t, c.err)
				require.Zero(t, members)
				require.NotEqual(t, "dial_finished", last.Source)
				return
			}
			require.NoError(t, c.err)
			require.Equal(t, "dial_finished", last.Source)
			require.Contains(t, last.Data, "dropped_or_truncated=")
			require.NotContains(t, last.Data, "dropped_or_truncated=0")
			if mode == "event budget" {
				require.Equal(t, 1, limits)
				require.Zero(t, members)
			} else {
				require.Equal(t, 2, members)
				require.Contains(t, records[2].Data, "[capture record truncated]")
			}
		})
	}
}

func TestCorruptionCaptureBatchResults(t *testing.T) {
	t.Setenv("QUIC_GO_CORRUPTION_CAPTURE_DIR", t.TempDir())
	c := newCorruptionCapture(t, "batch result reporting")
	c.readBatch("socket controlled", 1, nil, 0, 3, nil, nil)
	c.readBatch("socket controlled", 2, nil, -1, 4, net.ErrClosed, nil)
	// Recorder coverage for populated scatter buffers and nonempty ancillary
	// bytes complements the actual socket path, whose OOB support is OS-owned.
	messages := []ipv4.Message{{Buffers: [][]byte{[]byte("fir"), []byte("st-unused")}, N: 5, OOB: []byte{1, 2, 3}, NN: 2, Flags: 6}}
	c.readBatch("socket controlled", 3, messages, 1, 5, nil, nil)
	c.finish("batch results")
	records := readCorruptionCapture(t, c.path)
	require.Len(t, records, 6)
	require.Contains(t, records[1].Data, "batch=1 result_messages=0 flags=3 error=<nil>")
	require.Contains(t, records[2].Data, "batch=2 result_messages=-1 flags=4 error=use of closed network connection")
	require.Contains(t, records[3].Data, "batch=3 result_messages=1 flags=5 error=<nil>")
	require.Contains(t, records[4].Source, "batch=3 index=0 count=1")
	require.Contains(t, records[4].Data, "result_bytes=5 flags_or_oob_bytes=6 error=<nil>")
	require.Contains(t, records[4].Data, "payload=6669727374 oob=0102")
	require.Equal(t, "first-unused", string(messages[0].Buffers[0])+string(messages[0].Buffers[1]))
	require.Equal(t, []byte{1, 2, 3}, messages[0].OOB)
}
