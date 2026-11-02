// Adapted from qoke/mp-quic-go at 084c13ea3834fbe58cedb63db9208951d714f839.
// Copyright (c) 2016 the quic-go authors & Google, Inc. Licensed under the MIT license in ../../LICENSE.

package wire

import (
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/quicvarint"
)

// A PathStatusFrame is a PATH_STATUS_BACKUP or a PATH_STATUS_AVAILABLE frame of the multipath extension.
type PathStatusFrame struct {
	PathID         protocol.PathID
	SequenceNumber uint64
	// Backup is set for PATH_STATUS_BACKUP frames, and unset for PATH_STATUS_AVAILABLE frames.
	Backup bool
}

func parsePathStatusFrame(b []byte, typ FrameType, _ protocol.Version) (*PathStatusFrame, int, error) {
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
	return &PathStatusFrame{
		PathID:         pathID,
		SequenceNumber: seq,
		Backup:         typ == FrameTypePathStatusBackup,
	}, startLen - len(b), nil
}

func (f *PathStatusFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	if f.Backup {
		b = quicvarint.Append(b, uint64(FrameTypePathStatusBackup))
	} else {
		b = quicvarint.Append(b, uint64(FrameTypePathStatusAvailable))
	}
	b = quicvarint.Append(b, uint64(f.PathID))
	return quicvarint.Append(b, f.SequenceNumber), nil
}

// Length of a written frame
func (f *PathStatusFrame) Length(_ protocol.Version) protocol.ByteCount {
	return protocol.ByteCount(2 + quicvarint.Len(uint64(f.PathID)) + quicvarint.Len(f.SequenceNumber))
}
