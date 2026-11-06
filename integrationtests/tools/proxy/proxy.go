// Package quicproxy preserves the integration-test proxy import surface.
// Implementation and private tests live in retained internal test support.
package quicproxy

import proxy "github.com/quic-go/quic-go/internal/testutils/quicproxy"

type (
	Proxy         = proxy.Proxy
	Direction     = proxy.Direction
	DropCallback  = proxy.DropCallback
	DelayCallback = proxy.DelayCallback
	SocketEvent   = proxy.SocketEvent
)

const (
	DirectionIncoming = proxy.DirectionIncoming
	DirectionOutgoing = proxy.DirectionOutgoing
	DirectionBoth     = proxy.DirectionBoth
)
