// Campaign-only Stage A instrumentation. No buffer ownership or retry policy lives here.
package main

import (
	"fmt"
	"net"
	"runtime"
	"sync"
)

const candidateRevision = "23c83b9896980511286ca8b81cd632f7ec56afed"
const referenceRevision = "8095c72244755f0c1e24f4158253bf09cfbe6e17"

var overlayDigest = "unrecorded"

type batchWriter interface {
	WriteBatchV1([][]byte, []byte, *net.UDPAddr) (int, error)
}

type batchSnapshot struct {
	Calls         uint64 `json:"calls"`
	MultiOffers   uint64 `json:"multi_offers"`
	MultiAccepted uint64 `json:"multi_accepted"`
	Offered       uint64 `json:"offered_packets"`
	Accepted      uint64 `json:"accepted_packets"`
	LargestOffer  uint64 `json:"largest_offer"`
	ShortPrefixes uint64 `json:"short_prefixes"`
	Errors        uint64 `json:"errors"`
	InvalidCounts uint64 `json:"invalid_counts"`
	// Fixed order: Not-ECT, ECT(1), ECT(0), CE, unknown. Counts are datagrams.
	MultiOfferedECN  [5]uint64 `json:"multi_offered_ecn"`
	MultiAcceptedECN [5]uint64 `json:"multi_accepted_ecn"`
}

type batchCounters struct {
	mu     sync.Mutex
	counts batchSnapshot
}

func (c *batchCounters) forward(w batchWriter) func([][]byte, []byte, *net.UDPAddr) (int, error) {
	return func(bufs [][]byte, oob []byte, addr *net.UDPAddr) (int, error) {
		// Inspect borrowed metadata before forwarding; retain neither buffers nor destination.
		mark := ecnBucket(oob)
		n, err := w.WriteBatchV1(bufs, oob, addr)
		c.mu.Lock()
		c.counts.Calls++
		offered := uint64(len(bufs))
		c.counts.Offered += offered
		c.counts.LargestOffer = max(c.counts.LargestOffer, offered)
		if err != nil {
			c.counts.Errors++
		}
		valid := n >= 0 && n <= len(bufs)
		if valid {
			c.counts.Accepted += uint64(n)
			if n < len(bufs) {
				c.counts.ShortPrefixes++
			}
		} else {
			c.counts.InvalidCounts++
		}
		if len(bufs) > 1 {
			c.counts.MultiOffers++
			c.counts.MultiOfferedECN[mark] += offered
			if valid {
				c.counts.MultiAcceptedECN[mark] += uint64(n)
				if n > 1 {
					c.counts.MultiAccepted++
				}
			}
		}
		c.mu.Unlock()
		return n, err
	}
}

func (c *batchCounters) snapshot() batchSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts
}

type overlayMetadata struct {
	Schema           string `json:"schema"`
	OverlaySHA256    string `json:"overlay_sha256"`
	LibraryRevision  string `json:"library_revision"`
	FixtureRevision  string `json:"fixture_revision"`
	Mode             string `json:"mode"`
	Backend          string `json:"backend"`
	BackendEvidence  string `json:"backend_evidence"`
	Kernel           string `json:"kernel"`
	PMTU             bool   `json:"pmtu"`
	InitialUDPSize   int    `json:"initial_udp_size"`
	ACKMode          string `json:"ack_mode"`
	GSO              bool   `json:"gso"`
	NativeCapability string `json:"native_capability"`
}

func metadata() overlayMetadata {
	return overlayMetadata{
		Schema: "q2-stage-a-overlay-v1", OverlaySHA256: overlayDigest,
		LibraryRevision: sourceRevision, FixtureRevision: candidateRevision,
		Mode: "managed-ipv4-counted-callback", Backend: "ordinary-per-datagram-write",
		BackendEvidence: "pinned Linux source; grouping is not syscall batching",
		Kernel:          kernelRelease(), PMTU: false, InitialUDPSize: 1400, ACKMode: "ordinary-unset", GSO: false,
		NativeCapability: "separate per-lease receipt required; no ECN/offload qualification claim",
	}
}

func validateStageA(r Run, managed, tcp bool) error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" || !managed || tcp {
		return fmt.Errorf("Stage A requires Linux/amd64 managed IPv4 without competitors")
	}
	if sourceRevision != candidateRevision && sourceRevision != referenceRevision {
		return fmt.Errorf("unregistered library revision")
	}
	if len(overlayDigest) != 64 {
		return fmt.Errorf("unregistered overlay digest")
	}
	if r.WarmupMS != 30000 || r.MeasureMS != 180000 || r.CompletionBytes != 0 || r.BulkPauseStartMS != 0 || r.BulkPauseEndMS != 0 {
		return fmt.Errorf("not a Stage A time window")
	}
	arm := "C-Reno"
	if r.Controller == "bbrv3" {
		arm = "C-BBR"
	}
	if sourceRevision == referenceRevision {
		if r.Controller != "reno" {
			return fmt.Errorf("reference requires Reno")
		}
		arm = "R-Reno"
	}
	id := "M-LAN-linux-amd64-" + arm + "-" + r.Workload + "-r1-managed-s635599-p0"
	payload := 16384
	if r.Workload == "datagram" {
		payload = 1200
	}
	if r.ID != id || r.PayloadBytes != payload {
		return fmt.Errorf("unregistered Stage A observation")
	}
	return r.validate()
}
