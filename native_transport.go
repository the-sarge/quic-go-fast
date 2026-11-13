//go:build darwin || linux || freebsd

package quic

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// nativeIOGrants holds the sockets with a live write grant. Duplicate descriptors
// stay the caller's contract violation; only the exact pointer can be checked.
var nativeIOGrants = struct {
	sync.Mutex
	sockets map[*net.UDPConn]struct{}
}{sockets: make(map[*net.UDPConn]struct{})}

// configureNativeIO grants the endpoint exclusive write and deadline authority
// over the exact unconnected socket in Conn until waitNativeIOStopped succeeds. It
// must run before initialization. It doesn't take read deadlines or close ownership.
// This is the only installer of the native route, behind ConfigureMultipathIOV1.
func (t *Transport) configureNativeIO(maxWriteDuration time.Duration) error {
	if maxWriteDuration < time.Millisecond || maxWriteDuration > time.Second {
		return fmt.Errorf("%w: write budget %s outside [1ms, 1s]", ErrMultipathInvalidConfigV1, maxWriteDuration)
	}
	t.packetIO.mutex.Lock()
	defer t.packetIO.mutex.Unlock()
	if t.packetIO.started {
		return fmt.Errorf("%w: grant after initialization", ErrMultipathInvalidConfigV1)
	}
	if t.responses != nil {
		return fmt.Errorf("%w: repeated grant", ErrMultipathInvalidConfigV1)
	}
	socket, ok := t.Conn.(*net.UDPConn)
	if !ok || socket == nil || socket.RemoteAddr() != nil {
		return fmt.Errorf("%w: requires an exact unconnected *net.UDPConn", ErrMultipathUnsupportedV1)
	}
	if err := t.nativeIOConflict(); err != nil {
		return err
	}
	nativeIOGrants.Lock()
	defer nativeIOGrants.Unlock()
	if _, held := nativeIOGrants.sockets[socket]; held {
		return fmt.Errorf("%w: the socket already has a live grant", ErrMultipathInvalidConfigV1)
	}
	endpoint, err := newNativeEndpoint(socket, maxWriteDuration, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMultipathUnsupportedV1, err)
	}
	nativeIOGrants.sockets[socket] = struct{}{}
	endpoint.do(func(*endpointState) {
		endpoint.stopped = func() {
			nativeIOGrants.Lock()
			defer nativeIOGrants.Unlock()
			delete(nativeIOGrants.sockets, socket)
		}
	})
	t.nativeIOConn, t.responses = socket, newEndpointResponses(endpoint)
	return nil
}

// installNativeRoute puts a new connection on its transport's endpoint, before the
// connection loop starts. An ungranted transport keeps its existing routes.
func (c *Conn) installNativeRoute(t *Transport, remote net.Addr, info packetInfo) error {
	endpoint := nativeTransportEndpoint(t)
	if endpoint == nil {
		return nil
	}
	destination, ok := nativeDestination(remote)
	if !ok {
		return fmt.Errorf("%w: the remote address is not a UDP address", ErrMultipathUnsupportedV1)
	}
	err := c.useNativeOrdinary(endpoint, destination, info)
	if errors.Is(err, errEndpointCapacity) {
		return fmt.Errorf("%w: %w", ErrMultipathResourceLimitV1, err)
	}
	return err
}

// faultObservation returns the reserved fault record once the endpoint has faulted.
func (e *nativeEndpoint) faultObservation() (endpointFaultRecord, bool) {
	select {
	case <-e.fault:
		return e.faultRecord, true
	default:
		return endpointFaultRecord{}, false
	}
}

// waitNativeIOStopped observes the join that returns write and deadline authority
// over the socket. Success means no coordinator, captured producer or native call
// can touch them again; it does not certify that earlier writes succeeded, and the
// caller sets its own write deadline afterward. A canceled wait only stops waiting.
func (t *Transport) waitNativeIOStopped(ctx context.Context) error {
	endpoint := nativeTransportEndpoint(t)
	if endpoint == nil {
		return ErrMultipathNotConfiguredV1
	}
	t.mutex.Lock()
	closing := t.transportClosed
	t.mutex.Unlock()
	if !closing {
		return ErrMultipathNotStoppingV1
	}
	select {
	case <-endpoint.done:
		return nil
	default:
	}
	select {
	case <-endpoint.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
