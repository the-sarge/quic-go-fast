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
	c := &Conn{peerParams: &wire.TransportParameters{MaxDatagramFrameSize: 65535}, version: protocol.Version1, config: &Config{EnableDatagrams: true}}
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
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() { results <- c.SendDatagramOwnedContextV1(t.Context(), []byte("racing")) })
	}
	c.SealDatagramAdmissionV1(nil)
	c.datagramQueue.sendMx.Lock()
	before := c.datagramQueue.sendQueue.Len()
	c.datagramQueue.sendMx.Unlock()
	wg.Wait()
	for range 8 {
		if err := <-results; err != nil && !errors.Is(err, net.ErrClosed) {
			t.Fatalf("racing admission: got %v, want nil or closed", err)
		}
	}
	if err := c.SendDatagramOwnedContextV1(t.Context(), []byte("after")); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("post-seal admission: got %v, want closed", err)
	}
	c.datagramQueue.sendMx.Lock()
	got := c.datagramQueue.sendQueue.Len()
	c.datagramQueue.sendMx.Unlock()
	if got != before {
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

func TestSealDatagramAdmissionDisabledSupport(t *testing.T) {
	send := newOwnedDatagramTestConn()
	send.peerParams.MaxDatagramFrameSize = 0
	send.SealDatagramAdmissionV1(nil)
	if err := send.SendDatagramOwnedContextV1(t.Context(), []byte("disabled")); err == nil || err.Error() != "datagram support disabled" {
		t.Fatalf("disabled send: got %v, want datagram support disabled", err)
	}
	send.datagramQueue.CloseWithError(errors.New("later close"))

	receive := newOwnedDatagramTestConn()
	receive.config.EnableDatagrams = false
	receive.SealDatagramAdmissionV1(nil)
	if payload, err := receive.ReceiveDatagram(t.Context()); payload != nil || err == nil || err.Error() != "datagram support disabled" {
		t.Fatalf("disabled receive: got %q/%v, want nil/datagram support disabled", payload, err)
	}
	receive.datagramQueue.CloseWithError(errors.New("later close"))
}

func TestSealDatagramAdmissionReceiveDraining(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newOwnedDatagramTestConn()
		reason := errors.New("owner sealed")
		type result struct {
			payload []byte
			err     error
		}
		done := make(chan result, 1)
		go func() {
			payload, err := c.ReceiveDatagram(context.Background())
			done <- result{payload, err}
		}()
		synctest.Wait()
		c.SealDatagramAdmissionV1(reason)
		synctest.Wait()
		got := <-done
		if got.payload != nil || got.err != reason {
			t.Fatalf("blocked receive: got %q/%v, want nil/%v", got.payload, got.err, reason)
		}
		c.datagramQueue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("after seal")})
		if payload, err := c.ReceiveDatagram(t.Context()); string(payload) != "after seal" || err != nil {
			t.Fatalf("later receive: got %q/%v, want after seal/nil", payload, err)
		}
		if payload, err := c.ReceiveDatagram(t.Context()); payload != nil || err != reason {
			t.Fatalf("empty receive: got %q/%v, want nil/%v", payload, err, reason)
		}
	})

	c := newOwnedDatagramTestConn()
	c.datagramQueue.HandleDatagramFrame(&wire.DatagramFrame{Data: []byte("before seal")})
	c.SealDatagramAdmissionV1(nil)
	if payload, err := c.ReceiveDatagram(t.Context()); string(payload) != "before seal" || err != nil {
		t.Fatalf("queued receive: got %q/%v, want before seal/nil", payload, err)
	}
}

func TestSealDatagramAdmissionConcurrentConnectionClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newOwnedDatagramTestConn()
		for range maxDatagramSendQueueLen {
			if err := c.SendDatagramOwnedContextV1(t.Context(), []byte("queued")); err != nil {
				t.Fatal(err)
			}
		}
		blocked := make(chan error, 1)
		go func() { blocked <- c.SendDatagramOwnedContextV1(context.Background(), []byte("blocked")) }()
		synctest.Wait()
		sealReason, closeReason := errors.New("owner sealed"), errors.New("connection closed")
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() { <-start; c.SealDatagramAdmissionV1(sealReason) })
		wg.Go(func() { <-start; c.datagramQueue.CloseWithError(closeReason) })
		close(start)
		wg.Wait()
		synctest.Wait()
		winner := <-blocked
		if winner != sealReason && winner != closeReason {
			t.Fatalf("concurrent cause: got %v, want %v or %v", winner, sealReason, closeReason)
		}
		if got := c.datagramQueue.sendQueue.Len(); got != maxDatagramSendQueueLen {
			t.Fatalf("blocked admission: got queue length %d, want %d", got, maxDatagramSendQueueLen)
		}
		c.SealDatagramAdmissionV1(errors.New("later seal"))
		c.datagramQueue.CloseWithError(errors.New("later close"))
		if err := c.SendDatagramOwnedContextV1(t.Context(), nil); err != winner {
			t.Fatalf("later admission cause: got %v, want %v", err, winner)
		}
	})
}
