package quic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDialOwnerOptIn(t *testing.T) {
	now := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, platform, until string
		enabled               bool
	}{
		{"TestDial/DialAddr", "darwin", now.Add(time.Hour).Format(time.RFC3339), true},
		{"TestDial/DialAddrEarly", "darwin", now.Add(7 * 24 * time.Hour).Format(time.RFC3339), true},
		{"TestDial/Dial", "darwin", now.Add(time.Hour).Format(time.RFC3339), false},
		{"TestDial/DialAddr", "linux", now.Add(time.Hour).Format(time.RFC3339), false},
		{"TestDial/DialAddr", "darwin", "", false},
		{"TestDial/DialAddr", "darwin", now.Format(time.RFC3339), false},
		{"TestDial/DialAddr", "darwin", now.Add(8 * 24 * time.Hour).Format(time.RFC3339), false},
	} {
		require.Equal(t, tc.enabled, dialOwnerEnabled(tc.name, tc.platform, tc.until, now), "%+v", tc)
	}
}

func TestDialOwnerParsing(t *testing.T) {
	data := "p101\x00cfirst\x00\n" +
		"f3\x00tIPv4\x00PUDP\x00n127.0.0.1:61982\x00\n" +
		"f4\x00tIPv4\x00PUDP\x00n127.0.0.1:11111->127.0.0.1:61982\x00\n" +
		"p202\x00csecond\x00\n" +
		"f9\x00tIPv6\x00PUDP\x00n*:61982\x00\n" +
		"f10\x00tIPv6\x00PUDP\x00n[::1]:61982->[::1]:12345\x00\n"
	owners, incomplete := parseDialPortOwners([]byte(data), 61982)
	require.False(t, incomplete)
	require.Equal(t, []dialPortOwner{
		{PID: 101, Command: "first", Descriptor: "3", Family: "IPv4", Local: "127.0.0.1:61982"},
		{PID: 202, Command: "second", Descriptor: "9", Family: "IPv6", Local: "*:61982"},
		{PID: 202, Command: "second", Descriptor: "10", Family: "IPv6", Local: "[::1]:61982", Remote: "[::1]:12345"},
	}, owners)
	for _, bad := range []string{
		"pbroken\x00cname\x00\nf3\x00tIPv4\x00PUDP\x00n*:61982\x00\n",
		"p1\x00cname\x00\nf3\x00tIPv4\x00PUDP\x00n*:61982",
		"p1\x00cname\x00\nf3\x00tIPv4\x00PUDP\x00n*:61982\x00",
		"p1\x00cname\x00\nf3\x00tIPv4\x00PTCP\x00n*:61982\x00\n",
	} {
		_, incomplete := parseDialPortOwners([]byte(bad), 61982)
		require.True(t, incomplete, "%q", bad)
	}
}

func TestDialOwnerBudget(t *testing.T) {
	var budget atomic.Bool
	entered := make(chan struct{})
	p := startDialOwnerProbe(12345, &budget, func(ctx context.Context, _ int) dialOwnerResult {
		close(entered)
		<-ctx.Done()
		return dialOwnerResult{TimedOut: ctx.Err() == context.DeadlineExceeded}
	}, dialOwnerTimeout)
	require.NotNil(t, p)
	<-entered // starting a probe must not wait for its completion
	require.Nil(t, startDialOwnerProbe(12345, &budget, nil, dialOwnerTimeout))
	require.Nil(t, p.finish(false)) // passing fixture cancels and drains the worker
}

func TestDialOwnerChild(t *testing.T) {
	switch os.Getenv("QUIC_GO_DIAL_OWNER_CHILD") {
	case "timeout":
		time.Sleep(time.Hour)
	case "overflow":
		os.Stdout.WriteString(strings.Repeat("x", 13<<10))
		os.Stderr.WriteString(strings.Repeat("w", 5<<10))
	case "denied":
		os.Stderr.WriteString("permission denied")
		os.Exit(1)
	case "delayed-copy":
		fmt.Fprintf(os.Stdout, "p%d\x00cchild\x00\nf3\x00tIPv4\x00PUDP\x00n127.0.0.1:61982\x00\n", os.Getpid())
	case "retained-pipe":
		// The descendant inherits stdout and outlives this child.
		binary, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		holder := exec.Command(binary, "-test.run=^TestDialOwnerChild$")
		holder.Env = append(os.Environ(), "QUIC_GO_DIAL_OWNER_CHILD=hold")
		holder.Stdout = os.Stdout
		if err := holder.Start(); err != nil {
			os.Exit(2)
		}
		fmt.Fprint(os.Stderr, holder.Process.Pid)
	case "hold":
		time.Sleep(10 * time.Minute)
	default:
		return
	}
	os.Exit(0)
}

// dialOwnerSlowWriter models an output-copy goroutine that is scheduled late.
type dialOwnerSlowWriter struct {
	io.Writer
	delay time.Duration
}

func (w dialOwnerSlowWriter) Write(b []byte) (int, error) {
	time.Sleep(w.delay)
	return w.Writer.Write(b)
}

