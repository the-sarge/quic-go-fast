package model

import (
	"math"
	"time"
)

// NoEvent reports an empty queue. This read-only accessor is a demonstration
// overlay for the local relay; it does not change model behavior.
const NoEvent = time.Duration(math.MaxInt64)

// NextEvent returns the earliest pending service finish or propagation delivery.
func NextEvent(q *Queue) time.Duration {
	next := NoEvent
	if len(q.fifo) > 0 {
		next = q.fifo[0].Finish
	}
	if len(q.pending) > 0 {
		next = min(next, q.pending[0].Due)
	}
	return next
}

// QueueBytes returns the bottleneck FIFO occupancy in charged IP bytes.
func QueueBytes(q *Queue) int { return q.bytes }
