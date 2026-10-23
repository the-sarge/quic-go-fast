package quic

import "sync/atomic"

// Measurement-only overlay for the #710 attribution. It is copied into
// exported build trees and is never part of a tracked transport source.
var diagOccupancy struct {
	rxLen, rxMax                       atomic.Int64
	connReceived, connRead, connOccMax atomic.Int64
}

func diagRxQueue(n int) {
	v := int64(n)
	diagOccupancy.rxLen.Store(v)
	for m := diagOccupancy.rxMax.Load(); v > m && !diagOccupancy.rxMax.CompareAndSwap(m, v); m = diagOccupancy.rxMax.Load() {
	}
}

func diagConnReceived(highest int64) {
	diagOccupancy.connReceived.Store(highest)
	v := highest - diagOccupancy.connRead.Load()
	for m := diagOccupancy.connOccMax.Load(); v > m && !diagOccupancy.connOccMax.CompareAndSwap(m, v); m = diagOccupancy.connOccMax.Load() {
	}
}

func diagConnRead(read int64) { diagOccupancy.connRead.Store(read) }

// DiagnosticOccupancy returns the receive queue length and its maximum since
// the previous call, plus connection-level flow-control offsets and the
// largest received-but-unread byte count since the previous call.
func DiagnosticOccupancy() (rxLen, rxMax, connReceived, connRead, connOccMax int64) {
	return diagOccupancy.rxLen.Load(), diagOccupancy.rxMax.Swap(0), diagOccupancy.connReceived.Load(),
		diagOccupancy.connRead.Load(), diagOccupancy.connOccMax.Swap(0)
}
