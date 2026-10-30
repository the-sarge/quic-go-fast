package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
)

const headerBytes = 16
const maxSequence = 1 << 30 // At most 128 MiB of duplicate bitmap per connection.

type Delivery struct {
	UsefulBytes    uint64   `json:"useful_bytes"`
	UniqueMessages uint64   `json:"unique_messages"`
	Duplicates     uint64   `json:"duplicates"`
	Corrupt        uint64   `json:"corrupt"`
	OutsideWindow  uint64   `json:"outside_window"`
	PerSecondBytes []uint64 `json:"per_second_bytes"`
}

// Counter is the receiver-consumption boundary for this frozen campaign only.
// One receive goroutine owns it; Snapshot is called after that goroutine joins.
type Counter struct {
	start, end time.Time
	seen       []uint64
	delivery   Delivery
}

func NewCounter(start time.Time, duration time.Duration) *Counter {
	return &Counter{start: start, end: start.Add(duration), delivery: Delivery{
		PerSecondBytes: make([]uint64, int((duration+time.Second-1)/time.Second)),
	}}
}

func (c *Counter) Record(p []byte, at time.Time) error {
	invalid := func() error { c.delivery.Corrupt++; return fmt.Errorf("invalid campaign payload") }
	if len(p) < headerBytes || !bytes.Equal(p[:4], []byte("Q1B1")) || int(binary.BigEndian.Uint32(p[12:16])) != len(p)-headerBytes {
		return invalid()
	}
	seq := binary.BigEndian.Uint64(p[4:12])
	if seq >= maxSequence {
		return invalid()
	}
	for i, b := range p[headerBytes:] {
		if b != byte(seq+uint64(i)) {
			return invalid()
		}
	}
	word, bit := int(seq/64), uint(seq%64)
	if word >= len(c.seen) {
		// Grow in bounded 64K-sequence pages. No map entry per DATAGRAM.
		n := min(((word/1024)+1)*1024, maxSequence/64)
		c.seen = append(c.seen, make([]uint64, n-len(c.seen))...)
	}
	if c.seen[word]&(uint64(1)<<bit) != 0 {
		c.delivery.Duplicates++
		return nil
	}
	c.seen[word] |= uint64(1) << bit
	if at.Before(c.start) || !at.Before(c.end) {
		c.delivery.OutsideWindow++
		return nil
	}
	n := uint64(len(p) - headerBytes)
	c.delivery.UsefulBytes += n
	c.delivery.UniqueMessages++
	c.delivery.PerSecondBytes[int(at.Sub(c.start)/time.Second)] += n
	return nil
}

func (c *Counter) Snapshot() Delivery { return c.delivery }

func encodePayload(p []byte, seq uint64) {
	copy(p, "Q1B1")
	binary.BigEndian.PutUint64(p[4:12], seq)
	binary.BigEndian.PutUint32(p[12:16], uint32(len(p)-headerBytes))
	for i := range p[headerBytes:] {
		p[headerBytes+i] = byte(seq + uint64(i))
	}
}
