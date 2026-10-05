//go:build bbrworkcount

package ackhandler

import (
	"encoding/json"
	"os"
	"time"

	"github.com/quic-go/quic-go/internal/congestion"
)

// serviceWork is a disposable experiment counter. Inspected records are
// outcome or retained records read to decide a result; failed checks are the
// inspected records that did not qualify; nodes are index words, tree nodes
// and search steps traversed.
type serviceWork struct {
	Calls, Inspected, Failed, Nodes uint64
}

var (
	serviceCounters [serviceMechanisms]serviceWork
	activeService   serviceMechanism
)

// Demonstration overlay: per-call maxima and a periodic snapshot written from
// the connection goroutine that owns these counters. Not a maintained hook.
var (
	callStart       [serviceMechanisms]serviceWork
	maxPerCall      [serviceMechanisms]serviceWork
	workOutput      = os.Getenv("BBR_WORK_OUTPUT")
	workNextDump    time.Time
	workDumpChecks  uint64
	serviceNames    = [...]string{"none", "ack_transitions", "ack_witness", "pto_confirmation", "persistent_span", "retained_discovery", "retained_expiry"}
)

func beginServiceWork(m serviceMechanism) serviceMechanism {
	prior := activeService
	activeService = m
	serviceCounters[m].Calls++
	callStart[m] = serviceCounters[m]
	return prior
}

func endServiceWork(prior serviceMechanism) {
	m := activeService
	c, s := serviceCounters[m], callStart[m]
	x := &maxPerCall[m]
	x.Inspected = max(x.Inspected, c.Inspected-s.Inspected)
	x.Failed = max(x.Failed, c.Failed-s.Failed)
	x.Nodes = max(x.Nodes, c.Nodes-s.Nodes)
	activeService = prior
	if prior == serviceNone {
		maybeDumpWork()
	}
}
func countInspected() { serviceCounters[activeService].Inspected++ }
func countFailed()    { serviceCounters[activeService].Failed++ }

// Used only by the counted diagnostic patch, with its original definition.
func serviceCountersCorrectFailed() { serviceCounters[activeService].Failed-- }
func countNodes(n int) {
	serviceCounters[activeService].Nodes += uint64(n)
}

func resetServiceWork() {
	serviceCounters = [serviceMechanisms]serviceWork{}
	activeService = serviceNone
}

func maybeDumpWork() {
	if workOutput == "" {
		return
	}
	if workDumpChecks++; workDumpChecks%1024 != 0 {
		return
	}
	now := time.Now()
	if now.Before(workNextDump) {
		return
	}
	workNextDump = now.Add(250 * time.Millisecond)
	type entry struct {
		Total, MaxPerCall serviceWork
	}
	snapshot := struct {
		UnixNS     int64
		Mechanisms map[string]entry
		C4         map[string]uint64
		Phases     [][2]int64
	}{UnixNS: now.UnixNano(), Mechanisms: map[string]entry{}, C4: map[string]uint64{}}
	for m := serviceAckTransitions; m < serviceMechanisms; m++ {
		snapshot.Mechanisms[serviceNames[m]] = entry{serviceCounters[m], maxPerCall[m]}
	}
	snapshot.Phases = congestion.C4Phases
	for i, n := range congestion.C4WorkNames {
		snapshot.C4[n] = congestion.C4Work[i]
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	if os.WriteFile(workOutput+".tmp", data, 0o644) == nil {
		_ = os.Rename(workOutput+".tmp", workOutput)
	}
}
