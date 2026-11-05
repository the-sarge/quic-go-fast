package quic

import (
	"errors"
	"net"
	"sync/atomic"
)

var (
	// ErrMultipathBackpressureV1 identifies local capacity or deadline backpressure.
	// It does not establish non-submission or authorize replay. The native endpoint
	// route currently operates only in private fixtures; no public constructor installs it.
	ErrMultipathBackpressureV1 = errors.New("quic: native endpoint backpressure")
	// ErrMultipathDeadlineV1 identifies expiry of a local endpoint request deadline.
	// Returned deadline errors also match os.ErrDeadlineExceeded and ErrMultipathBackpressureV1.
	ErrMultipathDeadlineV1 = errors.New("quic: native endpoint deadline exceeded")
	// ErrMultipathUncertainV1 identifies a native result that cannot prove non-submission.
	// Neither this error nor a zero byte count authorizes replay.
	ErrMultipathUncertainV1 = errors.New("quic: native endpoint transmission uncertain")

	// The identities below belong to the same private route: until its public
	// activation, only private fixtures can receive them.

	// ErrMultipathInvalidConfigV1 identifies a refused native endpoint grant:
	// a budget outside its range, a repeated grant or a late one.
	ErrMultipathInvalidConfigV1 = errors.New("quic: invalid native endpoint configuration")
	// ErrMultipathUnsupportedV1 identifies a socket or transport combination outside
	// the native endpoint's support domain.
	ErrMultipathUnsupportedV1 = errors.New("quic: native endpoint combination unsupported")
	// ErrMultipathResourceLimitV1 identifies an exhausted native endpoint participant budget.
	ErrMultipathResourceLimitV1 = errors.New("quic: native endpoint resource limit reached")
	// ErrMultipathNotConfiguredV1 reports a join on a transport without a native endpoint grant.
	ErrMultipathNotConfiguredV1 = errors.New("quic: native endpoint not configured")
	// ErrMultipathNotStoppingV1 reports a join requested before the transport was closed.
	ErrMultipathNotStoppingV1 = errors.New("quic: native endpoint not stopping")
)

type endpointResponseBackend interface {
	writePublic([]byte, net.Addr, *responseEpoch) (int, error)
	writeResponse([]byte, net.Addr, packetInfo, *responseEpoch) error
	revokeResponse(*responseEpoch)
	awaitDepositedCloses()
	seal()
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

func (responses *endpointResponses) awaitDepositedCloses() {
	if responses != nil {
		responses.backend.awaitDepositedCloses()
	}
}

func (responses *endpointResponses) seal() {
	if responses != nil {
		responses.backend.seal()
	}
}
