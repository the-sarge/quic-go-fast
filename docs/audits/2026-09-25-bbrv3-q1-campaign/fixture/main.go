// Q1-only native workload fixture. Freeze its source with the campaign manifest;
// this is not an installed or maintained benchmark service.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"runtime"
	"time"
)

var sourceRevision = "unrecorded"

type resourceSample struct {
	UnixNS     int64   `json:"unix_ns"`
	CPUSeconds float64 `json:"cpu_seconds"`
	HeapBytes  uint64  `json:"heap_bytes"`
}

type resources struct {
	CPUSeconds      float64 `json:"cpu_seconds"`
	PeakRSSBytes    uint64  `json:"peak_rss_bytes"`
	TotalAllocBytes uint64  `json:"total_alloc_bytes"`
	HeapBytesAtExit uint64  `json:"heap_bytes_at_exit"`
	Error           string  `json:"error,omitempty"`
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func execute() error {
	role := flag.String("role", "", "cert, receive or send")
	config := flag.String("config", "", "frozen run JSON")
	local := flag.String("local", "", "explicit IPv4 bind address:port")
	peer := flag.String("peer", "", "receiver IPv4 address:port")
	cert := flag.String("cert", "campaign.pem", "campaign mTLS certificate")
	key := flag.String("key", "campaign-key.pem", "campaign mTLS private key")
	output := flag.String("output", "", "result JSON path")
	managed := flag.Bool("managed", true, "direct managed packet-I/O lease")
	flag.Parse()
	if *role == "cert" {
		c, k, e := createCertificate(".")
		if e == nil {
			fmt.Println(c, k)
		}
		return e
	}
	if *role != "receive" && *role != "send" {
		return fmt.Errorf("role must be cert, receive or send")
	}
	if *local == "" || *output == "" {
		return fmt.Errorf("local bind address and output required")
	}
	b, e := os.ReadFile(*config)
	if e != nil {
		return e
	}
	var cfg Run
	if e = json.Unmarshal(b, &cfg); e != nil {
		return e
	}
	if e = cfg.validate(); e != nil {
		return e
	}
	if time.Until(cfg.start()) < 0 || time.Until(cfg.start()) > 2*time.Minute {
		return fmt.Errorf("start must be in the next two minutes")
	}
	runtime.GOMAXPROCS(4)
	tr, closeTransport, e := newTransport(*local, *managed)
	if e != nil {
		return e
	}
	tlsConfig, e := loadTLS(*cert, *key, *role == "receive")
	if e != nil {
		closeTransport()
		return e
	}
	traces := newTrace()
	qcfg := quicConfig(cfg, traces)
	ctx, cancel := context.WithDeadline(context.Background(), cfg.end().Add(15*time.Second))
	defer cancel()
	started := time.Now()
	before := usage()
	var initial runtime.MemStats
	runtime.ReadMemStats(&initial)
	sampleDone := make(chan []resourceSample, 1)
	stopSamples := make(chan struct{})
	go func() {
		var samples []resourceSample
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopSamples:
				sampleDone <- samples
				return
			case <-ticker.C:
				u := usage()
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				samples = append(samples, resourceSample{time.Now().UnixNano(), u.CPUSeconds - before.CPUSeconds, mem.HeapAlloc})
			}
		}
	}()
	samplesStopped := false
	defer func() {
		if !samplesStopped {
			close(stopSamples)
			<-sampleDone
		}
	}()
	var connectionSetupNS int64
	var result Result
	if *role == "receive" {
		ln, err := tr.Listen(tlsConfig, qcfg)
		if err != nil {
			closeTransport()
			return err
		}
		fmt.Fprintln(os.Stderr, "ready", ln.Addr())
		conn, err := ln.Accept(ctx)
		if err != nil {
			ln.Close()
			closeTransport()
			return err
		}
		connectionSetupNS = time.Since(started).Nanoseconds()
		result, e = receiveSession(ctx, conn, cfg)
		ln.Close()
	} else {
		addr, err := net.ResolveUDPAddr("udp4", *peer)
		if err != nil {
			closeTransport()
			return err
		}
		conn, err := tr.Dial(ctx, addr, tlsConfig, qcfg)
		if err != nil {
			closeTransport()
			return err
		}
		connectionSetupNS = time.Since(started).Nanoseconds()
		result, e = sendSession(ctx, conn, cfg)
		conn.CloseWithError(0, "campaign complete")
	}
	closeTransport()
	close(stopSamples)
	samplesStopped = true
	resourceSamples := <-sampleDone
	after := usage()
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	after.CPUSeconds -= before.CPUSeconds
	after.TotalAllocBytes = mem.TotalAlloc - initial.TotalAlloc
	after.HeapBytesAtExit = mem.HeapAlloc
	record := struct {
		ConnectionSetupNS int64            `json:"connection_setup_ns"`
		ResourceSamples   []resourceSample `json:"resource_samples"`
		Result            Result           `json:"result"`
		Trace             *trace           `json:"trace"`
		Resources         resources        `json:"resources"`
		Source            string           `json:"source"`
		GoVersion         string           `json:"go_version"`
		Platform          string           `json:"platform"`
		GOMAXPROCS        int              `json:"gomaxprocs"`
		Managed           bool             `json:"managed_direct"`
		ElapsedNS         int64            `json:"elapsed_ns"`
		Error             string           `json:"error,omitempty"`
	}{ConnectionSetupNS: connectionSetupNS, ResourceSamples: resourceSamples, Result: result, Trace: traces, Resources: after, Source: sourceRevision, GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, GOMAXPROCS: runtime.GOMAXPROCS(0), Managed: *managed, ElapsedNS: time.Since(started).Nanoseconds()}
	if e != nil {
		record.Error = e.Error()
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(*output, append(data, '\n'), 0600); err != nil {
		return err
	}
	if e != nil {
		return e
	}
	if len(result.Errors) > 0 || result.Receiver.Corrupt > 0 {
		return fmt.Errorf("receiver integrity/session failure; retain result")
	}
	return nil
}
