// Adapted from qoke/mp-quic-go at 084c13ea3834fbe58cedb63db9208951d714f839.
// Copyright (c) 2016 the quic-go authors & Google, Inc. Licensed under the MIT license in ../../LICENSE.

package wire

import (
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/quicvarint"
)

// A PathsBlockedFrame is a PATHS_BLOCKED frame of the multipath extension.
type PathsBlockedFrame struct {
	MaximumPathID protocol.PathID
}

func parsePathsBlockedFrame(b []byte, _ protocol.Version) (*PathsBlockedFrame, int, error) {
	maxPathID, l, err := parsePathID(b)
	if err != nil {
		return nil, 0, replaceUnexpectedEOF(err)
	}
	return &PathsBlockedFrame{MaximumPathID: maxPathID}, l, nil
}

func (f *PathsBlockedFrame) Append(b []byte, _ protocol.Version) ([]byte, error) {
	b = quicvarint.Append(b, uint64(FrameTypePathsBlocked))
	return quicvarint.Append(b, uint64(f.MaximumPathID)), nil
}

// Length of a written frame
func (f *PathsBlockedFrame) Length(protocol.Version) protocol.ByteCount {
	return protocol.ByteCount(2 + quicvarint.Len(uint64(f.MaximumPathID)))
}
