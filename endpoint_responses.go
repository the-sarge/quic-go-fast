package quic

import (
	"errors"
	"net"
	"sync/atomic"
)

var (
	ErrMultipathBackpressureV1 = errors.New("quic: native endpoint backpressure")
	ErrMultipathDeadlineV1     = errors.New("quic: native endpoint deadline exceeded")
	ErrMultipathUncertainV1    = errors.New("quic: native endpoint transmission uncertain")
)

type endpointResponseBackend interface {
	writePublic([]byte, net.Addr, *responseEpoch) (int, error)
	writeResponse([]byte, net.Addr, packetInfo, *responseEpoch) error
	revokeResponse(*responseEpoch)
}

type responseEpoch struct {
	revoked atomic.Bool
	parent  *responseEpoch
}

func (epoch *responseEpoch) valid() bool {
	for current := epoch; current != nil; current = current.parent {
		if current.revoked.Load() {
			return false
		}
	}
	return true
}

func (responses *endpointResponses) send(group *candidateGroup, conn rawConn, data []byte, address net.Addr, info packetInfo) error {
	if responses == nil {
		return writeUnfencedResponse(group, conn, data, address, info)
	}
	if group.isFenced() {
		return nil
	}
	return responses.backend.writeResponse(data, address, info, responses.epoch)
}

type endpointResponses struct {
	backend endpointResponseBackend
	epoch   *responseEpoch
}

func (responses *endpointResponses) child() *endpointResponses {
	if responses == nil {
		return nil
	}
	return &endpointResponses{backend: responses.backend, epoch: &responseEpoch{parent: responses.epoch}}
}

func (responses *endpointResponses) revoke() {
	if responses != nil {
		responses.backend.revokeResponse(responses.epoch)
	}
}
