package ackhandler

import "testing"

// reducedEquivalenceBudget reports whether the randomized frozen-equivalence
// oracles use their reduced (-short) budget. They are single-goroutine
// comparisons that the race detector slows by more than an order of magnitude
// without adding coverage, so race runs use the reduced budget as well;
// ordinary runs keep the full budget.
func reducedEquivalenceBudget() bool { return testing.Short() || raceEnabled }
