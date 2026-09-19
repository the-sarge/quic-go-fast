package quic

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const dialCaptureEventBytes = 32 << 10

type dialCapture struct {
	mu                             sync.Mutex
	events                         []json.RawMessage
	milestones                     map[string]string
	bytes, dropped, encodingErrors int
	done                           bool
	rebindStarted                  time.Time
	rebindAddress                  string
	ownerEnabled                   bool
	ownerBudget                    *atomic.Bool
	originalClosed                 bool
	ownerProbe                     *dialOwnerProbe
	ownerStatus                    string
	ownerDeadline                  time.Time
}

func newDialCapture(name string) *dialCapture {
	c := &dialCapture{ownerEnabled: dialOwnerOptIn(name), ownerBudget: &dialOwnerClaimed, milestones: map[string]string{
		"original_socket":  "unavailable: dial-result synchronization not observed",
		"cancel_requested": "not requested before fixture exit",
		"dial_return":      "unobserved",
	}}
	c.ownerDeadline, _ = time.Parse(time.RFC3339, os.Getenv("QUIC_GO_DIAL_OWNER_UNTIL"))
	if c.ownerEnabled {
		c.ownerStatus = "no_qualifying_failure"
	} else {
		c.ownerStatus = "disabled_or_expired"
	}
	c.record("start", map[string]any{"test": name, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "args": os.Args, "run_id": os.Getenv("QUIC_GO_HTTP_RUN_ID"), "timescale": os.Getenv("TIMESCALE_FACTOR"), "owner_until": os.Getenv("QUIC_GO_DIAL_OWNER_UNTIL"), "pid": os.Getpid(), "parent_pid": os.Getppid(), "source": "source and actual shuffle seed belong to the matching command artifact"})
	return c
}

func (c *dialCapture) record(event string, data any) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recordLocked(event, data)
}

func (c *dialCapture) recordLocked(event string, data any) {
	if c.done {
		return
	}
	line, err := json.Marshal(map[string]any{"time": time.Now().UTC(), "event": event, "data": data})
	if err != nil {
		c.encodingErrors++
		c.dropped++
		return
	}
	if c.bytes+len(line) > dialCaptureEventBytes {
		c.dropped++
		return
	}
	if _, tracked := c.milestones[event]; tracked {
		c.milestones[event] = "recorded"
	}
	c.events = append(c.events, line)
	c.bytes += len(line)
}

func dialCaptureError(err error) map[string]any {
	if err == nil {
		return nil
	}
	result := map[string]any{"type": fmt.Sprintf("%T", err), "message": err.Error(), "is_closed": errors.Is(err, net.ErrClosed)}
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		result["errno"] = uint64(errno)
	}
	if op, ok := errors.AsType[*net.OpError](err); ok {
		result["op"], result["network"] = op.Op, op.Net
		if op.Source != nil {
			result["source"] = op.Source.String()
		}
		if op.Addr != nil {
			result["address"] = op.Addr.String()
		}
	}
	return result
}

// Call only after the dial result synchronizes access to the factory's socket.
// Control observes the Go handle without changing deadlines or duplicating it.
func (c *dialCapture) observeSocket(socket *net.UDPConn) {
	if socket == nil {
		c.record("original_socket", "unavailable: socket factory returned no handle")
		return
	}
	raw, err := socket.SyscallConn()
	if err == nil {
		err = raw.Control(func(uintptr) {})
	}
	c.mu.Lock()
	c.originalClosed = errors.Is(err, net.ErrClosed)
	c.mu.Unlock()
	c.record("original_socket", map[string]any{"identity": fmt.Sprintf("%p", socket), "address": socket.LocalAddr().String(), "control_error": dialCaptureError(err), "observed_closed": errors.Is(err, net.ErrClosed)})
}

func (c *dialCapture) rebind(addr *net.UDPAddr) (*net.UDPConn, error) {
	return c.rebindWith(addr, net.ListenUDP)
}

func (c *dialCapture) rebindWith(addr *net.UDPAddr, listen func(string, *net.UDPAddr) (*net.UDPConn, error)) (*net.UDPConn, error) {
	started := time.Now()
	c.mu.Lock()
	if !c.done {
		c.rebindStarted, c.rebindAddress = started, addr.String()
		c.recordLocked("rebind_enter", map[string]any{"network": "udp", "address": addr.String()})
	}
	c.mu.Unlock()
	conn, err := listen("udp", addr)
	c.mu.Lock()
	if !c.done {
		c.rebindStarted = time.Time{}
		c.recordLocked("rebind_return", map[string]any{"address": addr.String(), "duration_ns": time.Since(started).Nanoseconds(), "error": dialCaptureError(err)})
		if c.ownerEnabled && c.originalClosed && errors.Is(err, syscall.EADDRINUSE) {
			if c.ownerProbe == nil && !c.ownerDeadline.IsZero() && !time.Now().Before(c.ownerDeadline) {
				c.ownerStatus = "expired_before_trigger"
			} else if c.ownerProbe == nil {
				c.ownerProbe = startDialOwnerProbe(addr.Port, c.ownerBudget, runDialOwnerProbe)
				c.ownerStatus = "process_allowance_consumed"
				if c.ownerProbe != nil {
					c.ownerStatus = "requested"
				}
			}
			if p := c.ownerProbe; p != nil {
				if len(p.failures) < 64 {
					p.failures = append(p.failures, time.Now())
				} else {
					p.failuresTruncated = true
				}
			}
		}
	}
	c.mu.Unlock()
	return conn, err
}

// The admitted window ends at fixture return, before testing cleanup. It does
// not claim that transport workers stopped. Interrupted processes may omit it.
func (c *dialCapture) finish(failed bool) []byte {
	c.mu.Lock()
	if c.done {
		c.mu.Unlock()
		return nil
	}
	c.done = true
	owner := c.ownerProbe
	if !failed {
		c.events = nil
		c.mu.Unlock()
		if owner != nil {
			owner.finish(false)
		}
		return nil
	}
	stack := make([]byte, 64<<10)
	n := runtime.Stack(stack, true)
	report := map[string]any{
		"port_owner_status": c.ownerStatus, "snapshot_started": time.Now().UTC(),
		"schema": 1, "events": c.events, "milestones": c.milestones, "dropped": c.dropped, "encoding_errors": c.encodingErrors,
		"complete_observations": c.dropped == 0 && c.encodingErrors == 0 && c.rebindStarted.IsZero(),
		"goroutines":            string(stack[:n]), "goroutines_truncated": n == len(stack),
		"scope":                   "admitted observations before fixture cleanup; not transport quiescence",
		"production_close_result": "unavailable", "receive_loop_completion": "unavailable", "other_port_owner": "unavailable",
	}
	if !c.rebindStarted.IsZero() {
		report["unfinished_rebind"] = map[string]any{
			"address": c.rebindAddress, "result": "unavailable", "elapsed_ns": time.Since(c.rebindStarted).Nanoseconds(),
		}
	}
	c.events = nil
	c.mu.Unlock()
	// Freeze events and the stack before waiting, and let late binds return.
	if owner != nil {
		report["port_owner_observation"] = owner.finish(true)
	}
	data, err := json.Marshal(report)
	if err != nil || len(data) > 256<<10 {
		return []byte(`{"complete_observations":false,"summary_error":"encoding or report size limit"}`)
	}
	return data
}
