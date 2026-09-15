# Optional packet-I/O consumer

Compile this unchanged consumer in two separate temporary main modules, each requiring `github.com/quic-go/quic-go v0.62.0`. The upstream module has no replacement and runs with argument `upstream`; the fork module replaces that module path with the absolute candidate worktree and runs with argument `fork`. Run `go mod tidy` and `go run . <selection>` in each isolated directory using the installed repository-approved Go toolchain. Never import both repository paths into one graph.

The structural interface intentionally uses only standard-library parameter types. Successful upstream execution establishes that the optional extension is absent without breaking compilation; successful fork execution requires both methods with the exact signatures. This is a small verification fixture, not a runtime helper module.
