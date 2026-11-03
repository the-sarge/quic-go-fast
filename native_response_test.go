//go:build unix

package quic

import (
	"bytes"
	"errors"
	"net"
	"net/netip"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/quic-go/quic-go/internal/handshake"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/utils"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
)

func TestEndpointPublicWriteBorrowsUntilReturn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		payload := []byte("public payload")
		endpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			require.Same(t, &payload[0], &data[0])
			<-release
			return len(data), nil
		}}, time.Second, nil)
		defer func() { <-endpoint.stop() }()
		transport := &Transport{responses: newEndpointResponses(endpoint)}
		returned := make(chan struct{})
		go func() {
			written, err := transport.WriteTo(payload, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234})
			require.NoError(t, err)
			require.Equal(t, len(payload), written)
			close(returned)
		}()
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("public write returned while native still borrowed its bytes")
		default:
		}
		close(release)
		<-returned
	})
}

type endpointResponseTestNative struct {
	endpointTestNative
	sendMetadata func([]byte, []byte, netip.AddrPort) (int, int, error)
}

func (native endpointResponseTestNative) WriteMsgUDPAddrPort(data, oob []byte, destination netip.AddrPort) (int, int, error) {
	return native.sendMetadata(data, oob, destination)
}

func TestEndpointResponseSnapshotsAliases(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
		info := packetInfo{addr: netip.MustParseAddr("127.0.0.2"), ifIndex: 3}
		var payloads []string
		var destinations []netip.AddrPort
		var metadata [][]byte
		endpoint := startNativeEndpoint(endpointResponseTestNative{sendMetadata: func(data, oob []byte, destination netip.AddrPort) (int, int, error) {
			if string(data) == "current" {
				<-release
			}
			payloads = append(payloads, string(data))
			destinations = append(destinations, destination)
			metadata = append(metadata, bytes.Clone(oob))
			return len(data), len(oob), nil
		}}, time.Second, nil)
		defer func() { <-endpoint.stop() }()
		responses := newEndpointResponses(endpoint)
		require.NoError(t, responses.send(nil, nil, []byte("current"), address, packetInfo{}))
		synctest.Wait()
		storage := []byte("aliased Retry/refusal output")
		require.NoError(t, responses.send(nil, nil, storage, address, info))
		clear(storage)
		clear(address.IP)
		address.Port = 4321
		close(release)
		synctest.Wait()
		require.Equal(t, []string{"current", "aliased Retry/refusal output"}, payloads)
		require.Equal(t, netip.MustParseAddrPort("[::ffff:127.0.0.1]:1234"), destinations[1])
		require.Equal(t, info.OOB(), metadata[1])
	})
}

func TestEndpointPublicQueuedExpiryAndClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		writes := 0
		endpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			writes++
			<-release
			return len(data), nil
		}}, time.Second, nil)
		defer func() { <-endpoint.stop() }()
		transport := &Transport{responses: newEndpointResponses(endpoint)}
		address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
		current := make(chan error, 1)
		go func() { _, err := transport.WriteTo([]byte("current"), address); current <- err }()
		synctest.Wait()
		queued := make(chan error, 7)
		for range 7 {
			go func() { _, err := transport.WriteTo([]byte("queued"), address); queued <- err }()
			synctest.Wait()
		}
		written, err := transport.WriteTo([]byte("refused"), address)
		require.Zero(t, written)
		require.ErrorIs(t, err, ErrMultipathBackpressureV1)
		require.NotErrorIs(t, err, ErrMultipathUncertainV1)
		time.Sleep(time.Second)
		for range 7 {
			err := <-queued
			require.ErrorIs(t, err, ErrMultipathDeadlineV1)
			require.ErrorIs(t, err, os.ErrDeadlineExceeded)
			require.ErrorIs(t, err, ErrMultipathBackpressureV1)
			require.NotErrorIs(t, err, ErrMultipathUncertainV1)
		}
		transport.close(nil)
		select {
		case <-current:
			t.Fatal("close returned the application's still-live native borrow")
		default:
		}
		close(release)
		require.NoError(t, <-current)
		require.Equal(t, 1, writes)
	})
}

func TestEndpointPublicDatagramBounds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		writes := 0
		endpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			writes++
			return len(data), nil
		}}, time.Second, nil)
		defer func() { <-endpoint.stop() }()
		transport := &Transport{responses: newEndpointResponses(endpoint)}
		address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
		written, err := transport.WriteTo(nil, address)
		require.NoError(t, err)
		require.Zero(t, written)
		written, err = transport.WriteTo(make([]byte, 1, (64<<10)+1), address)
		require.Zero(t, written)
		require.ErrorIs(t, err, syscall.EMSGSIZE)
		require.Equal(t, 1, writes)
	})
}

