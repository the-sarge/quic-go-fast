//go:build !darwin

package quic

// sconnBatchState is empty on platforms without a batched send path.
type sconnBatchState struct{}
