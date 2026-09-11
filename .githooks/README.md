# Git Hooks

This directory contains an optional pre-commit hook for working with quic-go. Install it with:

```bash
git config core.hooksPath .githooks
```

The hook requires Bash, Git, the repository's supported Go toolchain and `gofumpt` on `PATH`. It validates a temporary Git checkout of the current index, including Git's checkout attributes and any alternate index supplied by Git for a partial commit. Unstaged edits and untracked files are excluded. The hook does not format, tidy or stage caller files; failures leave the working tree and index unchanged. Temporary files are removed on exit.

- Added, copied, modified, renamed and type-changed Go files are passed to `gofumpt -d` with their staged content. Paths containing spaces, tabs, newlines, glob characters or leading dashes are supported. Deleted files are omitted; with no staged Go files the formatter is skipped. Staged Go symlinks are rejected rather than following a target outside the staged inputs.
- The `integrationtests/gomodvendor` module is checked on every invocation with `GOWORK=off go mod tidy -diff`. Its repository-relative replacement resolves to the staged root, so module validation sees staged source and module files together. The command reports required tidy changes without applying them. Tool errors, missing tools and nonzero validation exits fail the hook, even when stdout is empty; a successful formatter must also produce no diff.
- Tests use the standard Go testing dialect. Dependency restrictions, including the Ginkgo/Gomega prohibition, remain owned by `.golangci.yml`; reusable helpers should call `t.Helper()` per the [maintained conventions](../docs/agents/conventions.md). This hook does not scan test names or replace lint and test execution.

Run the isolated hook fixtures on macOS or Linux with:

```bash
go test -count=1 ./.githooks
```

The fixtures invoke the hook in disposable Git repositories and substitute tools at the executable boundary to exercise tool failures and inspect staged inputs. They check that caller files and index entries remain unchanged. The lint workflow runs this suite on Linux. Git owns index and pathname parsing; these fixtures provide regression coverage for the listed behavior, not a sandbox for untrusted Git filters, tools or repository configuration.
