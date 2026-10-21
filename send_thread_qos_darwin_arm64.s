//go:build darwin && arm64 && !ios

#include "textflag.h"

// Trampolines into libSystem for send_thread_qos_darwin_arm64.go, in the form
// golang.org/x/sys/unix uses: each jumps to the dynamically imported symbol,
// and its address is what the Go side passes to syscall.syscall.

TEXT libc_pthread_self_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_pthread_self(SB)
GLOBL	·libc_pthread_self_trampoline_addr(SB), RODATA, $8
DATA	·libc_pthread_self_trampoline_addr(SB)/8, $libc_pthread_self_trampoline<>(SB)

TEXT libc_pthread_get_qos_class_np_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_pthread_get_qos_class_np(SB)
GLOBL	·libc_pthread_get_qos_class_np_trampoline_addr(SB), RODATA, $8
DATA	·libc_pthread_get_qos_class_np_trampoline_addr(SB)/8, $libc_pthread_get_qos_class_np_trampoline<>(SB)

TEXT libc_pthread_set_qos_class_self_np_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_pthread_set_qos_class_self_np(SB)
GLOBL	·libc_pthread_set_qos_class_self_np_trampoline_addr(SB), RODATA, $8
DATA	·libc_pthread_set_qos_class_self_np_trampoline_addr(SB)/8, $libc_pthread_set_qos_class_self_np_trampoline<>(SB)

TEXT libc_pthread_threadid_np_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_pthread_threadid_np(SB)
GLOBL	·libc_pthread_threadid_np_trampoline_addr(SB), RODATA, $8
DATA	·libc_pthread_threadid_np_trampoline_addr(SB)/8, $libc_pthread_threadid_np_trampoline<>(SB)

TEXT libc_proc_pidinfo_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_proc_pidinfo(SB)
GLOBL	·libc_proc_pidinfo_trampoline_addr(SB), RODATA, $8
DATA	·libc_proc_pidinfo_trampoline_addr(SB)/8, $libc_proc_pidinfo_trampoline<>(SB)
