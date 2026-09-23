//go:build windows

package quic

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestWindowsManagedECNRoundTrip(t *testing.T) {
	t.Setenv("QUIC_GO_DISABLE_ECN", "false")
	t.Setenv("QUIC_GO_DISABLE_GRO", "true")
	for _, family := range []struct{ name, network, ip string }{
		{"ipv4", "udp4", "127.0.0.1"},
		{"ipv6", "udp6", "::1"},
		{"dual-v4", "udp", "127.0.0.1"},
		{"dual-v6", "udp", "::1"},
	} {
		for _, checked := range []bool{false, true} {
			t.Run(family.name+map[bool]string{false: "/direct", true: "/checked"}[checked], func(t *testing.T) {
				endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(family.network, &net.UDPAddr{})
				require.NoError(t, err)
				t.Cleanup(func() { endpoint.Close() })
				lease, err := acquire()
				require.NoError(t, err)
				t.Cleanup(func() { lease.Close() })
				conn := lease
				var callback func([][]byte, []byte, *net.UDPAddr) (int, error)
				if checked {
					conn = &struct{ net.PacketConn }{lease}
					callback = lease.(managedBatchWriterV1).WriteBatchV1
				}
				tr := &Transport{Conn: conn}
				require.NoError(t, tr.ConfigureManagedPacketIOV1(conn, lease, callback))
				raw := tr.wrapExternalPacketIO(&basicConn{PacketConn: conn})
				require.True(t, raw.capabilities().ECN)
				require.False(t, raw.capabilities().GSO)
				require.False(t, raw.capabilities().GRO)
				require.NoError(t, lease.SetDeadline(time.Now().Add(5*time.Second)))
				addr := &net.UDPAddr{IP: net.ParseIP(family.ip), Port: endpoint.LocalAddr().(*net.UDPAddr).Port}
				for _, mark := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1} {
					n, err := raw.WritePacket([]byte("marked datagram"), addr, nil, 0, mark)
					require.NoError(t, err)
					require.Equal(t, len("marked datagram"), n)
					packet, err := raw.ReadPacket()
					require.NoError(t, err)
					require.Equal(t, []byte("marked datagram"), packet.data)
					require.Equal(t, mark, packet.ecn)
					packet.buffer.Release()
				}
			})
		}
	}
}

func TestWindowsManagedECNFamilySetup(t *testing.T) {
	for _, tc := range []struct {
		name             string
		v4, v6, disabled bool
		denied           int
		want             bool
	}{
		{"v4 ignores v6", true, false, false, windows.IPPROTO_IPV6, true},
		{"v6 ignores v4", false, true, false, windows.IPPROTO_IP, true},
		{"dual", true, true, false, -1, true},
		{"dual v4 denial", true, true, false, windows.IPPROTO_IP, false},
		{"dual v6 denial", true, true, false, windows.IPPROTO_IPV6, false},
		{"unknown", false, false, false, -1, false},
		{"disabled", true, true, true, -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := setupWindowsECNFamilies(managedECNQualification{admittedIPv4: tc.v4, admittedIPv6: tc.v6, disabled: tc.disabled}, func(level int) error {
				if level == tc.denied {
					return windows.WSAENOPROTOOPT
				}
				return nil
			})
			require.Equal(t, tc.want, q.qualified)
		})
	}
}

