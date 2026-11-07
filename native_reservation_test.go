//go:build darwin || linux || freebsd

package quic

import "net"

// Test-only reservation fixtures characterize the endpoint arbitration owner.
func (e *nativeEndpoint) prepare(p *endpointParticipant, lane, size int) (endpointAdmission, error) {
	var admission endpointAdmission
	err := net.ErrClosed
	e.do(func(s *endpointState) { admission, err = s.admit(p, lane, size) })
	return admission, err
}

func (e *nativeEndpoint) submit(admission endpointAdmission, request endpointRequest) error {
	err := net.ErrClosed
	e.do(func(s *endpointState) {
		if admission.index < 0 || admission.index >= len(s.records) {
			return
		}
		record := &s.records[admission.index]
		if !record.used || !record.reserved || record.serial != admission.serial || !s.valid(record.participant) || s.sealed || s.faulted {
			return
		}
		err = errEndpointRequest
		size, valid := request.checkedSize(record.participant.class)
		if request.identity.connection != record.participant.id || !valid || size > record.bytes {
			return
		}
		s.commit(admission, request)
		err = nil
	})
	return err
}

func (e *nativeEndpoint) cancelAdmission(admission endpointAdmission) {
	e.do(func(s *endpointState) { s.cancelAdmission(admission) })
}
