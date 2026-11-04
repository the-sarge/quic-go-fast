//go:build !unix

package quic

import "sync/atomic"

// endpointWriteState has no writer without the native endpoint, and a route only
// reads its segmentation fallback.
type endpointWriteState struct {
	gsoDisabled atomic.Bool
}
