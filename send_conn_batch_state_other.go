//go:build !darwin

package quic

import "github.com/quic-go/quic-go/internal/protocol"

// sconnBatchState is empty on platforms without a batched send path.
type sconnBatchState struct{}

func (c *sconn) nativeBatchSendAvailable() bool { return false }

func (c *sconn) sendNativeBatch([][]byte, protocol.ECN) (int, error) { return 0, nil }
