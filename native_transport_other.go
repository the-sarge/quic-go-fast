//go:build !(darwin || linux || freebsd)

package quic

import (
	"context"
	"net"
	"time"

	"github.com/quic-go/quic-go/internal/protocol"
)

// The native endpoint route has no activation on this platform.
func (c *Conn) installNativeRoute(*Transport, net.Addr, packetInfo) error { return nil }

// isEndpointFault has no native endpoint to report here.
func isEndpointFault(error) bool { return false }

// No platform here holds a native grant, so these stay refusals.
func (t *Transport) configureNativeIO(time.Duration) error { return ErrMultipathUnsupportedV1 }

func (t *Transport) waitNativeIOStopped(context.Context) error { return ErrMultipathNotConfiguredV1 }

func (c *Conn) openPathPrivate(context.Context, *Transport, net.Addr) (protocol.PathID, error) {
	return 0, ErrMultipathUnsupportedV1
}

func (c *Conn) migratePathPrivate(context.Context, protocol.PathID, *Transport) error {
	return ErrMultipathUnsupportedV1
}

func (c *Conn) waitTransportDetachedPrivate(context.Context, *Transport) error {
	return ErrMultipathUnsupportedV1
}
