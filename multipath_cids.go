package quic

import (
	"errors"

	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/qerr"
	"github.com/quic-go/quic-go/internal/wire"
)

var errPathCIDCapacity = errors.New("local path CID retention capacity exhausted")

const maxPathCIDRecords = 16

type receivedPathCID struct {
	newConnID
	hasToken bool
	used     bool
	retired  bool
	expiry   monotime.Time
}

func (c receivedPathCID) detectsReset() bool { return c.hasToken && c.used && !c.retired }

type receivedPathCIDs struct {
	records     map[uint64]receivedPathCID
	active      uint64
	retirePrior uint64
	terminal    bool
}

func (h *connIDManager) enablePathCIDs() {
	initial := receivedPathCID{used: true, newConnID: newConnID{SequenceNumber: h.activeSequenceNumber, ConnectionID: h.activeConnectionID}}
	if h.activeStatelessResetToken != nil {
		initial.StatelessResetToken, initial.hasToken = *h.activeStatelessResetToken, true
	}
	set := &receivedPathCIDs{records: map[uint64]receivedPathCID{h.activeSequenceNumber: initial}, active: h.activeSequenceNumber, retirePrior: max(h.activeSequenceNumber, h.highestRetired, h.highestProbingID)}
	for _, entry := range h.queue {
		set.records[entry.SequenceNumber] = receivedPathCID{newConnID: entry, hasToken: true}
	}
	h.queue = nil
	h.activeStatelessResetToken = nil
	h.pathCIDs = map[protocol.PathID]*receivedPathCIDs{0: set}
}

func (h *connIDManager) addPathCID(f *wire.PathNewConnectionIDFrame, expiry monotime.Time) error {
	h.assertNotClosed()
	set := h.pathCIDs[f.PathID]
	if set == nil {
		set = &receivedPathCIDs{records: make(map[uint64]receivedPathCID)}
		h.pathCIDs[f.PathID] = set
	}
	if set.terminal {
		return nil
	}
	old, exists := set.records[f.SequenceNumber]
	if exists && (old.ConnectionID != f.ConnectionID || (old.hasToken && old.StatelessResetToken != f.StatelessResetToken)) {
		return &qerr.TransportError{ErrorCode: qerr.ProtocolViolation, ErrorMessage: "conflicting path connection ID"}
	}
	floor := max(set.retirePrior, f.RetirePriorTo)
	active := 0
	for seq := range set.records {
		if seq >= floor {
			active++
		}
	}
	if !exists && f.SequenceNumber >= floor {
		active++
	}
	if active > 4 {
		return &qerr.TransportError{ErrorCode: qerr.ConnectionIDLimitError}
	}
	if !exists && f.SequenceNumber >= floor && len(set.records) == maxPathCIDRecords {
		return errPathCIDCapacity
	}
	if !exists && f.SequenceNumber >= floor {
		for _, other := range h.pathCIDs {
			for _, entry := range other.records {
				if entry.ConnectionID == f.ConnectionID {
					return &qerr.TransportError{ErrorCode: qerr.ProtocolViolation, ErrorMessage: "connection ID reused for another path or sequence"}
				}
			}
		}
		set.records[f.SequenceNumber] = receivedPathCID{newConnID: newConnID{SequenceNumber: f.SequenceNumber, ConnectionID: f.ConnectionID, StatelessResetToken: f.StatelessResetToken}, hasToken: true}
	} else if exists && !old.hasToken {
		old.hasToken, old.StatelessResetToken = true, f.StatelessResetToken
		set.records[f.SequenceNumber] = old
		if old.detectsReset() {
			h.addStatelessResetToken(f.StatelessResetToken)
		}
	}
	for seq, entry := range set.records {
		if seq < floor && !entry.retired {
			if entry.detectsReset() {
				h.removeStatelessResetToken(entry.StatelessResetToken)
			}
			entry.retired, entry.expiry = true, expiry
			set.records[seq] = entry
			h.queueControlFrame(&wire.PathRetireConnectionIDFrame{PathID: f.PathID, SequenceNumber: seq})
		}
	}
	set.retirePrior = floor
	if f.SequenceNumber < floor {
		h.queueControlFrame(&wire.PathRetireConnectionIDFrame{PathID: f.PathID, SequenceNumber: f.SequenceNumber})
	}
	if entry, ok := set.records[set.active]; !ok || entry.retired {
		set.active = f.SequenceNumber
		for seq, entry := range set.records {
			if !entry.retired && seq < set.active {
				set.active = seq
			}
		}
	}
	return nil
}

func (h *connIDManager) releasePathCIDs(id protocol.PathID) {
	if set := h.pathCIDs[id]; set != nil {
		for _, entry := range set.records {
			if entry.detectsReset() {
				h.removeStatelessResetToken(entry.StatelessResetToken)
			}
		}
		delete(h.pathCIDs, id)
	}
}

func (h *connIDManager) expirePathCIDs(now monotime.Time) {
	for _, set := range h.pathCIDs {
		if set.terminal {
			continue
		}
		for seq, entry := range set.records {
			if entry.retired && !entry.expiry.After(now) {
				delete(set.records, seq)
			}
		}
	}
}

func (m *connIDGenerator) retirePathCID(f *wire.PathRetireConnectionIDFrame, dest protocol.ConnectionID, expiry monotime.Time) error {
	set := m.pathCIDs[f.PathID]
	if set == nil || f.SequenceNumber >= set.next {
		return &qerr.TransportError{ErrorCode: qerr.ProtocolViolation, ErrorMessage: "retired an unissued path connection ID"}
	}
	cid, ok := set.active[f.SequenceNumber]
	if !ok {
		return nil
	}
	if cid == dest {
		return &qerr.TransportError{ErrorCode: qerr.ProtocolViolation, ErrorMessage: "retired the packet destination connection ID"}
	}
	delete(set.active, f.SequenceNumber)
	set.retiring = append(set.retiring, connIDToRetire{t: expiry, connID: cid})
	if f.PathID == 0 && f.SequenceNumber == 0 {
		return nil
	}
	return m.issuePathCIDs(f.PathID, uint64(len(set.active))+1)
}

func (m *connIDGenerator) expirePathCIDs(now monotime.Time) {
	for _, set := range m.pathCIDs {
		if set.terminal {
			continue
		}
		retained := set.retiring[:0]
		for _, entry := range set.retiring {
			if entry.t.After(now) {
				retained = append(retained, entry)
			} else {
				m.connRunners.RemoveConnectionID(entry.connID)
			}
		}
		set.retiring = retained
	}
}

// extraPathCIDs excludes path-zero active entries already owned by the legacy
// map alias. Closing and destruction retain every additional routing obligation.
func (m *connIDGenerator) extraPathCIDs() []protocol.ConnectionID {
	var ids []protocol.ConnectionID
	for id, set := range m.pathCIDs {
		if id != 0 {
			for _, cid := range set.active {
				ids = append(ids, cid)
			}
		}
		for _, entry := range set.retiring {
			ids = append(ids, entry.connID)
		}
	}
	return ids
}
