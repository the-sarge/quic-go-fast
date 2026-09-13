package quic

// coalescedDelivery owns only the undelivered views of one coalesced read.
// Its mutation is confined to the reader and post-read cleanup; returned
// views can release concurrently through the slab's atomic reference count.
type coalescedDelivery struct {
	pending []receivedPacket
}

func (d *coalescedDelivery) next() (receivedPacket, bool) {
	if len(d.pending) == 0 {
		return receivedPacket{}, false
	}
	p := d.pending[0]
	d.pending[0] = receivedPacket{}
	d.pending = d.pending[1:]
	return p, true
}

// accept consumes a nonempty read in coalesced-tier storage and returns its
// first view. Even a singleton must be slab-backed so retention queues copy
// it out or charge the retained-bytes budget. The already-filled buffer is
// adopted directly: allocating another backing store here would add a copy.
func (d *coalescedDelivery) accept(p receivedPacket, segmentSize int) receivedPacket {
	if len(d.pending) != 0 {
		panic("coalescedDelivery.accept: pending views")
	}
	if len(p.data) == 0 {
		panic("coalescedDelivery.accept: empty read")
	}
	p.buffer.Data = p.buffer.Data[:len(p.data)]
	slab := &coalescedSlab{buf: p.buffer}
	if segmentSize <= 0 {
		segmentSize = len(p.data)
	}
	// split establishes the full sibling reference count before publication.
	views := slab.split(segmentSize)
	p.data = views[0].Data
	p.buffer = views[0]
	for _, view := range views[1:] {
		sibling := p
		sibling.data = view.Data
		sibling.buffer = view
		d.pending = append(d.pending, sibling)
	}
	return p
}

// discard releases pending siblings without touching any returned view.
func (d *coalescedDelivery) discard() {
	for i, p := range d.pending {
		p.buffer.Release()
		d.pending[i] = receivedPacket{}
	}
	d.pending = nil
}
