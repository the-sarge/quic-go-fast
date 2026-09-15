# Archived container launches

All four launches ran from `/Volumes/worktrees/quic-go-fast/diagnose-317`. The source tree at `4fcf07fd9836a6dafebfde7bbbf21fef436d2dc7` had `observer.patch` applied for E1/E2, and `lifecycle.patch` applied for E3/Suite. These patches are alternatives against the baseline, not cumulative. Each container copied source at startup; no runtime source changed during its attempt.

Each invocation used this common prefix, followed by the attempt name and exact Go command recorded in its environment log:

```sh
docker run --rm --platform linux/amd64 --name diagnose-317-ATTEMPT \
  -v /Volumes/worktrees/quic-go-fast/diagnose-317:/source:ro \
  -v /Users/josh/diagnostics/diagnose-dial-317-2026-09-15:/evidence \
  sha256:20dfa9aeeb42795279e3e73318bdd425f1fe09e3cebed05484fce1a3ca9aef20 \
  bash /evidence/run.sh ATTEMPT go test ...
```

`ATTEMPT` was `e1`, `e2`, `e3`, then `suite`. E1 additionally mounted `quic-go-fast-gomod:/home/runner/go/pkg/mod` and `quic-go-fast-gocache:/home/runner/.cache/go-build` before the image argument. Those shared caches produced permission errors; E2/E3/Suite omitted both mounts and used private caches inside disposable containers. Each container ran as the image's `runner` user, without a user override. No cache permissions were changed.

The host tool session observed exits `1`, `0`, `0`, `1` respectively, matching the status files. Each container was removed automatically when its command ended. Environment timestamps mark the start before Go compilation; status timestamps mark completion. All four executions finished well below the ten-minute outer limit. Source preparation and environment inspection occurred before the timeout-wrapped Go command.