func TestEndpointServerResponses(t *testing.T) {
	for _, response := range []string{"retry", "refusal", "invalid-token", "version-negotiation", "stateless-reset"} {
		t.Run(response, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				written := make(chan []byte, 1)
				release := make(chan struct{})
				endpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
					written <- bytes.Clone(data)
					<-release
					return len(data), nil
				}}, time.Second, nil)
				defer func() { close(release); <-endpoint.stop() }()
				server := &baseServer{
					responses:       newEndpointResponses(endpoint),
					logger:          utils.DefaultLogger,
					connIDGenerator: &protocol.DefaultConnectionIDGenerator{},
					tokenGenerator:  handshake.NewTokenGenerator(TokenGeneratorKey{}),
					config:          populateConfig(nil),
				}
				header := &wire.Header{Version: protocol.Version1, SrcConnectionID: protocol.ParseConnectionID([]byte{1, 2}), DestConnectionID: protocol.ParseConnectionID([]byte{3, 4, 5, 6, 7, 8, 9, 10})}
				address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
				switch response {
				case "retry":
					server.sendRetry(rejectedPacket{hdr: header, receivedPacket: receivedPacket{buffer: getPacketBuffer(), remoteAddr: address}})
				case "refusal":
					server.sendConnectionRefused(rejectedPacket{hdr: header, receivedPacket: receivedPacket{buffer: getPacketBuffer(), remoteAddr: address}})
				case "invalid-token":
					sealer, _ := handshake.NewInitialAEAD(header.DestConnectionID, protocol.PerspectiveServer, header.Version)
					require.NoError(t, server.sendError(address, header, sealer, InvalidToken, packetInfo{}))
				case "version-negotiation":
					packet := getLongHeaderPacket(t, address, &wire.ExtendedHeader{Header: wire.Header{Type: protocol.PacketTypeHandshake, Version: 0x42, SrcConnectionID: header.SrcConnectionID, DestConnectionID: header.DestConnectionID}, PacketNumberLen: protocol.PacketNumberLen4}, make([]byte, protocol.MinUnknownVersionPacketSize))
					server.maybeSendVersionNegotiationPacket(packet)
				case "stateless-reset":
					transport := &Transport{responses: server.responses, logger: utils.DefaultLogger, statelessResetter: newStatelessResetter(nil), connIDLen: 8}
					buffer := getPacketBuffer()
					buffer.Data = append(buffer.Data, make([]byte, 40)...)
					buffer.Data[0] = 0x40
					transport.sendStatelessReset(receivedPacket{data: buffer.Data, buffer: buffer, remoteAddr: address})
				}
				synctest.Wait()
				select {
				case data := <-written:
					if response == "version-negotiation" {
						require.True(t, wire.IsVersionNegotiationPacket(data))
						return
					}
					if response == "stateless-reset" {
						require.Len(t, data, protocol.MinStatelessResetSize)
						require.False(t, wire.IsLongHeaderPacket(data[0]))
						return
					}
					parsed, _, _, err := wire.ParsePacket(data)
					require.NoError(t, err)
					if response == "retry" {
						require.Equal(t, protocol.PacketTypeRetry, parsed.Type)
					} else {
						require.Equal(t, protocol.PacketTypeInitial, parsed.Type)
					}
					require.Equal(t, header.SrcConnectionID, parsed.DestConnectionID)
				default:
					t.Fatal("response did not reach the endpoint writer")
				}
			})
		})
	}
}

func TestEndpointPublicErrorCertainty(t *testing.T) {
	for _, nativeError := range []error{os.ErrDeadlineExceeded, syscall.EAGAIN, syscall.ENOBUFS} {
		t.Run(nativeError.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				writes := 0
				endpoint := startNativeEndpoint(endpointTestNative{send: func([]byte) (int, error) {
					writes++
					return 0, nativeError
				}}, time.Second, nil)
				defer func() { <-endpoint.stop() }()
				transport := &Transport{responses: newEndpointResponses(endpoint)}
				written, err := transport.WriteTo([]byte("uncertain"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234})
				require.Zero(t, written)
				require.ErrorIs(t, err, nativeError)
				require.ErrorIs(t, err, ErrMultipathUncertainV1)
				require.ErrorIs(t, err, ErrMultipathBackpressureV1)
				require.Equal(t, errors.Is(nativeError, os.ErrDeadlineExceeded), errors.Is(err, ErrMultipathDeadlineV1))
				require.Equal(t, 1, writes)
			})
		})
	}
}

