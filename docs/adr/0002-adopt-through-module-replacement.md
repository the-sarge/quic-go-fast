---
status: accepted
---

# Adopt the fork through an application-owned module replacement

The fork retains `github.com/quic-go/quic-go` as its declared Go module path and is hosted at `github.com/the-sarge/quic-go-fast`. Applications select an exact fork version through a `replace` directive in their main module. This preserves existing imports and supports matched upstream/fork comparisons while avoiding an internal import-path rename that would complicate upstream integration.

Replacement directives in dependencies do not propagate to consuming applications. wiremux's measurement builds and GridCast's application builds must each select the fork explicitly and record the selected revision in their evidence. A wiremux release containing its own replacement does not make downstream applications use the fork. Published examples must use real released tags or Go-generated pseudo-versions; no illustrative version is an existing release.

Giving the fork its own module identity and migrating imports is deferred while its value is being established. Adoption therefore carries explicit per-application version maintenance. Existing-API consumers can switch back to upstream through dependency configuration, but consumers of optional fork-only APIs must remove or adapt those calls first. Go's [module reference](https://go.dev/ref/mod#go-mod-file-replace) and [fork guidance](https://go.dev/doc/modules/managing-dependencies#external_fork) govern the replacement behavior.

## wiremux socket integration compatibility

The joint socket integration design keeps wiremux buildable and functional with upstream quic-go, with an explicit optional integration for fork capabilities. Requiring the fork for wiremux's built-in QUIC transport is rejected at this stage so that upstream comparisons and rollback remain available, accepting the cost of maintaining both paths during evaluation. This consumer requirement does not prohibit optional fork-only interfaces; it requires isolating their use from wiremux's ordinary downstream build. [ADR 0006](0006-explicit-external-packet-io.md) selects explicit structural transport registration and records the ownership, managed-reuse and fixed-peer design. This decision does not change the application-owned replacement model or claim completed implementation.

The first usable integration enables offloads through wiremux's existing selected-peer wrapper. Native fixed-peer enforcement and wrapper removal remain a separately gated improvement in the same joint design rather than a prerequisite for initial integration. Supporting wrappers first serves other consumers and permits measurement before moving enforcement responsibility, at the cost of retaining wrapper overhead initially. The [execution companion](2026-09-14-wiremux-packet-io-integration.md) points to the complete program through reusable endpoints, qualification, releases, consumers and closeout. Exact interface names are frozen by its bounded contract stage; existing caller contracts remain in force.