func TestWindowsManagedECNControlMessages(t *testing.T) {
	good := appendIPv4ECNMsg(nil, protocol.ECT0)
	invalidValue := bytes.Clone(good)
	binary.NativeEndian.PutUint32(invalidValue[wsaCmsgDataOffset:], 4)
	short, _ := appendCmsg(nil, windows.IPPROTO_IP, windowsECN, 3)
	unknown, _ := appendCmsg(nil, windows.IPPROTO_UDP, 999, 4)
	badLength := bytes.Clone(good)
	(*windows.WSACMSGHDR)(unsafe.Pointer(&badLength[0])).Len = ^uintptr(0)
	for _, tc := range []struct {
		name  string
		oob   []byte
		want  protocol.ECN
		valid bool
	}{
		{"absent", nil, protocol.ECNUnsupported, true},
		{"unrelated", unknown, protocol.ECNUnsupported, true},
		{"not ect", appendIPv4ECNMsg(nil, protocol.ECNNon), protocol.ECNNon, true},
		{"ect0", good, protocol.ECT0, true},
		{"ect1", appendIPv6ECNMsg(nil, protocol.ECT1), protocol.ECT1, true},
		{"ce", appendIPv6ECNMsg(nil, protocol.ECNCE), protocol.ECNCE, true},
		{"short value", short, 0, false},
		{"invalid value", invalidValue, 0, false},
		{"header bounds", badLength, 0, false},
		{"truncated", good[:wsaCmsgDataOffset+2], 0, false},
		{"duplicate", append(bytes.Clone(good), good...), 0, false},
		{"conflicting family", append(bytes.Clone(good), appendIPv6ECNMsg(nil, protocol.ECT1)...), 0, false},
		{"trailing junk", append(bytes.Clone(good), 1), 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, ecn, valid := parseWindowsControlMessages(tc.oob, true)
			require.Equal(t, tc.valid, valid)
			if valid {
				require.Equal(t, tc.want, ecn)
			}
		})
	}
}

type windowsECNMessageFixture struct {
	OOBCapablePacketConn
	payload, oob []byte
	firstOOB     []byte
	firstFlags   int
	firstErr     error
	calls        int
	n            int
	writeErr     error
}

func (c *windowsECNMessageFixture) ReadMsgUDP(b, oob []byte) (int, int, int, *net.UDPAddr, error) {
	c.calls++
	n := copy(b, c.payload)
	o := c.oob
	flags := 0
	var err error
	if c.calls == 1 {
		o = c.firstOOB
		flags = c.firstFlags
		err = c.firstErr
	}
	return n, copy(oob, o), flags, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, err
}

func (c *windowsECNMessageFixture) WriteMsgUDP(b, oob []byte, _ *net.UDPAddr) (int, int, error) {
	return c.n, len(oob), c.writeErr
}

func TestWindowsManagedECNReadRecovery(t *testing.T) {
	for _, tc := range []struct {
		name  string
		oob   []byte
		flags int
		err   error
	}{
		{name: "malformed", oob: []byte{1}},
		{name: "payload truncation", flags: windows.MSG_TRUNC},
		{name: "control truncation", flags: windows.MSG_CTRUNC},
		{name: "native truncation", err: windows.WSAEMSGSIZE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &windowsECNMessageFixture{payload: bytes.Repeat([]byte{7}, 4000), oob: appendIPv4ECNMsg(nil, protocol.ECT1), firstOOB: tc.oob, firstFlags: tc.flags, firstErr: tc.err}
			c := &windowsConn{OOBCapablePacketConn: f, managed: true, oobBuffer: make([]byte, oobBufferSize)}
			p, err := c.ReadPacket()
			require.NoError(t, err)
			defer p.buffer.Release()
			require.Equal(t, f.payload, p.data)
			require.Equal(t, protocol.ECT1, p.ecn)
			require.Equal(t, 2, f.calls)
		})
	}
}

func TestWindowsManagedECNSendResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		err  error
		want int
	}{
		{"zero", 0, nil, 7},
		{"full", 7, nil, 7},
		{"short", 2, nil, 2},
		{"invalid", 0, windows.WSAEINVAL, 0},
		{"message size", 0, windows.WSAEMSGSIZE, 0},
	} {
		for _, mark := range []protocol.ECN{protocol.ECT0, protocol.ECNUnsupported} {
			t.Run(tc.name+mark.String(), func(t *testing.T) {
				f := &windowsECNMessageFixture{n: tc.n, writeErr: tc.err}
				c := &windowsConn{OOBCapablePacketConn: f, managed: true}
				oob := appendIPv4ECNMsg(nil, protocol.ECT0)
				n, err := c.WritePacket([]byte("payload"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, oob, 0, mark)
				require.Equal(t, tc.want, n)
				require.Equal(t, tc.err, err)
			})
		}
	}
}