func TestEndpointResponseEpochRevocation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var submitted []string
		endpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			submitted = append(submitted, string(data))
			if string(data) == "current" {
				<-release
			}
			return len(data), nil
		}}, time.Second, nil)
		defer func() { <-endpoint.stop() }()
		transport := &Transport{responses: newEndpointResponses(endpoint)}
		address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
		require.NoError(t, transport.responses.send(nil, nil, []byte("current"), address, packetInfo{}))
		synctest.Wait()
		running := make(chan struct{})
		close(running)
		server := &baseServer{responses: transport.responses.child(), errorChan: make(chan struct{}), running: running, stopAccepting: make(chan struct{})}
		captured := server.responses
		require.NoError(t, captured.send(nil, nil, []byte("queued"), address, packetInfo{}))
		server.close(ErrServerClosed, true)
		require.ErrorIs(t, captured.send(nil, nil, []byte("stale"), address, packetInfo{}), net.ErrClosed)
		fresh := transport.responses.child()
		require.NoError(t, fresh.send(nil, nil, []byte("fresh"), address, packetInfo{}))
		close(release)
		synctest.Wait()
		require.Equal(t, []string{"current", "fresh"}, submitted)
		transport.close(nil)
		require.ErrorIs(t, fresh.send(nil, nil, []byte("after-close"), address, packetInfo{}), net.ErrClosed)
		written, err := transport.WriteTo([]byte("closed"), address)
		require.Zero(t, written)
		require.ErrorIs(t, err, net.ErrClosed)
		require.Nil(t, transport.conn)
	})
}

func TestEndpointCachedCloseDisposal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		writes := 0
		observed := make(chan string, 1)
		endpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			writes++
			<-release
			observed <- string(data)
			return len(data), nil
		}}, time.Second, nil)
		defer func() { <-endpoint.stop() }()
		transport := &Transport{responses: newEndpointResponses(endpoint), logger: utils.DefaultLogger, listening: make(chan struct{}), closeQueue: make(chan closePacket, 4), handlers: make(map[protocol.ConnectionID]packetHandlerEntry)}
		workerDone := make(chan struct{})
		go func() { transport.runSendQueue(); close(workerDone) }()
		defer func() { close(transport.listening); <-workerDone }()
		identity := protocol.ParseConnectionID([]byte{1, 2, 3, 4})
		handlers := (*packetHandlerMap)(transport)
		require.True(t, handlers.Add(identity, newClosedRemoteConn()))
		payload := []byte("cached close")
		handlers.ReplaceWithClosed([]protocol.ConnectionID{identity}, payload, 10*time.Millisecond)
		cached := transport.handlers[identity].handler
		address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
		cached.handlePacket(receivedPacket{buffer: getPacketBuffer(), remoteAddr: address})
		synctest.Wait()
		time.Sleep(11 * time.Millisecond)
		require.Empty(t, transport.handlers)
		clear(payload)
		cached.handlePacket(receivedPacket{buffer: getPacketBuffer(), remoteAddr: address})
		close(release)
		synctest.Wait()
		require.Equal(t, "cached close", <-observed)
		require.Equal(t, 1, writes)
	})
}

func TestEndpointResponseRevokedDuringDeadlineSetup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		writes := 0
		endpoint := startNativeEndpoint(endpointTestNative{
			deadline: func(deadline time.Time) error {
				if !deadline.IsZero() {
					<-release
				}
				return nil
			},
			send: func(data []byte) (int, error) { writes++; return len(data), nil },
		}, time.Second, nil)
		defer func() { <-endpoint.stop() }()
		responses := newEndpointResponses(endpoint)
		require.NoError(t, responses.send(nil, nil, []byte("claimed"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}, packetInfo{}))
		synctest.Wait()
		responses.revoke()
		close(release)
		synctest.Wait()
		require.Zero(t, writes)
	})
}

func TestEndpointPublicOverdueBorrow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		endpoint := startNativeEndpoint(endpointTestNative{send: func(data []byte) (int, error) {
			<-release
			return len(data), nil
		}}, time.Second, nil)
		defer func() { <-endpoint.stop() }()
		transport := &Transport{responses: newEndpointResponses(endpoint)}
		returned := make(chan int, 1)
		go func() {
			written, _ := transport.WriteTo([]byte("overdue"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234})
			returned <- written
		}()
		synctest.Wait()
		time.Sleep(time.Second + endpointTolerance + time.Millisecond)
		select {
		case <-endpoint.fault:
		default:
			t.Fatal("overdue native request did not fault the endpoint")
		}
		select {
		case <-returned:
			t.Fatal("overdue notification released the caller's native borrow")
		default:
		}
		close(release)
		require.Equal(t, len("overdue"), <-returned)
	})
}
