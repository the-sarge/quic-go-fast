package self_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/qlog"
	"github.com/quic-go/quic-go/qlogwriter"
	"github.com/quic-go/quic-go/testutils/events"
	"github.com/quic-go/quic-go/testutils/simnet"

	"github.com/stretchr/testify/require"
)

// Model the captured #44 mechanism by frame content, independently of TLS
// packetization: drop eight attempts at one stream range, suppress application
// ACKs, and allow FIN probes while dropping duplicate data. The exact ordinal
// reconstruction remains in docs/audits/issue-44-captured-loss.
type capturedHandshakeLoss struct {
	mu                                sync.Mutex
	control                           string
	pending                           [3][]capturedSentPacket
	mappingError                      string
	run                               [3]int
	maxRun                            int
	seenOffsets                       map[int64]bool
	gapAttempts                       int
	gapStart                          int64
	gapReleased, ackReleased          bool
	gapSent, gapReceived, ackReceived int
	finReceived, suffixReceived       bool
	ptoCount                          uint32
}

// Own scalar snapshots, never the tracer's borrowed frame storage. This fixture
// has one connection per direction and no GSO. PacketSent precedes socket enqueue;
// sum packet lengths to match coalesced datagrams without parsing encrypted data.
type capturedSentPacket struct {
	length       int
	ack, ackOnly bool
	streams      []qlog.StreamFrame
}

func (c *capturedHandshakeLoss) drop(d direction, p simnet.Packet) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	var size, packetCount int
	var ack, ackOnly bool
	var streams []qlog.StreamFrame
	for size < len(p.Data) && len(c.pending[d]) > 0 {
		packet := c.pending[d][0]
		c.pending[d] = c.pending[d][1:]
		size += packet.length
		packetCount++
		ack = ack || packet.ack
		ackOnly = ackOnly || packet.ackOnly
		streams = append(streams, packet.streams...)
	}
	if size != len(p.Data) {
		c.mappingError = fmt.Sprintf("sent metadata %d bytes, datagram %d bytes in direction %s", size, len(p.Data), d)
		return false
	}
	drop := d == directionToClient && ack
	if drop && ackOnly && packetCount == 1 && c.control == "deliver ACK" && !c.ackReleased {
		c.ackReleased = true
		drop = false
	}
	if d == directionToServer {
		for _, f := range streams {
			if f.StreamID != 2 || f.Length == 0 {
				continue
			}
			if f.Offset <= 2508 && f.Offset+f.Length > 2508 {
				if c.gapAttempts == 0 {
					c.gapStart = f.Offset
				}
				c.gapAttempts++
				if c.gapAttempts <= 8 {
					if c.control == "deliver gap" && c.gapAttempts == 1 {
						c.gapReleased = true
					} else {
						drop = true
					}
				}
			} else if c.seenOffsets[f.Offset] && !f.Fin {
				// As in the capture, successful FIN/control probes break the burst
				// counter while repeated data can be dropped without making progress.
				drop = true
			}
			c.seenOffsets[f.Offset] = true
		}
	}
	// Apply the historical guard even when the ACK intervention changes which
	// frames recovery sends. A semantic fault must not create an illegal burst.
	if drop && c.run[d] == 10 {
		drop = false
	}
	if drop {
		c.run[d]++
		c.maxRun = max(c.maxRun, c.run[d])
	} else {
		c.run[d] = 0
	}
	return drop
}

type capturedHandshakeRecorder struct {
	loss   *capturedHandshakeLoss
	client bool
	qlogwriter.Recorder
}

func (r *capturedHandshakeRecorder) RecordEvent(ev qlogwriter.Event) {
	r.Recorder.RecordEvent(ev)
	c := r.loss
	c.mu.Lock()
	defer c.mu.Unlock()
	if pto, ok := ev.(qlog.PTOCountUpdated); ok && r.client {
		c.ptoCount = max(c.ptoCount, pto.PTOCount)
	}
	var frames []qlog.Frame
	var sent bool
	switch e := ev.(type) {
	case qlog.PacketSent:
		packet := capturedSentPacket{length: e.Raw.Length}
		if e.Header.PacketType == qlog.PacketType1RTT {
			for _, frame := range e.Frames {
				switch f := frame.Frame.(type) {
				case *qlog.AckFrame:
					packet.ack = true
					packet.ackOnly = len(e.Frames) == 1
				case *qlog.StreamFrame:
					packet.streams = append(packet.streams, *f)
				}
			}
		}
		direction := directionToClient
		if r.client {
			direction = directionToServer
		}
		c.pending[direction] = append(c.pending[direction], packet)
		if e.Header.PacketType != qlog.PacketType1RTT {
			return
		}
		sent, frames = true, e.Frames
	case qlog.PacketReceived:
		if e.Header.PacketType != qlog.PacketType1RTT {
			return
		}
		frames = e.Frames
	default:
		return
	}
	for _, frame := range frames {
		switch f := frame.Frame.(type) {
		case *qlog.AckFrame:
			if !sent && r.client {
				c.ackReceived++
			}
		case *qlog.StreamFrame:
			if f.StreamID != 2 {
				continue
			}
			gap := f.Offset <= 2508 && f.Offset+f.Length > 2508
			if sent && r.client && gap {
				c.gapSent++
			}
			if !sent && !r.client {
				if gap {
					c.gapReceived++
				}
				c.finReceived = c.finReceived || (f.Fin && f.Offset+f.Length == 5000)
				c.suffixReceived = c.suffixReceived || (f.Offset <= 4999 && f.Offset+f.Length > 4999)
			}
		}
	}
}

