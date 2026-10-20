---
status: accepted
---

# Public libSystem calls without cgo

The opt-in send thread QoS hook ([#859](https://github.com/the-sarge/quic-go-fast/issues/859)) must call libSystem functions that have no system-call equivalent: `pthread_set_qos_class_self_np`, `pthread_get_qos_class_np`, `pthread_self` and `pthread_threadid_np` from `<pthread/qos.h>` and `<pthread.h>`, and `proc_pidinfo` with `PROC_PIDTHREADID64INFO` from `<libproc.h>`. The fork builds without cgo, and the existing raw `syscall.Syscall6` calls such as `SYS_sendmsg_x` reach the kernel, not these library functions.

Call them the way `golang.org/x/sys/unix` does: `//go:cgo_import_dynamic` imports each symbol from `/usr/lib/libSystem.B.dylib`, an assembly trampoline jumps to it, and the Go side passes the trampoline's address to `syscall.syscall` or `syscall.syscall6`, reached by `//go:linkname`. The files are `send_thread_qos_darwin_arm64.go` and `.s`, built only for `darwin && arm64 && !ios`; every other build compiles a no-op. They build with `CGO_ENABLED=0`.

This borrows one invariant: the Go runtime keeps `syscall.syscall` and `syscall.syscall6` reachable by linkname. The runtime guarantees it for `golang.org/x/sys/unix`, which every Go program on macOS that uses that module already relies on, and the repository already uses linkname into the standard library for TLS. If a Go release withdraws it, the hook fails to build rather than misbehaving; the fallback is to read the same functions through `golang.org/x/sys/unix` if it exports them, or to drop the hook.

These functions are public API in the macOS SDK headers, so [the private-call rules](0008-darwin-batch-compatibility.md) for `sendmsg_x` do not apply: no kernel allowlist, no startup self-check and no `quic_go_no_private_syscalls` exclusion. The hook verifies its own effect at run time instead, by reading back the thread's base priority. Adding another libSystem function through this mechanism needs only that the function is public SDK API; a private one falls under ADR 0008's rules.
