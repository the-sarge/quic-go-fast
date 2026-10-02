#!/bin/bash
# Each mutation must make the differential equivalence suite, the slot-set
# search check or the work-bound scaling check fail. Run from internal/ackhandler
# of the service-layer worktree: mutations.sh <dir>.
set -u
cd "$1"
run() { # file, from, to, label
  cp "$1" "$1.orig"
  python3 -c "
import sys;p,a,b=sys.argv[1:4];s=open(p).read();assert s.count(a)==1,a;open(p,'w').write(s.replace(a,b))" "$1" "$2" "$3"
  if ! go test -count=1 -run 'TestRecoveryEquivalence|TestSlotSet' ./ >/dev/null 2>&1; then
    echo "killed (equivalence):  $4"
  elif ! go test -count=1 -tags bbrworkcount -run TestRecoveryServiceWorkScaling ./ >/dev/null 2>&1; then
    echo "killed (work bound):   $4"
  else
    echo "SURVIVED:              $4"
  fi
  mv "$1.orig" "$1"
}
R=bbr_recovery.go; I=retained_index.go; S=delivery_sampler.go; X=recovery_index.go
run $R "		if o.number >= witness || o.sent.After(cutoff) {" "		if o.number >= witness {" "PTO confirmation ignores the send-time cutoff"
run $R "			countFailed()
			break" "			countFailed()
			continue" "PTO confirmation keeps scanning (bound only)"
run $R "	if l := r.rightmostRunIn(0, r.head-1, threshold); l >= 0 {" "	if l := r.rightmostRunIn(r.head, recoverySlotMask, threshold); l >= 0 {" "span query searches logical halves in the wrong order"
run $R "	if end.ordinal <= r.reported {" "	if end.ordinal < r.reported {" "report deduplication off by one"
run $R "		if _, _, d := r.runFrom(l); d > threshold {" "		if _, _, d := r.runFrom(l); d >= threshold {" "strict 3*PTO test made inclusive"
run $R "	sp.endpoints.assign(p, lost && r.index.outcomes[p].endpoint)" "	sp.endpoints.assign(p, lost)" "non-endpoints counted as endpoints"
run $R "	if l > 0 && sp.lost.has(r.slot(l-1)) {" "	if false && l > 0 && sp.lost.has(r.slot(l-1)) {" "left run's block not refreshed"
run $R "	return r.scanBlock(lo, bl*spanBlockSlots+spanBlockSlots-1, threshold)" "	return -1" "left partial block skipped"
run $R "			witness = max(witness, x.outcomes[r.slot(l)].number)" "			witness = max(witness, x.outcomes[r.slot(l)].number-1)" "witness off by one"
run $R "	x.eligible[s].assign(p, o.receiptEligible && state != outcomeDisposed)" "	x.eligible[s].assign(p, o.receiptEligible)" "disposed outcomes stay witnesses"
run $R "	if o.state == outcomeLost {
		r.setLost(p, false)
	}" "" "eviction leaves a lost run in the index"
run $R "		i, j := r.lowerBound(q, rng.Smallest), r.lowerBound(q, rng.Largest+1)" "		i, j := r.lowerBound(q, rng.Smallest), r.lowerBound(q, rng.Largest)" "ACK range upper bound excludes Largest"
run $R "		if o := &r.index.outcomes[p]; o.level == level {" "		if o := &r.index.outcomes[p]; true {" "space disposal ignores level"
run $S "	d.sampler.expiry.remove(r.expiryIndex)
" "" "removal leaves an expiry-heap entry"
run $S "			countFailed()
			next = expires
			break" "			countFailed()
			break" "expiry loses the next deadline"
run $I "	if retainedBefore(n.key, high) {" "	if false && retainedBefore(n.key, high) {" "discovery skips right subtrees"
run $I "	return a.expires < b.expires || (a.expires == b.expires && a.packet.Ordinal < b.packet.Ordinal)" "	return a.packet.Ordinal < b.packet.Ordinal" "expiry ordered by ordinal"
run $X "		return ^s.full2 & (1<<recoverySummaryBits - 1)" "		return ^s.full2" "clear search escapes the summary width"
run $R "	for l := r.nextL(&x.pending[s], a, b, false); l >= 0; l = r.nextL(&x.pending[s], l+1, b, false) {" "	for l := a; l <= b; l++ {
		if !x.pending[s].has(r.slot(l)) {
			countInspected()
			countFailed()
			continue
		}" "ACK walks every covered registration (bound only)"
run $S "		if expires.After(now) {
			countFailed()
			next = expires
			break
		}" "		if expires.After(now) {
			countFailed()
			next = expires
			for _, r := range d.sampler.retained {
				countInspected()
				_ = r
			}
			break
		}" "expiry walks every retained record (bound only)"
