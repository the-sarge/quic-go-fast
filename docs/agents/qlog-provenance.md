# Qlog provenance

The normative acceptance criteria are from [issue #77](https://github.com/the-sarge/quic-go-fast/issues/77):

- [ ] Select the replacement version when a versioned replacement supplies the dependency, rather than repeating the original requirement.
- [ ] Correct both interop linker overrides to target the current qlogwriter version symbol.
- [ ] Cover ordinary modules, versioned replacements, local replacements and linker-provided revisions.
- [ ] Preserve the upstream module path and ADR 0002 adoption route; do not change release or publication policy.

## Boundary and evidence

Go's module loader and runtime/debug own dependency and replacement metadata; the linker owns explicit version overrides. The qlogwriter initializer selects the version emitted as code_version. A versioned replacement reports its own version, an unversioned local replacement retains the required version with the existing intended ` (replaced)` marker, and an explicit linker revision takes precedence. A local replacement may have an empty version or `(devel)` in build information. The marker does not claim to identify a local checkout's actual commit. Repository binaries continue to use the documented linker override. Public API/module identity, ADR 0002 adoption, release policy and packet-emission contracts remain unchanged.

Shipped behavior consists of the initializer correction and the two interop Dockerfile linker targets. Tests are verification aids and this document is traceability metadata. Example-level evidence uses actual consumer builds and the public qlog constructor's serialized code_version for ordinary modules, versioned fork replacements, local replacements and linker overrides with and without a replacement. Synthetic module versions come from an isolated local proxy populated with current trace-writer sources; these fixture versions are not published releases or adoption examples. Go parses the module files and build metadata; no alternate module syntax parser is introduced.

The terminating plan is the failing/passing consumer regression, affected-package tests, one focused race run, builds of both interop entrypoints with their corrected linker flags, one uncached full local test suite, go vet, go mod tidy -diff, and formatting checks. Review comprises independent Standards and Spec reviews, one fully briefed initial RAS review, verification of accepted fixes if needed, and at most one fresh replacement review. No mutation campaign, repeated successful hosted runs, full Docker/network-simulator deployment, platform cross-product beyond existing hosted CI, or provenance-policy redesign is included.

This repository has upstream-style workflows that run for draft PRs, with no Taskfile/preflight or ci.yml/ci-* gate. Require applicable existing hosted PR checks successful and unskipped on the exact live head. Mark ready after review/local certification, confirm head/base and checks, then squash merge with --match-head-commit. Journal after the product merge, then complete the OmniFocus task. Keep review history and exact-head receipts outside this contract.
