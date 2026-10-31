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
	c := config{Scenario: "gridcast-loss", StartUnixNS: now.UnixNano(), MeasureSeconds: 600, LifetimeSeconds: 600}
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
