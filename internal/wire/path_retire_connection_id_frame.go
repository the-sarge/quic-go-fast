// Adapted from qoke/mp-quic-go at 084c13ea3834fbe58cedb63db9208951d714f839.
// Copyright (c) 2016 the quic-go authors & Google, Inc. Licensed under the MIT license in ../../LICENSE.

package wire

import (
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/quicvarint"
)

// A PathRetireConnectionIDFrame is a PATH_RETIRE_CONNECTION_ID frame of the multipath extension.
// Sequence numbers are counted per path.
type PathRetireConnectionIDFrame struct {
	PathID         protocol.PathID
	SequenceNumber uint64
}

func parsePathRetireConnectionIDFrame(b []byte, _ protocol.Version) (*PathRetireConnectionIDFrame, int, error) {
	startLen := len(b)
	pathID, l, err := parsePathID(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	b = b[l:]
	seq, l, err := quicvarint.Parse(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	b = b[l:]
	return &PathRetireConnectionIDFrame{
		PathID:         pathID,
		SequenceNumber: seq,
	}, startLen - len(b), nil
}

func (f *PathRetireConnectionIDFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	b = quicvarint.Append(b, uint64(FrameTypePathRetireConnectionID))
	b = quicvarint.Append(b, uint64(f.PathID))
	return quicvarint.Append(b, f.SequenceNumber), nil
}

// Length of a written frame
func (f *PathRetireConnectionIDFrame) Length(protocol.Version) protocol.ByteCount {
	return protocol.ByteCount(2 + quicvarint.Len(uint64(f.PathID)) + quicvarint.Len(f.SequenceNumber))
}