func TestHandshakeCapturedLoss(t *testing.T) {
	for _, control := range []string{"ACK blackout", "deliver gap", "deliver ACK"} {
		t.Run(control, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const timeout = 2 * time.Minute
				const rtt = 20 * time.Millisecond
				data := GeneratePRData(5000)
				loss := &capturedHandshakeLoss{control: control, gapStart: -1, seenOffsets: make(map[int64]bool)}
				diagnostics := newHandshakeDiagnostics(t, fmt.Sprintf("captured #44 control=%s post_quantum=true long_chain=true retry=false client_speaks_first version=%s", control, version))
				tracer := func(ctx context.Context, client bool, id quic.ConnectionID) qlogwriter.Trace {
					return &events.Trace{Recorder: &capturedHandshakeRecorder{loss: loss, client: client, Recorder: diagnostics.tracer(ctx, client, id).AddProducer()}}
				}
				clientAddr := &net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9001}
				serverAddr := &net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 9002}
				n := &simnet.Simnet{Router: &directionAwareDroppingRouter{
					ClientAddr: clientAddr, ServerAddr: serverAddr, Drop: diagnostics.observeDrop(loss.drop),
				}}
				settings := simnet.NodeBiDiLinkSettings{Latency: rtt / 2}
				clientSocket := n.NewEndpoint(clientAddr, settings)
				defer clientSocket.Close()
				serverSocket := n.NewEndpoint(serverAddr, settings)
				defer serverSocket.Close()
				require.NoError(t, n.Start())
				defer n.Close()
				serverTransport := &quic.Transport{Conn: serverSocket}
				defer serverTransport.Close()
				config := getQuicConfig(&quic.Config{
					Tracer: tracer, MaxIdleTimeout: timeout, HandshakeIdleTimeout: timeout, DisablePathMTUDiscovery: true,
				})
				ln, err := serverTransport.Listen(getTLSConfigWithLongCertChain(), config)
				require.NoError(t, err)
				defer ln.Close()
				ctx, cancel := context.WithTimeout(t.Context(), timeout)
				defer cancel()
				client, err := quic.Dial(ctx, clientSocket, ln.Addr(), getTLSClientConfig(), config)
				require.NoError(t, err)
				defer diagnostics.closeConnection(true, client)
				require.Equal(t, tls.X25519MLKEM768, getCurveID(client.ConnectionState().TLS))
				stream, err := client.OpenUniStream()
				require.NoError(t, err)
				written, err := stream.Write(data)
				require.NoError(t, err)
				require.Equal(t, len(data), written)
				err = stream.Close()
				diagnostics.phase(true, fmt.Sprintf("close stream returned: %v", err))
				require.NoError(t, err)
				server, err := ln.Accept(ctx)
				require.NoError(t, err)
				defer diagnostics.closeConnection(false, server)
				received, err := server.AcceptUniStream(ctx)
				require.NoError(t, err)
				started := time.Now()
				payload, readErr := io.ReadAll(&readerWithTimeout{Reader: received, Timeout: timeout})
				diagnostics.phase(false, fmt.Sprintf("read stream returned: bytes=%d err=%v", len(payload), readErr))

				// Stream delivery can wake Read before PacketReceived is recorded.
				// Settle producers before inspecting their causal evidence.
				synctest.Wait()

				loss.mu.Lock()
				defer loss.mu.Unlock()
				require.Empty(t, loss.mappingError)
				require.Greater(t, loss.gapStart, int64(0))
				require.Less(t, loss.gapStart, int64(len(data)))
				require.LessOrEqual(t, loss.maxRun, 10, "the captured fault respects the existing random-loss guard")
				require.True(t, loss.finReceived)
				require.True(t, loss.suffixReceived)
				if control == "ACK blackout" {
					require.ErrorIs(t, readErr, os.ErrDeadlineExceeded)
					require.Equal(t, data[:loss.gapStart], payload)
					require.Equal(t, timeout, time.Since(started))
					require.GreaterOrEqual(t, loss.gapSent, 2, "recovery attempted to fill the gap")
					require.Zero(t, loss.gapReceived)
					require.Zero(t, loss.ackReceived)
					require.GreaterOrEqual(t, loss.ptoCount, uint32(2))
				} else {
					require.NoError(t, readErr)
					require.Equal(t, data, payload)
					require.Less(t, time.Since(started), timeout)
					if control == "deliver gap" {
						require.True(t, loss.gapReleased, "the intervention must forward the missing range")
						require.Zero(t, loss.ackReceived, "delivery must succeed despite the ACK blackout")
					} else {
						require.True(t, loss.ackReleased, "the intervention must forward an ACK-only datagram")
						require.Positive(t, loss.ackReceived)
						require.GreaterOrEqual(t, loss.gapSent, 2, "an ACK must enable retransmission of the lost range")
					}
				}
			})
		})
	}
}
