package model

// QueueBytesByFlow splits the bottleneck FIFO occupancy in charged IP bytes
// between focal and competitor packets. Like next.go, this read-only accessor is
// a measurement overlay for the local two-flow relay (#777); it does not change
// model behavior.
func QueueBytesByFlow(q *Queue) (focal, competitor int) {
	for _, x := range q.fifo {
		if x.Competitor {
			competitor += len(x.Bytes)
		} else {
			focal += len(x.Bytes)
		}
	}
	return focal, competitor
}