func TestWindowsManagedECNCoalescedMetadata(t *testing.T) {
	oob := append(coalescedInfoMsg(t, 100), appendIPv4ECNMsg(nil, protocol.ECNCE)...)
	rc := &uroReadConn{t: t, payloads: [][]byte{bytes.Repeat([]byte{3}, 300)}, oobs: [][]byte{oob}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	c := newUROConn(t, rc)
	c.managed = true
	first, err := c.ReadPacket()
	require.NoError(t, err)
	second, err := c.ReadPacket()
	require.NoError(t, err)
	require.Equal(t, protocol.ECNCE, first.ecn)
	require.Equal(t, protocol.ECNCE, second.ecn)
	require.Equal(t, first.buffer.slab, second.buffer.slab)
	slab := first.buffer.slab
	c.releaseReadBuffers()
	first.buffer.Release()
	require.False(t, slab.released())
	second.buffer.Release()
	require.True(t, slab.released())
	require.Equal(t, 1, rc.callCounter)
}

func TestWindowsManagedECNLeaseReuse(t *testing.T) {
	for _, uro := range []string{"true", "false"} {
		t.Run("disable URO="+uro, func(t *testing.T) {
			t.Setenv("QUIC_GO_DISABLE_GRO", uro)
			t.Setenv("QUIC_GO_DISABLE_ECN", "false")
			endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			require.NoError(t, err)
			defer endpoint.Close()
			lease, err := acquire()
			require.NoError(t, err)
			tr := &Transport{Conn: lease}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(lease, lease, nil))
			e := endpoint.(*managedPacketConn).endpoint
			reader := e.receiver
			peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			require.NoError(t, err)
			defer peer.Close()
			payload := bytes.Repeat([]byte{5}, 4000)
			_, err = peer.WriteTo(payload, endpoint.LocalAddr())
			require.NoError(t, err)
			_, err = peer.WriteTo([]byte("queued"), endpoint.LocalAddr())
			require.NoError(t, err)
			require.NoError(t, lease.SetReadDeadline(time.Now().Add(5*time.Second)))
			b := make([]byte, 5000)
			n, _, err := lease.ReadFrom(b)
			require.NoError(t, err)
			require.Equal(t, payload, b[:n])
			require.NoError(t, lease.Close())
			t.Setenv("QUIC_GO_DISABLE_ECN", "true")
			next, err := acquire()
			require.NoError(t, err)
			tr = &Transport{Conn: next}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(next, next, nil))
			require.False(t, e.managedECN)
			if uro == "false" {
				require.Same(t, reader, e.receiver)
			} else {
				require.Nil(t, e.receiver)
			}
			require.NoError(t, next.SetReadDeadline(time.Now().Add(5*time.Second)))
			n, _, err = next.ReadFrom(b)
			require.NoError(t, err)
			require.Equal(t, "queued", string(b[:n]))
			require.NoError(t, next.Close())
			t.Setenv("QUIC_GO_DISABLE_ECN", "false")
			last, err := acquire()
			require.NoError(t, err)
			defer last.Close()
			tr = &Transport{Conn: last}
			require.NoError(t, tr.ConfigureManagedPacketIOV1(last, last, nil))
			require.True(t, e.managedECN)
			if uro == "false" {
				require.Same(t, reader, e.receiver)
			}
		})
	}
}

