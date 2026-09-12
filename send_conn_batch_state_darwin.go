//go:build darwin

package quic

// sconnBatchState carries the darwin batched-send state embedded in sconn.
// batch holds a *darwinBatch once the sendmsg_x path initializes it (always
// nil under the ios/opt-out stub, which never reads it). Accessed only from
// the sendQueue.Run goroutine.
type sconnBatchState struct {
	batch any
}