func TestDialOwnerCommand(t *testing.T) {
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, mode := range []string{"timeout", "overflow", "denied", "delayed-copy", "retained-pipe"} {
		t.Run(mode, func(t *testing.T) {
			// Only the timeout case exercises the probe deadline. Ordinary child
			// startup can exceed it on a loaded or instrumented runner.
			timeout := scaleDuration(5 * time.Second)
			if mode == "timeout" {
				timeout = dialOwnerTimeout
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "-test.run=^TestDialOwnerChild$")
			cmd.Env = append(os.Environ(), "QUIC_GO_DIAL_OWNER_CHILD="+mode)
			var wrap func(io.Writer) io.Writer
			if mode == "delayed-copy" {
				// Hosted failures finished within 35-53 ms, past the former 10 ms drain.
				wrap = func(w io.Writer) io.Writer { return dialOwnerSlowWriter{Writer: w, delay: 50 * time.Millisecond} }
			}
			result := collectDialOwnerProbe(ctx, cmd, 61982, wrap)
			require.False(t, result.Ended.Before(result.Started))
			switch mode {
			case "timeout":
				require.Empty(t, result.Owners)
				require.True(t, result.TimedOut)
				require.NotEmpty(t, result.Error)
			case "overflow":
				require.Empty(t, result.Owners)
				require.False(t, result.TimedOut, "%+v", result)
				require.True(t, result.Truncated)
				require.True(t, result.ParseIncomplete)
				require.Len(t, result.Warnings, 4<<10)
			case "denied":
				require.Empty(t, result.Owners)
				require.False(t, result.TimedOut, "%+v", result)
				require.Equal(t, 1, result.ExitCode)
				require.Equal(t, "permission denied", result.Warnings)
			case "delayed-copy":
				// Successful exit with complete output, merely copied late.
				require.Empty(t, result.Error, "%+v", result)
				require.Zero(t, result.ExitCode)
				require.False(t, result.TimedOut)
				require.False(t, result.ParseIncomplete)
				require.Equal(t, []dialPortOwner{{
					PID: cmd.Process.Pid, Command: "child", Descriptor: "3", Family: "IPv4", Local: "127.0.0.1:61982",
				}}, result.Owners)
			case "retained-pipe":
				pid, err := strconv.Atoi(result.Warnings)
				require.NoError(t, err, "%+v", result)
				t.Cleanup(func() {
					if holder, err := os.FindProcess(pid); err == nil {
						holder.Kill()
						holder.Release()
					}
				})
				// The drain bound ends collection before the context deadline,
				// and long before the descendant releases stdout.
				require.Zero(t, result.ExitCode)
				require.False(t, result.TimedOut, "%+v", result)
				require.Equal(t, exec.ErrWaitDelay.Error(), result.Error)
				require.Empty(t, result.Owners)
			}
		})
	}
}

// One actual lsof invocation validates both the adapter and its capture wiring.
// This is controlled replacement occupancy, not a reproduction of #241.
func TestDialOwnerHeldPort(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS lsof adapter")
	}
	// An expired CI opt-in must disable natural probes without breaking this
	// independent held-port control.
	t.Setenv("QUIC_GO_DIAL_OWNER_UNTIL", "2000-01-01T00:00:00Z")
	capture := newDialCapture(t.Name())
	capture.ownerDeadline = time.Time{}
	capture.ownerEnabled = true
	capture.ownerBudget = new(atomic.Bool)
	capture.ownerTimeout = scaleDuration(dialOwnerTimeout)
	retained := captureAddrSocket(t, capture)
	original, err := listenUDPConn("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	require.Same(t, original, *retained)
	addr := original.LocalAddr().(*net.UDPAddr)
	require.NoError(t, original.Close())
	replacement, err := net.ListenUDP("udp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { replacement.Close() })
	// An open original observation must not trigger a probe.
	capture.observeSocket(replacement)
	conn, err := capture.rebind(addr)
	require.Error(t, err)
	require.Nil(t, conn)
	require.Nil(t, capture.ownerProbe)
	capture.observeSocket(original)
	conn, err = capture.rebind(addr)
	require.Error(t, err)
	require.Nil(t, conn)
	require.NotNil(t, capture.ownerProbe)
	require.Len(t, capture.ownerProbe.failures, 1)
	require.False(t, capture.ownerProbe.failures[0].After(capture.ownerProbe.trigger), "triggering bind must predate observer admission")
	report := capture.finish(true)
	var summary struct {
		Events []struct {
			Event string
			Data  struct {
				Descriptor *uintptr `json:"descriptor"`
			}
		} `json:"events"`
		Complete bool `json:"complete_observations"`
		Owner    struct {
			Probe  dialOwnerResult `json:"probe"`
			During int             `json:"bind_failures_during_query"`
		} `json:"port_owner_observation"`
	}
	require.NoError(t, json.Unmarshal(report, &summary))
	require.True(t, summary.Complete)
	var descriptor *uintptr
	for _, event := range summary.Events {
		if event.Event == "original_socket_created" {
			descriptor = event.Data.Descriptor
		}
	}
	require.NotNil(t, descriptor, string(report))
	require.Zero(t, summary.Owner.During, "no later bind occurred during the query")
	probe := summary.Owner.Probe
	require.False(t, probe.TimedOut, string(report))
	require.False(t, probe.Truncated, string(report))
	require.False(t, probe.ParseIncomplete, string(report))
	require.Empty(t, probe.Error, string(report))
	var found bool
	for _, owner := range probe.Owners {
		if owner.PID == os.Getpid() && owner.Local == addr.String() {
			found = true
		}
	}
	require.True(t, found, string(report))
}

func TestDialOwnerOverlap(t *testing.T) {
	start := time.Now()
	done := make(chan dialOwnerResult, 1)
	done <- dialOwnerResult{Started: start, Ended: start.Add(time.Millisecond)}
	p := &dialOwnerProbe{
		cancel: func() {}, result: done,
		failures: []time.Time{start.Add(-time.Nanosecond), start.Add(time.Microsecond), start.Add(time.Second)},
	}
	require.Equal(t, 1, p.finish(true)["bind_failures_during_query"])
}
