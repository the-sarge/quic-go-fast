package main

import (
	"net"
	"testing"
	"time"
)

func TestSharedMacAddressClassification(t *testing.T) {
	ip := net.ParseIP
	a, c, b := ip("10.204.1.2"), ip("10.204.1.3"), ip("192.168.1.110")
	for _, reverse := range []bool{false, true} {
		fs, fd, cs, cd := a, b, c, b
		if reverse {
			fs, fd, cs, cd = b, a, b, c
		}
		if isCompetitor(fs, fd, fs, fd, cs, cd) || !isCompetitor(cs, cd, fs, fd, cs, cd) {
			t.Fatal("shared Mac IP confused focal and competitor")
		}
	}
}
func TestBoundedLifetime(t *testing.T) {
	now := time.Now()
	c := config{Scenario: "gridcast-loss", StartUnixNS: 0, AttemptClock: "/owned/attempt.json", MeasureSeconds: 600, LifetimeSeconds: 600}
	if err := validateConfig(c, now); err != nil {
		t.Fatal(err)
	}
	c.LifetimeSeconds = 601
	if validateConfig(c, now) == nil {
		t.Fatal("accepted extended lifetime")
	}
	c.LifetimeSeconds = 600
	c.Scenario = "L6"
	if validateConfig(c, now) == nil {
		t.Fatal("accepted override for historical row")
	}
}

func TestSenderClockRequiresExactly600Seconds(t *testing.T) {
	c, err := parseAttemptClock([]byte(`{"start_monotonic_ns":1000,"deadline_monotonic_ns":600000001000,"start_unix_ns":2000,"deadline_unix_ns":600000002000}`))
	if err != nil || c.DeadlineMonotonicNS-c.StartMonotonicNS != int64(600*time.Second) {
		t.Fatal(c, err)
	}
	for _, data := range []string{`{}`, `{"start_monotonic_ns":1000,"deadline_monotonic_ns":601000001000,"start_unix_ns":2000,"deadline_unix_ns":600000002000}`} {
		if _, err := parseAttemptClock([]byte(data)); err == nil {
			t.Fatal("accepted a changed attempt allowance")
		}
	}
}

func TestCancellationAccountingPreservesLegacy(t *testing.T) {
	for _, gate := range []bool{false, true} {
		errors, discards := cancellationCounts(gate)
		if gate && (errors != 0 || discards != 1) || !gate && (errors != 1 || discards != 0) {
			t.Fatal(gate, errors, discards)
		}
	}
}
