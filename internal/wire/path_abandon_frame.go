// Adapted from qoke/mp-quic-go at 084c13ea3834fbe58cedb63db9208951d714f839.
// Copyright (c) 2016 the quic-go authors & Google, Inc. Licensed under the MIT license in ../../LICENSE.

package wire

import (
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/quicvarint"
)

// A PathAbandonFrame is a PATH_ABANDON frame of the multipath extension.
type PathAbandonFrame struct {
	PathID    protocol.PathID
	ErrorCode qerr.TransportErrorCode
}

func parsePathAbandonFrame(b []byte, _ protocol.Version) (*PathAbandonFrame, int, error) {
	startLen := len(b)
	pathID, l, err := parsePathID(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	b = b[l:]
	errorCode, l, err := quicvarint.Parse(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	b = b[l:]
	return &PathAbandonFrame{
		PathID:    pathID,
		ErrorCode: qerr.TransportErrorCode(errorCode),
	}, startLen - len(b), nil
}

func (f *PathAbandonFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	b = quicvarint.Append(b, uint64(FrameTypePathAbandon))
	b = quicvarint.Append(b, uint64(f.PathID))
	return quicvarint.Append(b, uint64(f.ErrorCode)), nil
}

// Length of a written frame
func (f *PathAbandonFrame) Length(_ protocol.Version) protocol.ByteCount {
	return protocol.ByteCount(2 + quicvarint.Len(uint64(f.PathID)) + quicvarint.Len(uint64(f.ErrorCode)))
}
