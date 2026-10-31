package main

import (
	"fmt"
	"net"
	"time"
)

type side struct {
	Interface  string
	PeerMAC    string
	Focal      string
	Competitor string
}
type config struct {
	A, B            side
	Scenario        string
	StartUnixNS     int64
	MeasureSeconds  int
	LifetimeSeconds int // GridCast only: no historical export tail beyond this lifetime.
	Phase           int
	Seed            uint64
}

func validateConfig(c config, now time.Time) error {
	gate := c.Scenario == "gridcast-loss" || c.Scenario == "gridcast-coexistence"
	limit := 300
	if gate {
		limit = 600
	}
	if c.MeasureSeconds < 1 || c.MeasureSeconds > limit || time.Unix(0, c.StartUnixNS).Sub(now) > 2*time.Minute {
		return fmt.Errorf("invalid bounded configuration")
	}
	if gate {
		if c.LifetimeSeconds < 1 || c.LifetimeSeconds > 600 || c.Phase != 0 {
			return fmt.Errorf("GridCast requires lifetime 1..600 and phase zero")
		}
	} else if c.LifetimeSeconds != 0 {
		return fmt.Errorf("lifetime override is only for GridCast profiles")
	}
	return nil
}

// A shared Mac address cannot distinguish flows; the distinct Linux IP does.
func isCompetitor(src, dst, focalSrc, focalDst, competitorSrc, competitorDst net.IP) bool {
	return (competitorSrc != nil && !competitorSrc.Equal(focalSrc) && src.Equal(competitorSrc)) || (competitorDst != nil && !competitorDst.Equal(focalDst) && dst.Equal(competitorDst))
}
