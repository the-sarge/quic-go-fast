//go:build unix

package quic

import (
	"errors"
	"net"
	"os"
	"slices"
	"syscall"
)

type endpointDatagram struct {
	data   []byte
	epoch  *responseEpoch
	result chan endpointResult
}

func (endpoint *nativeEndpoint) writeResponse(data []byte, address net.Addr, info packetInfo, epoch *responseEpoch) error {
	destination, ok := address.(*net.UDPAddr)
	if !ok || destination == nil || !destination.AddrPort().IsValid() {
		return &net.AddrError{Err: "invalid native endpoint destination"}
	}
	if len(data) > 64<<10 {
		return syscall.EMSGSIZE
	}
	request := endpointRequest{
		destination: destination.AddrPort(),
		members:     []endpointMember{{datagram: &endpointDatagram{data: slices.Clone(data), epoch: epoch}, oob: info.OOB()}},
	}
	return endpoint.offer(endpoint.stateless, 0, request)
}

func newEndpointResponses(endpoint *nativeEndpoint) *endpointResponses {
	return &endpointResponses{backend: endpoint, epoch: &responseEpoch{}}
}

func (endpoint *nativeEndpoint) revokeResponse(epoch *responseEpoch) {
	endpoint.do(func(state *endpointState) {
		epoch.revoked.Store(true)
		if state.current != nil && !state.current.request.authorized() {
			state.current.canceled.Store(true)
		}
	})
}

func (endpoint *nativeEndpoint) writePublic(data []byte, address net.Addr, epoch *responseEpoch) (int, error) {
	destination, ok := address.(*net.UDPAddr)
	if !ok || destination == nil || !destination.AddrPort().IsValid() {
		return 0, &net.AddrError{Err: "invalid native endpoint destination"}
	}
	if cap(data) > 64<<10 {
		return 0, syscall.EMSGSIZE
	}
	completion := make(chan endpointResult, 1)
	request := endpointRequest{
		destination: destination.AddrPort(),
		members:     []endpointMember{{datagram: &endpointDatagram{data: data, result: completion, epoch: epoch}}},
	}
	if err := endpoint.offer(endpoint.public, 0, request); err != nil {
		if errors.Is(err, errEndpointCapacity) {
			err = errors.Join(ErrMultipathBackpressureV1, err)
		}
		return 0, err
	}
	terminal := <-completion
	result := terminal.members[0]
	err := errors.Join(terminal.err, result.err)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		err = errors.Join(err, ErrMultipathDeadlineV1, ErrMultipathBackpressureV1)
	} else if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.ENOBUFS) {
		err = errors.Join(err, ErrMultipathBackpressureV1)
	}
	if result.uncertain != 0 {
		err = errors.Join(err, ErrMultipathUncertainV1)
	}
	if errors.Is(err, errEndpointRevoked) || errors.Is(err, errEndpointFault) {
		err = errors.Join(err, net.ErrClosed)
	}
	return result.bytes, err
}
