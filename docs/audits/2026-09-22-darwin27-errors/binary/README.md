# Exact Darwin 27 binary: sendmsg_x error owner

Inspected read-only on 2026-09-22. No kernel execution, patching, tracing, new socket probes, or repository edits were performed for this analysis. Run `python3 /tmp/quic-darwin27-binary-research/extract.py` to reproduce the mapping and three bounded disassembly excerpts with Python 3 and Xcode llvm-objdump. The script asserts the kernel digest and UUID before proceeding. The installed binary is not copied into these artifacts.

## Identity

Binary: `/System/Library/Kernels/kernel.release.t6041`. SHA-256: `d0cf2fb69845bc5a34624ba69aa2b4f35ee65b4dbca150068a6cc93c5bc13bae`. Mach-O UUID: `D46FC75B-A889-3796-A66C-D28FDD50FB68`, matching the coordinating agent's recorded running `kern.uuid`. Reported target: XNU `13432.1.9~1/RELEASE_ARM64_T6041`, macOS 27.0 build `26A428`. All addresses below are unslid image virtual addresses.

## Handler identity independent of source resemblance

The dispatch excerpt extracts the syscall selector into `w26`, uses its low 16 bits, then at `0xfffffe0007aa8040`–`0xfffffe0007aa8050` computes `0xfffffe00072289b0 + index*24`. At `0xfffffe0007aa81d8` it loads the handler at offset zero of that record; at `0xfffffe0007aa81ec` it authenticates/calls the handler with discriminator `0xbcad` and process/argument/return-value pointers. This identifies the actual dispatch table and stride without guessing from code resemblance.

Entry 481 is virtual address `0xfffffe000722b6c8`, file offset `0x2276c8`, raw pointer `0x8010bcad009ad544`; it has return type 6, four arguments and 16 bytes of 32-bit arguments. `extract.py` walks the actual fixup chain from page 259's start offset 8 to confirm that this is an ARM64E_KERNEL (format 7) authenticated rebase entry. Its low 32-bit runtime offset `0x9ad544` plus image base `0xfffffe0007004000` resolves to `0xfffffe00079b1544`. Pointer decoding follows the installed Apple SDK's `mach-o/fixup-chains.h` (format enum and `dyld_chained_ptr_arm64e_auth_rebase`). Neighbor entries 8 and 11 resolve to exported `_enosys` at `0xfffffe000796d4d0`; 480 resolves to the separately inspected receive-batch function. The userland probe's syscall 481 thus resolves to the inspected function, not merely a function with a similar string.

[Apple's published syscall definition](https://github.com/apple-oss-distributions/xnu/blob/ac9718fb1af618d5ce8678d0dc6e8a58f252216f/bsd/kern/syscalls.master) names syscall 481 `sendmsg_x`. The target dispatch mapping and instructions themselves are the semantic evidence; the older source provides names and explanatory vocabulary, not an assertion of target source equivalence.

## Generic path and accepted-count owner

The handler checks socket type against 2 at `0xfffffe00079b15c0`–`0xfffffe00079b15c8` and the protocol atomic flag at `0xfffffe00079b15cc`–`0xfffffe00079b15d4`. The possible specialized path additionally checks socket state bit 1 at `0xfffffe00079b16f0`–`0xfffffe00079b16f4`; an unset bit jumps to the generic path at `0xfffffe00079b15d8`. The bit corresponds to `SS_ISCONNECTED=0x0002` in [Apple's socket definitions](https://github.com/apple-oss-distributions/xnu/blob/ac9718fb1af618d5ce8678d0dc6e8a58f252216f/bsd/sys/socketvar.h). These branches contain no AF_INET, AF_INET6 or AF_UNIX comparison. Unconnected datagram sockets use this common path.

The generic path initializes count `x26=0` at `0xfffffe00079b1618`. It internalizes each message at `0xfffffe00079b1680`, then calls the per-message helper at `0xfffffe00079b16a0` (target `0xfffffe00079b0e00`, matching `sendit`'s role). A nonzero helper return branches to the shared error epilogue at `0xfffffe00079b16a4`; only a zero return advances count at `0xfffffe00079b16a8`. Message iteration continues at `0xfffffe00079b16c0`–`0xfffffe00079b16c4`. The helper invokes the socket's protocol send operation indirectly at `0xfffffe00079b0fa8`–`0xfffffe00079b0fd0`; it is not an AF_UNIX-specific call site.

The shared epilogue at `0xfffffe00079b172c` first requires nonzero count. Instructions `0xfffffe00079b1730`–`0xfffffe00079b1754` compute `error+1`, guard its unsigned range to 56, and test the corresponding bit in `0x0100021000000021`. Its set positions map exactly to error values `-1, 4, 35, 40, 55`: ERESTART, EINTR, EAGAIN/EWOULDBLOCK, EMSGSIZE and ENOBUFS. A match reaches `0xfffffe00079b1768`, clears `w0`, sign-extends the accepted count and writes it through the return-value pointer at `0xfffffe00079b1770`. With count zero these errors are preserved. No socket-family decision intervenes in this epilogue.

## Supported conclusion and boundary

In this exact target binary, a returned ENOBUFS, EAGAIN or EINTR from this common batch path cannot hide preceding messages whose per-message helper returned success: once the count is nonzero, these errors are converted to successful prefix-count returns. This closes the specific uncertainty about protocol-independent suppression in the batch owner. The native UDP EAGAIN/EINTR cases and AF_UNIX ENOBUFS cases remain separately classified observations.

This is direct target-binary evidence, not a native UDP ENOBUFS observation. It does not by itself establish that every lower-level UDP failure path is internally atomic, prove equivalence with other kernel builds, or qualify connected sockets. In particular, “returned error implies no preceding successful helper calls” must not be inflated into an independent proof of every possible packet-side effect inside the failing helper. Any stronger qualification claim must compose this evidence with the existing lower-layer contract and declared empirical guarantee level.

LLVM labels such as `_pru_sopoll_notsupp+...` and `_copystr+...` in the excerpts name the nearest exported symbol; they do not identify the stripped functions. Use the independently decoded dispatch target and the exact addresses above.
