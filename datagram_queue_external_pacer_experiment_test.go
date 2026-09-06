//go:build linux && queue_experiment

package quic

// Experimental burst ticks cross a local Unix datagram socket. Payloads stay
// inside the measured queue process; IPC losses/expired ticks are separate.

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const pacedStart = ^uint64(0)
const pacedEnd = pacedStart - 1

func pacedMonotonicNS() int64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		panic(err)
	}
	return ts.Nano()
}

type externalPacedSource struct {
	conn                         *net.UnixConn
	epoch                        time.Time
	received, expired, lastSlot  int64
	generatorSkipped, ipcSkipped int64
}

func prepareExternalPacer(t *testing.T, duration time.Duration) *externalPacedSource {
	t.Helper()
	if os.Getenv("QUEUE_PACING") != "external" {
		return nil
	}
	path := os.Getenv("QUEUE_SOCKET")
	if path == "" {
		t.Fatal("QUEUE_SOCKET required")
	}
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(); os.Remove(path) })
	if err := conn.SetReadDeadline(time.Now().Add(duration + 10*time.Second)); err != nil {
		t.Fatal(err)
	}
	var msg [32]byte
	n, err := conn.Read(msg[:])
	if err != nil || n != len(msg) || binary.LittleEndian.Uint64(msg[:8]) != pacedStart {
		t.Fatalf("invalid external start: n=%d err=%v", n, err)
	}
	mono := int64(binary.LittleEndian.Uint64(msg[8:16]))
	now := time.Now()
	epoch := now.Add(time.Duration(mono - pacedMonotonicNS()))
	return &externalPacedSource{conn: conn, epoch: epoch, lastSlot: -1}
}

func (s *externalPacedSource) next(t *testing.T, bursts int64) (int64, bool) {
	var msg [32]byte
	n, err := s.conn.Read(msg[:])
	if err != nil || n != len(msg) {
		t.Fatalf("external tick read: n=%d err=%v", n, err)
	}
	slot := binary.LittleEndian.Uint64(msg[:8])
	if slot == pacedEnd {
		s.generatorSkipped = int64(binary.LittleEndian.Uint64(msg[8:16]))
		s.ipcSkipped = int64(binary.LittleEndian.Uint64(msg[16:24]))
		if s.received+s.generatorSkipped+s.ipcSkipped != bursts {
			t.Fatal("external tick accounting failure")
		}
		return 0, false
	}
	if int64(slot) <= s.lastSlot || slot >= uint64(bursts) {
		t.Fatal("external tick order/range failure")
	}
	s.lastSlot = int64(slot)
	s.received++
	return int64(slot), true
}

func TestDatagramExternalPacer(t *testing.T) {
	if os.Getenv("QUEUE_PACER") != "1" {
		t.Skip("external pacer subprocess only")
	}
	rate, burst := pacedEnvInt(t, "QUEUE_RATE"), pacedEnvInt(t, "QUEUE_BURST")
	duration := time.Duration(pacedEnvInt(t, "QUEUE_DURATION_MS")) * time.Millisecond
	total := int64(rate) * duration.Nanoseconds() / int64(time.Second)
	if total%int64(burst) != 0 {
		t.Fatal("partial burst")
	}
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := unix.Connect(fd, &unix.SockaddrUnix{Name: os.Getenv("QUEUE_SOCKET")}); err != nil {
		t.Fatal(err)
	}
	epoch := pacedMonotonicNS() + int64(50*time.Millisecond)
	var msg [32]byte
	binary.LittleEndian.PutUint64(msg[:8], pacedStart)
	binary.LittleEndian.PutUint64(msg[8:16], uint64(epoch))
	if _, err := unix.Write(fd, msg[:]); err != nil {
		t.Fatal(err)
	}
	var sent, skipped, ipcSkipped int64
	bursts := total / int64(burst)
	interval := int64(burst) * int64(time.Second) / int64(rate)
	lateness := new(pacedHistogram)
	cpuStart := pacedCPUSeconds()
	for slot := int64(0); slot < bursts; slot++ {
		deadline := epoch + slot*interval
		for pacedMonotonicNS() < deadline {
		}
		now := pacedMonotonicNS()
		if now >= epoch+int64(duration) {
			skipped += bursts - slot
			break
		}
		if now-deadline >= interval {
			latest := min((now-epoch)/interval, bursts-1)
			skipped += latest - slot
			slot = latest
			deadline = epoch + slot*interval
		}
		lateness.add(now - deadline)
		binary.LittleEndian.PutUint64(msg[:8], uint64(slot))
		_, err := unix.Write(fd, msg[:])
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			ipcSkipped++
		} else if err != nil {
			t.Fatal(err)
		} else {
			sent++
		}
	}
	for pacedMonotonicNS() < epoch+int64(duration) {
	}
	cpuSeconds := pacedCPUSeconds() - cpuStart
	binary.LittleEndian.PutUint64(msg[:8], pacedEnd)
	binary.LittleEndian.PutUint64(msg[8:16], uint64(skipped))
	binary.LittleEndian.PutUint64(msg[16:24], uint64(ipcSkipped))
	finishDeadline := time.Now().Add(5 * time.Second)
	for {
		_, err := unix.Write(fd, msg[:])
		if err == nil {
			break
		}
		if (err != unix.EAGAIN && err != unix.EWOULDBLOCK) || time.Now().After(finishDeadline) {
			t.Fatal(err)
		}
		time.Sleep(100 * time.Microsecond)
	}
	if sent+skipped+ipcSkipped != bursts {
		t.Fatal("pacer accounting failure")
	}
	encoded, err := json.Marshal(map[string]any{"sent_ticks": sent, "generator_skipped_ticks": skipped,
		"ipc_skipped_ticks": ipcSkipped, "cpu_seconds": cpuSeconds, "lateness": lateness.summary()})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("PACER_RESULT %s\n", encoded)
}
