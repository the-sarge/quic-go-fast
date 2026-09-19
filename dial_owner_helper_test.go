package quic

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const dialOwnerTimeout = 250 * time.Millisecond

var dialOwnerClaimed atomic.Bool

// A fixed expiration makes an abandoned opt-in harmless. CI supplies a
// repository variable; local runs can supply the same RFC3339 timestamp.
func dialOwnerEnabled(name, platform, until string, now time.Time) bool {
	if platform != "darwin" || (name != "TestDial/DialAddr" && name != "TestDial/DialAddrEarly") {
		return false
	}
	end, err := time.Parse(time.RFC3339, until)
	return err == nil && end.After(now) && !end.After(now.Add(7*24*time.Hour))
}

type dialPortOwner struct {
	PID        int    `json:"pid"`
	Command    string `json:"command"`
	Descriptor string `json:"descriptor"`
	Family     string `json:"family"`
	Local      string `json:"local"`
	Remote     string `json:"remote,omitempty"`
}

type dialOwnerResult struct {
	Started         time.Time       `json:"started"`
	Ended           time.Time       `json:"ended"`
	ExitCode        int             `json:"exit_code"`
	Error           string          `json:"error,omitempty"`
	TimedOut        bool            `json:"timed_out"`
	Truncated       bool            `json:"truncated"`
	ParseIncomplete bool            `json:"parse_incomplete"`
	Warnings        string          `json:"warnings,omitempty"`
	Owners          []dialPortOwner `json:"owners"`
}

type dialOwnerProbe struct {
	trigger           time.Time
	port              int
	cancel            context.CancelFunc
	result            <-chan dialOwnerResult
	failures          []time.Time
	failuresTruncated bool
}

// Called with the recorder lock held. Tests use a separate budget so their
// controlled observations never consume the real fixture's process allowance.
func startDialOwnerProbe(port int, budget *atomic.Bool, run func(context.Context, int) dialOwnerResult) *dialOwnerProbe {
	if !budget.CompareAndSwap(false, true) {
		return nil
	}
	now := time.Now()
	ctx, cancel := context.WithDeadline(context.Background(), now.Add(dialOwnerTimeout))
	done := make(chan dialOwnerResult, 1)
	go func() {
		defer cancel()
		done <- run(ctx, port)
	}()
	return &dialOwnerProbe{trigger: now, port: port, cancel: cancel, result: done}
}

func (p *dialOwnerProbe) finish(failed bool) map[string]any {
	if !failed {
		p.cancel()
	}
	result := <-p.result
	p.cancel()
	if !failed {
		return nil
	}
	overlap := 0
	for _, at := range p.failures {
		if !at.Before(result.Started) && !at.After(result.Ended) {
			overlap++
		}
	}
	return map[string]any{
		"trigger": p.trigger, "port": p.port, "pid": os.Getpid(), "parent_pid": os.Getppid(),
		"probe": result, "bind_failures_during_query": overlap, "bind_times_truncated": p.failuresTruncated,
		"scope":          "visible process descriptors only; not an atomic kernel ownership snapshot",
		"interpretation": "no match, late output, access restrictions or incomplete output are inconclusive; matching descriptor numbers can be reused",
	}
}

// Both writers keep draining after their quota, avoiding a pipe deadlock.
// stdout and stderr have separate writers, each used by one exec copy worker.
type dialOwnerOutput struct {
	data      []byte
	limit     int
	truncated bool
}

func (w *dialOwnerOutput) Write(b []byte) (int, error) {
	keep := min(len(b), w.limit-len(w.data))
	w.data = append(w.data, b[:keep]...)
	w.truncated = w.truncated || keep < len(b)
	return len(b), nil
}

func runDialOwnerProbe(ctx context.Context, port int) dialOwnerResult {
	// No PID filter: the conflicting socket can belong to the parent or another
	// process. No address-family filter: a wildcard IPv6 socket can matter too.
	cmd := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-iUDP:"+strconv.Itoa(port), "-F0pcftPn")
	return collectDialOwnerProbe(ctx, cmd, port)
}

func collectDialOwnerProbe(ctx context.Context, cmd *exec.Cmd, port int) dialOwnerResult {
	stdout := &dialOwnerOutput{limit: 12 << 10}
	stderr := &dialOwnerOutput{limit: 4 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Bound pipe cleanup too if the child exits but a descendant holds a pipe.
	cmd.WaitDelay = 10 * time.Millisecond
	result := dialOwnerResult{Started: time.Now(), ExitCode: -1}
	err := cmd.Run()
	result.Ended = time.Now()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		result.Error = err.Error()
	}
	result.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	result.Truncated = stdout.truncated || stderr.truncated
	result.Warnings = string(stderr.data)
	result.Owners, result.ParseIncomplete = parseDialPortOwners(stdout.data, port)
	return result
}

// lsof's NUL mode terminates fields with NUL and process/file sets with NL.
// Parse only complete sets. A remote endpoint match must not become an owner.
func parseDialPortOwners(data []byte, port int) ([]dialPortOwner, bool) {
	owners := make([]dialPortOwner, 0)
	incomplete := len(data) > 0 && data[len(data)-1] != '\n'
	if incomplete {
		data = data[:bytes.LastIndexByte(data, '\n')+1]
	}
	var pid int
	var command string
	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		if line[len(line)-1] != 0 {
			incomplete = true
			continue
		}
		fields := bytes.Split(line[:len(line)-1], []byte{0})
		if len(fields[0]) == 0 {
			incomplete = true
			continue
		}
		owner := dialPortOwner{PID: pid, Command: command}
		protocol := ""
		process := fields[0][0] == 'p'
		valid := true
		for _, field := range fields {
			if len(field) == 0 {
				valid = false
				continue
			}
			value := string(field[1:])
			switch field[0] {
			case 'p':
				var err error
				pid, err = strconv.Atoi(value)
				command = ""
				valid = valid && err == nil && pid > 0
			case 'c':
				command = value
			case 'f':
				owner.Descriptor = value
			case 't':
				owner.Family = value
			case 'P':
				protocol = value
			case 'n':
				owner.Local, owner.Remote, _ = strings.Cut(value, "->")
			default:
				valid = false
			}
		}
		if process {
			if !valid || command == "" {
				incomplete = true
				pid = 0
			}
			continue
		}
		_, localPort, err := net.SplitHostPort(owner.Local)
		fd, fdErr := strconv.Atoi(owner.Descriptor)
		if !valid || owner.PID <= 0 || owner.Command == "" || fdErr != nil || fd < 0 || err != nil || protocol != "UDP" || (owner.Family != "IPv4" && owner.Family != "IPv6") {
			incomplete = true
			continue
		}
		if localPort == strconv.Itoa(port) {
			owners = append(owners, owner)
		}
	}
	return owners, incomplete
}

func dialOwnerOptIn(name string) bool {
	return dialOwnerEnabled(name, runtime.GOOS, os.Getenv("QUIC_GO_DIAL_OWNER_UNTIL"), time.Now())
}
