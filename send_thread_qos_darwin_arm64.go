//go:build darwin && arm64 && !ios

package quic

// The send thread QoS hook on Apple silicon Macs. It calls public libSystem
// functions from <pthread/qos.h>, <pthread.h> and <libproc.h> through the
// trampolines in send_thread_qos_darwin_arm64.s, without cgo; see
// docs/adr/0010-libsystem-calls-without-cgo.md.

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// syscall.syscall calls a libSystem function on the system stack. The runtime
// keeps it reachable by linkname because golang.org/x/sys/unix depends on it.
//
//go:linkname sendQoSLibcCall syscall.syscall
func sendQoSLibcCall(fn, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno)

//go:linkname sendQoSLibcCall6 syscall.syscall6
func sendQoSLibcCall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

var (
	libc_pthread_self_trampoline_addr                  uintptr
	libc_pthread_get_qos_class_np_trampoline_addr      uintptr
	libc_pthread_set_qos_class_self_np_trampoline_addr uintptr
	libc_pthread_threadid_np_trampoline_addr           uintptr
	libc_proc_pidinfo_trampoline_addr                  uintptr
)

//go:cgo_import_dynamic libc_pthread_self pthread_self "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_get_qos_class_np pthread_get_qos_class_np "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_set_qos_class_self_np pthread_set_qos_class_self_np "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_pthread_threadid_np pthread_threadid_np "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_proc_pidinfo proc_pidinfo "/usr/lib/libSystem.B.dylib"

// QoS classes from <sys/qos.h>.
const (
	qosClassUserInitiated = 0x19
	qosClassDefault       = 0x15
	qosClassUnspecified   = 0x00
)

// procPIDThreadID64Info is PROC_PIDTHREADID64INFO from <sys/proc_info.h>.
const procPIDThreadID64Info = 15

// procThreadInfo is struct proc_threadinfo from <sys/proc_info.h>.
type procThreadInfo struct {
	userTime    uint64
	systemTime  uint64
	cpuUsage    int32
	policy      int32
	runState    int32
	flags       int32
	sleepTime   int32
	curPriority int32
	priority    int32 // pth_priority, the thread's base priority
	maxPriority int32
	name        [64]byte
}

type libSystemSendQoSThread struct{}

func newPlatformSendQoSThread() sendQoSThread { return libSystemSendQoSThread{} }

func (libSystemSendQoSThread) lock()   { runtime.LockOSThread() }
func (libSystemSendQoSThread) unlock() { runtime.UnlockOSThread() }

// newSendQoSClass encodes a QoS class and its relative priority.
func newSendQoSClass(class uint32, relative int32) sendQoSClass {
	return sendQoSClass(uint64(class) | uint64(uint32(relative))<<32)
}

func (c sendQoSClass) class() uint32   { return uint32(c) }
func (c sendQoSClass) relative() int32 { return int32(uint32(c >> 32)) }

func (libSystemSendQoSThread) raise() (sendQoSClass, error) {
	prior, err := currentSendQoSClass()
	if err != nil {
		return 0, err
	}
	return prior, setSendQoSClass(newSendQoSClass(qosClassUserInitiated, 0))
}

func (libSystemSendQoSThread) basePriority() (int, error) {
	var tid uint64
	// A null thread is the calling thread.
	if r, _, _ := sendQoSLibcCall(libc_pthread_threadid_np_trampoline_addr, 0, uintptr(unsafe.Pointer(&tid)), 0); r != 0 {
		return 0, fmt.Errorf("pthread_threadid_np: %w", syscall.Errno(r))
	}
	var info procThreadInfo
	n, _, errno := sendQoSLibcCall6(libc_proc_pidinfo_trampoline_addr, uintptr(os.Getpid()), procPIDThreadID64Info, uintptr(tid), uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0)
	if int32(n) != int32(unsafe.Sizeof(info)) {
		if errno != 0 {
			return 0, fmt.Errorf("proc_pidinfo: %w", errno)
		}
		return 0, errors.New("proc_pidinfo: short thread info")
	}
	return int(info.priority), nil
}

// restore returns the thread to its recorded class. The setter rejects an
// unspecified class, which the default class stands in for.
func (libSystemSendQoSThread) restore(prior sendQoSClass) error {
	if prior.class() == qosClassUnspecified {
		prior = newSendQoSClass(qosClassDefault, 0)
	}
	return setSendQoSClass(prior)
}

func currentSendQoSClass() (sendQoSClass, error) {
	self, _, _ := sendQoSLibcCall(libc_pthread_self_trampoline_addr, 0, 0, 0)
	var class uint32
	var relative int32
	if r, _, _ := sendQoSLibcCall(libc_pthread_get_qos_class_np_trampoline_addr, self, uintptr(unsafe.Pointer(&class)), uintptr(unsafe.Pointer(&relative))); r != 0 {
		return 0, fmt.Errorf("pthread_get_qos_class_np: %w", syscall.Errno(r))
	}
	return newSendQoSClass(class, relative), nil
}

// setSendQoSClass sets the calling thread's class; on failure the class is
// unchanged.
func setSendQoSClass(c sendQoSClass) error {
	if r, _, _ := sendQoSLibcCall(libc_pthread_set_qos_class_self_np_trampoline_addr, uintptr(c.class()), uintptr(c.relative()), 0); r != 0 {
		return fmt.Errorf("pthread_set_qos_class_self_np: %w", syscall.Errno(r))
	}
	return nil
}
