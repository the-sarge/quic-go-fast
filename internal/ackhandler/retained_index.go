package ackhandler

import (
	"github.com/quic-go/quic-go/internal/monotime"
	"github.com/quic-go/quic-go/internal/protocol"
)

// Retained delivery records are indexed three ways: by key for removal, by
// ordinal for capacity eviction, by (space, packet number) for ACK discovery,
// and by expiry time. A record retired later with a shorter PTO can expire
// before an older one, so expiry cannot follow ordinal order.

func retainedBefore(a, b congestionPacketKey) bool {
	return a.space < b.space || (a.space == b.space && a.number < b.number)
}

func retainedHeight(n *retainedDelivery) int8 {
	if n == nil {
		return 0
	}
	return n.height
}

func retainedBalance(n *retainedDelivery) *retainedDelivery {
	n.height = 1 + max(retainedHeight(n.left), retainedHeight(n.right))
	switch d := retainedHeight(n.left) - retainedHeight(n.right); {
	case d > 1:
		if retainedHeight(n.left.left) < retainedHeight(n.left.right) {
			n.left = retainedRotateLeft(n.left)
		}
		return retainedRotateRight(n)
	case d < -1:
		if retainedHeight(n.right.right) < retainedHeight(n.right.left) {
			n.right = retainedRotateRight(n.right)
		}
		return retainedRotateLeft(n)
	}
	return n
}

func retainedRotateRight(n *retainedDelivery) *retainedDelivery {
	l := n.left
	n.left, l.right = l.right, n
	n.height = 1 + max(retainedHeight(n.left), retainedHeight(n.right))
	l.height = 1 + max(retainedHeight(l.left), retainedHeight(l.right))
	return l
}

func retainedRotateLeft(n *retainedDelivery) *retainedDelivery {
	r := n.right
	n.right, r.left = r.left, n
	n.height = 1 + max(retainedHeight(n.left), retainedHeight(n.right))
	r.height = 1 + max(retainedHeight(r.left), retainedHeight(r.right))
	return r
}

func retainedInsert(n, r *retainedDelivery) *retainedDelivery {
	if n == nil {
		r.left, r.right, r.height = nil, nil, 1
		return r
	}
	if retainedBefore(r.key, n.key) {
		n.left = retainedInsert(n.left, r)
	} else {
		n.right = retainedInsert(n.right, r)
	}
	return retainedBalance(n)
}

func retainedDelete(n *retainedDelivery, key congestionPacketKey) *retainedDelivery {
	if n == nil {
		return nil
	}
	countNodes(1)
	switch {
	case retainedBefore(key, n.key):
		n.left = retainedDelete(n.left, key)
	case retainedBefore(n.key, key):
		n.right = retainedDelete(n.right, key)
	default:
		if n.left == nil || n.right == nil {
			child := n.left
			if child == nil {
				child = n.right
			}
			n.left, n.right = nil, nil
			return child
		}
		right, m := retainedDeleteMin(n.right)
		m.left, m.right = n.left, right
		n.left, n.right = nil, nil
		n = m
	}
	return retainedBalance(n)
}

func retainedDeleteMin(n *retainedDelivery) (*retainedDelivery, *retainedDelivery) {
	countNodes(1)
	if n.left == nil {
		right := n.right
		n.right = nil
		return right, n
	}
	var m *retainedDelivery
	n.left, m = retainedDeleteMin(n.left)
	return retainedBalance(n), m
}

// retainedCovered appends, in key order, every record of space with a packet
// number in [lo, hi]. It visits O(height + returned) nodes.
func retainedCovered(n *retainedDelivery, space protocol.EncryptionLevel, lo, hi protocol.PacketNumber, out []*retainedDelivery) []*retainedDelivery {
	if n == nil {
		return out
	}
	countNodes(1)
	low := congestionPacketKey{space: space, number: lo}
	high := congestionPacketKey{space: space, number: hi}
	if retainedBefore(low, n.key) {
		out = retainedCovered(n.left, space, lo, hi, out)
	}
	if !retainedBefore(n.key, low) && !retainedBefore(high, n.key) {
		countInspected()
		out = append(out, n)
	} else {
		countFailed()
	}
	if retainedBefore(n.key, high) {
		out = retainedCovered(n.right, space, lo, hi, out)
	}
	return out
}

// retainedExpiryHeap orders records by expiry, then ordinal.
type retainedExpiryHeap []*retainedDelivery

func (q retainedExpiryHeap) less(i, j int) bool {
	a, b := q[i], q[j]
	return a.expires < b.expires || (a.expires == b.expires && a.packet.Ordinal < b.packet.Ordinal)
}

func (q retainedExpiryHeap) swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].expiryIndex = i
	q[j].expiryIndex = j
}

func (q *retainedExpiryHeap) push(r *retainedDelivery) {
	r.expiryIndex = len(*q)
	*q = append(*q, r)
	q.up(r.expiryIndex)
}

func (q *retainedExpiryHeap) remove(i int) {
	a := *q
	n := len(a) - 1
	if i != n {
		a.swap(i, n)
	}
	a[n] = nil
	*q = a[:n]
	if i != n && !q.down(i) {
		q.up(i)
	}
}

func (q retainedExpiryHeap) up(i int) {
	for i > 0 {
		countNodes(1)
		parent := (i - 1) / 2
		if !q.less(i, parent) {
			return
		}
		q.swap(i, parent)
		i = parent
	}
}

func (q retainedExpiryHeap) down(i int) bool {
	start := i
	for {
		l := 2*i + 1
		if l >= len(q) {
			break
		}
		countNodes(1)
		c := l
		if r := l + 1; r < len(q) && q.less(r, l) {
			c = r
		}
		if !q.less(c, i) {
			break
		}
		q.swap(i, c)
		i = c
	}
	return i > start
}

func (q retainedExpiryHeap) earliest() (monotime.Time, bool) {
	if len(q) == 0 {
		return 0, false
	}
	countNodes(1)
	return q[0].expires, true
}
