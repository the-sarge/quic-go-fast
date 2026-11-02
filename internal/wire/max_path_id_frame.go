// Adapted from qoke/mp-quic-go at 084c13ea3834fbe58cedb63db9208951d714f839.
// Copyright (c) 2016 the quic-go authors & Google, Inc. Licensed under the MIT license in ../../LICENSE.

package wire

import (
	"errors"

	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/quicvarint"
)

var errInvalidPathID = errors.New("path ID exceeds 2^32-1")

// parsePathID validates the wire bound before narrowing to the internal identity.
func parsePathID(b []byte) (protocol.PathID, int, error) {
	id, n, err := quicvarint.Parse(b)
	if err != nil {
		return 0, 0, replaceUnexpectedEOF(err)
	}
	if id > uint64(protocol.MaxPathID) {
		return 0, 0, errInvalidPathID
	}
	return protocol.PathID(id), n, nil
}

// MaxPathIDFrame is a draft-21 MAX_PATH_ID frame.
type MaxPathIDFrame struct {
	MaximumPathID protocol.PathID
}

func parseMaxPathIDFrame(b []byte, _ protocol.Version) (*MaxPathIDFrame, int, error) {
	id, n, err := parsePathID(b)
	if err != nil {
		return nil, 0, err
	}
	return &MaxPathIDFrame{MaximumPathID: id}, n, nil
}

func (f *MaxPathIDFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	b = quicvarint.Append(b, uint64(FrameTypeMaxPathID))
	return quicvarint.Append(b, uint64(f.MaximumPathID)), nil
}

func (f *MaxPathIDFrame) Length(protocol.Version) protocol.ByteCount {
	return protocol.ByteCount(2 + quicvarint.Len(uint64(f.MaximumPathID)))
}
