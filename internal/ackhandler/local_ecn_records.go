package ackhandler

// localECNRecords retains the same ordered range sequence in a circular deque.
// Edits move the shorter side: sequential completion coalesces the first pending
// singleton into its settled predecessor without copying the pending suffix.
// Pointers from at must not survive a splice, just as pointers into the former
// slice could not survive growth. Storage moves change no semantic holder.
type localECNRecords struct {
	data        []localECNRecord
	head, count int
}

func (r *localECNRecords) len() int { return r.count }
func (r *localECNRecords) index(i int) int {
	p := r.head + i
	if p >= len(r.data) {
		p -= len(r.data)
	}
	return p
}

func (r *localECNRecords) at(i int) *localECNRecord {
	if i < 0 || i >= r.count {
		panic("invalid local ECN record index")
	}
	return &r.data[r.index(i)]
}

// coalesceAt retains equivalent settled facts in whichever end record permits
// the shorter deletion. No record pointer survives the single storage edit.
func (r *localECNRecords) coalesceAt(i int) {
	if r.count == 0 {
		return
	}
	current := r.at(i)
	first, last := i, i
	left, right := current, current
	if i > 0 {
		previous := r.at(i - 1)
		if extendsLocalECNRange(previous, current) {
			first, left = i-1, previous
		}
	}
	if i+1 < r.count {
		next := r.at(i + 1)
		if extendsLocalECNRange(current, next) {
			last, right = i+1, next
		}
	}
	if first == last {
		return
	}
	if first < r.count-last-1 {
		right.pn = left.pn
		r.splice(first, last, nil)
	} else {
		left.last = right.last
		r.splice(first+1, last+1, nil)
	}
}

// append changes only the unused tail when backing capacity is available.
// Growth retains the general splice path and the existing logical ceiling.
func (r *localECNRecords) append(value localECNRecord) {
	if r.count == len(r.data) {
		r.splice(r.count, r.count, []localECNRecord{value})
		return
	}
	r.data[r.index(r.count)] = value
	r.count++
}

func (r *localECNRecords) splice(start, end int, values []localECNRecord) {
	old := r.count
	n := old - (end - start) + len(values)
	if start < 0 || end < start || end > old || n > maxECNMarkRanges {
		panic("invalid local ECN record splice")
	}
	if n > len(r.data) {
		size := min(maxECNMarkRanges, max(8, max(n, 2*len(r.data))))
		data := make([]localECNRecord, size)
		for i := range start {
			data[i] = *r.at(i)
		}
		copy(data[start:], values)
		for i := end; i < old; i++ {
			data[start+len(values)+i-end] = *r.at(i)
		}
		r.data, r.head, r.count = data, 0, n
		return
	}
	delta := n - old
	if delta < 0 {
		removed := -delta
		if start < old-end {
			for i := start - 1; i >= 0; i-- {
				r.data[r.index(i+removed)] = *r.at(i)
			}
			for i := range removed {
				r.data[r.index(i)] = localECNRecord{}
			}
			r.head = r.index(removed)
		} else {
			for i := end; i < old; i++ {
				r.data[r.index(i-removed)] = *r.at(i)
			}
			for i := n; i < old; i++ {
				r.data[r.index(i)] = localECNRecord{}
			}
		}
	} else if delta > 0 {
		if start < old-end {
			nextHead := r.head - delta
			if nextHead < 0 {
				nextHead += len(r.data)
			}
			for i := range start {
				target := nextHead + i
				if target >= len(r.data) {
					target -= len(r.data)
				}
				r.data[target] = *r.at(i)
			}
			r.head = nextHead
		} else {
			for i := old - 1; i >= end; i-- {
				r.data[r.index(i+delta)] = *r.at(i)
			}
		}
	}
	r.count = n
	for i, value := range values {
		r.data[r.index(start+i)] = value
	}
	if n == 0 {
		r.head = 0
	}
}
