package main

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestReceiverIntegrityAndDuplicateAccounting(t *testing.T) {
	start := time.Unix(100, 0)
	c := NewCounter(start, 2*time.Second)
	// The fixture format is Q1B1, uint64 sequence, uint32 content length,
	// followed by content byte i = byte(sequence+i). Metadata never counts.
	p := []byte{'Q', '1', 'B', '1', 0, 0, 0, 0, 0, 0, 0, 7, 0, 0, 0, 4, 7, 8, 9, 10}
	if err := c.Record(p, start.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if err := c.Record(p, start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), p...)
	corrupt[18] ^= 1
	if err := c.Record(corrupt, start.Add(time.Second)); err == nil {
		t.Fatal("corruption accepted")
	}
	next := append([]byte(nil), p...)
	binary.BigEndian.PutUint64(next[4:12], 8)
	copy(next[16:], []byte{8, 9, 10, 11})
	if err := c.Record(next, start.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	s := c.Snapshot()
	if s.UsefulBytes != 4 || s.UniqueMessages != 1 || s.Duplicates != 1 || s.Corrupt != 1 || s.OutsideWindow != 1 {
		t.Fatalf("wrong receiver-defined accounting: %+v", s)
	}
	if len(s.PerSecondBytes) != 2 || s.PerSecondBytes[0] != 4 || s.PerSecondBytes[1] != 0 {
		t.Fatalf("wrong time series: %+v", s)
	}
}