// The independent Linux peer observes outgoing IP marks and originates CE.
// Qualification supplies its two addresses; ordinary CI uses the local tests.
func TestWindowsManagedECNIndependentPeer(t *testing.T) {
	peer4, peer6 := os.Getenv("QGF_WINDOWS_ECN_PEER4"), os.Getenv("QGF_WINDOWS_ECN_PEER6")
	if peer4 == "" || peer6 == "" {
		t.Skip("requires the bounded native qualification peer")
	}
	t.Setenv("QUIC_GO_DISABLE_ECN", "false")
	t.Setenv("QUIC_GO_DISABLE_GRO", "true")
	for _, family := range []struct{ name, network, peer string }{
		{"ipv4", "udp4", peer4}, {"ipv6", "udp6", peer6}, {"dual-v4", "udp", peer4}, {"dual-v6", "udp", peer6},
	} {
		for _, route := range []string{"direct", "checked", "batch"} {
			t.Run(family.name+"/"+route, func(t *testing.T) {
				addr, err := net.ResolveUDPAddr("udp", family.peer)
				require.NoError(t, err)
				endpoint, acquire, err := (&Transport{}).NewManagedPacketEndpointV1(family.network, &net.UDPAddr{})
				require.NoError(t, err)
				defer endpoint.Close()
				lease, err := acquire()
				require.NoError(t, err)
				defer lease.Close()
				conn := lease
				var callback func([][]byte, []byte, *net.UDPAddr) (int, error)
				if route != "direct" {
					conn = &windowsManagedFilter{PacketConn: lease, peer: addr, filtered: make(chan struct{}, 1)}
					callback = lease.(managedBatchWriterV1).WriteBatchV1
				}
				tr := &Transport{Conn: conn}
				require.NoError(t, tr.ConfigureManagedPacketIOV1(conn, lease, callback))
				raw := tr.wrapExternalPacketIO(&basicConn{PacketConn: conn})
				require.True(t, raw.capabilities().ECN)
				require.NoError(t, lease.SetDeadline(time.Now().Add(15*time.Second)))
				for i, received := range []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1, protocol.ECNCE} {
					outgoing := []protocol.ECN{protocol.ECNNon, protocol.ECT0, protocol.ECT1}[i%3]
					payload := append([]byte{outgoing.ToHeaderBits(), received.ToHeaderBits()}, []byte(t.Name())...)
					count := 1
					if route == "batch" {
						count = 2
						n, err := lease.(managedBatchWriterV1).WriteBatchV1([][]byte{payload, payload}, appendExternalECN(nil, addr, outgoing), addr)
						require.NoError(t, err)
						require.Equal(t, 2, n)
					} else {
						n, err := raw.WritePacket(payload, addr, nil, 0, outgoing)
						require.NoError(t, err)
						require.Equal(t, len(payload), n)
					}
					for range count {
						p, err := raw.ReadPacket()
						require.NoError(t, err)
						require.Equal(t, payload, p.data)
						require.Equal(t, received, p.ecn)
						p.buffer.Release()
					}
					t.Logf("outgoing=%d incoming=%d payload=%d packets=%d", outgoing.ToHeaderBits(), received.ToHeaderBits(), len(payload), count)
				}
				_, err = raw.WritePacket([]byte("CE must fail"), addr, nil, 0, protocol.ECNCE)
				require.ErrorIs(t, err, windows.WSAEINVAL)
			})
		}
	}
}

func TestWindowsManagedECNCheckedErrors(t *testing.T) {
	for _, failure := range []error{windows.WSAEINVAL, windows.WSAEMSGSIZE} {
		for _, normalize := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/normalize=%t", failure, normalize), func(t *testing.T) {
				native := &managedECNFixtureConn{writeResult: failure}
				lease := &managedPacketLease{done: make(chan struct{})}
				endpoint := &managedPacketEndpoint{managedNative: native, managedECN: true, lease: lease}
				endpoint.idle = sync.NewCond(&endpoint.mutex)
				conn := &managedPacketConn{endpoint: endpoint, lease: lease}
				cb := func(b [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
					n, err := conn.WriteBatchV1(b, oob, addr)
					if normalize {
						return 0, nil
					}
					return n, err
				}
				raw := newManagedPacketRawConn(native, conn, &externalPacketIO{sendBatch: cb}, false)
				n, err := raw.WritePacket([]byte("marked"), &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}, nil, 0, protocol.ECT0)
				require.Zero(t, n)
				if normalize {
					require.ErrorContains(t, err, "unchanged")
				} else {
					require.Equal(t, failure, err)
				}
			})
		}
	}
}
