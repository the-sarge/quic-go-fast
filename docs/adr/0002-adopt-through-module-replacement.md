---
status: accepted
---

# Adopt the fork through an application-owned module replacement

The fork retains `github.com/quic-go/quic-go` as its declared Go module path and is hosted at `github.com/the-sarge/quic-go-fast`. Applications select an exact fork version through a `replace` directive in their main module. This preserves existing imports and supports matched upstream/fork comparisons while avoiding an internal import-path rename that would complicate upstream integration.

Replacement directives in dependencies do not propagate to consuming applications. wiremux's measurement builds and GridCast's application builds must each select the fork explicitly and record the selected revision in their evidence. A wiremux release containing its own replacement does not make downstream applications use the fork. Published examples must use real released tags or Go-generated pseudo-versions; no illustrative version is an existing release.

Giving the fork its own module identity and migrating imports is deferred while its value is being established. Adoption therefore carries explicit per-application version maintenance. Existing-API consumers can switch back to upstream through dependency configuration, but consumers of optional fork-only APIs must remove or adapt those calls first. Go's [module reference](https://go.dev/ref/mod#go-mod-file-replace) and [fork guidance](https://go.dev/doc/modules/managing-dependencies#external_fork) govern the replacement behavior.
