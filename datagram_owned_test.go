package quic

import (
	"bytes"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
)

func newOwnedDatagramTestConn() *Conn {
	c := &Conn{peerParams: &wire.TransportParameters{MaxDatagramFrameSize: 65535}, version: protocol.Version1}
	c.datagramQueue = newDatagramQueue(func() {}, utils.DefaultLogger)
	c.maxPayloadSizeEstimate.Store(1192)
	return c
}

func TestSendDatagramOwnedContextTransferAndCopying(t *testing.T) {
	for _, ordinary := range []bool{false, true} {
		c := newOwnedDatagramTestConn()
		payload := []byte("one independent record")
		var err error
		if ordinary {
			err = c.SendDatagramContext(t.Context(), payload)
		} else {
			err = c.SendDatagramOwnedContextV1(t.Context(), payload)
		}
		if err != nil {
			t.Fatalf("admission: got %v, want nil", err)
		}
		frame := c.datagramQueue.Peek()
		if frame == nil || !bytes.Equal(frame.Data, payload) {
			t.Fatal("payload: got missing or changed record, want original bytes")
		}
		if ordinary {
			payload[0] = 'X'
			if frame.Data[0] != 'o' {
				t.Fatal("copying admission: got caller mutation, want isolated payload")
			}
		} else if &frame.Data[0] != &payload[0] {
			t.Fatal("owned admission: got copied allocation, want transferred allocation")
		}
	}
	c := newOwnedDatagramTestConn()
	if err := c.SendDatagramOwnedContextV1(t.Context(), nil); err != nil {
		t.Fatalf("empty admission: got %v, want nil", err)
	}
	c = newOwnedDatagramTestConn()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.SendDatagramOwnedContextV1(ctx, make([]byte, 65536)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled validation: got %v, want canceled", err)
	}
	if c.datagramQueue.Peek() != nil {
		t.Fatal("canceled admission: got queued frame, want none")
	}
}

func TestSendDatagramOwnedContextBlockedCancellationAndSeal(t *testing.T) {
	for _, stop := range []string{"cancel", "deadline", "seal"} {
		seal := stop == "seal"
		synctest.Test(t, func(t *testing.T) {
			c := newOwnedDatagramTestConn()
			for range maxDatagramSendQueueLen {
				if err := c.SendDatagramOwnedContextV1(context.Background(), []byte("queued")); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- c.SendDatagramOwnedContextV1(ctx, []byte("must not commit")) }()
			synctest.Wait()
			if seal {
				c.SealDatagramAdmissionV1(nil)
				c.SealDatagramAdmissionV1(nil)
				c.datagramQueue.CloseWithError(errors.New("later connection error"))
			} else if stop == "cancel" {
				cancel()
			} else {
				time.Sleep(5 * time.Millisecond)
			}
			synctest.Wait()
			err := <-done
			if seal && !errors.Is(err, net.ErrClosed) {
				t.Fatalf("sealed wait: got %v, want closed", err)
			}
			want := context.Canceled
			if stop == "deadline" {
				want = context.DeadlineExceeded
			}
			if !seal && !errors.Is(err, want) {
				t.Fatalf("canceled wait: got %v, want %v", err, want)
			}
			if c.datagramQueue.sendQueue.Len() != maxDatagramSendQueueLen {
				t.Fatalf("failed admission queue: got %d, want %d", c.datagramQueue.sendQueue.Len(), maxDatagramSendQueueLen)
			}
			if seal {
				c.datagramQueue.Pop()
				if err := c.SendDatagramOwnedContextV1(context.Background(), nil); !errors.Is(err, net.ErrClosed) {
					t.Fatalf("admission after seal with capacity: got %v, want closed", err)
				}
			}
		})
	}
}

func TestSealDatagramAdmissionFence(t *testing.T) {
	c := newOwnedDatagramTestConn()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _ = c.SendDatagramOwnedContextV1(t.Context(), []byte("racing")) })
	}
	c.SealDatagramAdmissionV1(nil)
	wg.Wait()
	before := c.datagramQueue.sendQueue.Len()
	if err := c.SendDatagramOwnedContextV1(t.Context(), []byte("after")); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("post-seal admission: got %v, want closed", err)
	}
	if got := c.datagramQueue.sendQueue.Len(); got != before {
		t.Fatalf("post-seal queue: got %d, want %d", got, before)
	}
}

func TestSealDatagramAdmissionWithoutQueue(t *testing.T) {
	c := new(Conn)
	c.SealDatagramAdmissionV1(nil)
	c.SealDatagramAdmissionV1(nil)
}

func TestSealDatagramAdmissionPreservesReason(t *testing.T) {
	c := newOwnedDatagramTestConn()
	reason := errors.New("owner sealed")
	c.SealDatagramAdmissionV1(reason)
	c.SealDatagramAdmissionV1(errors.New("later owner"))
	c.datagramQueue.CloseWithError(errors.New("later connection"))
	if err := c.SendDatagramOwnedContextV1(t.Context(), []byte("late")); err != reason {
		t.Fatalf("sealed cause: got %v, want %v", err, reason)
	}
}
