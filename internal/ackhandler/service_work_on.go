//go:build bbrworkcount

package ackhandler

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

func beginServiceWork(m serviceMechanism) serviceMechanism {
	prior := activeService
	activeService = m
	serviceCounters[m].Calls++
	return prior
}

func endServiceWork(prior serviceMechanism) { activeService = prior }
func countInspected()                       { serviceCounters[activeService].Inspected++ }
func countFailed()                          { serviceCounters[activeService].Failed++ }
func countNodes(n int)                      { serviceCounters[activeService].Nodes += uint64(n) }

func resetServiceWork() {
	serviceCounters = [serviceMechanisms]serviceWork{}
	activeService = serviceNone
}
