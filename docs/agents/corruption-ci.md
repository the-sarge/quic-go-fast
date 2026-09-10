# Deterministic corruption CI

## Approved outcome

The maintainer approved this one-PR plan on 2026-09-10 after analysis of the I1 and I2 failure captures: make required corruption CI deterministic, preserve the original randomized callback as explicit opt-in stress, and add the observed Initial starvation mechanism as a regression. This supersedes the earlier requirement that ordinary CI preserve randomized corruption selection; it does not authorize production recovery changes. Integration base: `6cc25c236659b4b86594f558916a9cabf29f1d7f`; the original approved base was `8cba598931f38814ba8a81cd7fccbaaf0bbdfca4`. Worktree: `/Volumes/worktrees/quic-go-fast/issue-46-deterministic-ci`; branch: `codex/issue-46-deterministic-ci`.

## Implementation contract

- Preserve both raw JSONL captures, hashes, source/job identities, and packet-by-packet analysis under `docs/audits/issue-46-captured-corruption/`. Check observed timer selection/backoff against the recovery implementation. Explain these two failures without attributing every older timeout to the same cause.
- Add three simulated-link Initial CRYPTO cases: sustained damage permits ACK-only packets but requires deadline failure, retries, authentication rejection and no usable Initial CRYPTO; releasing the first retransmission requires handshake and bidirectional transfer; an undamaged control requires the same successful outcomes. Assert the intended interventions occurred. Use typed qlog observations and existing packet parsing, snapshotting scalar data rather than borrowed frames. These are mechanism regressions, not exact live TLS/UDP replay.
- Mandatory real-UDP `TestMITCorruptPackets` uses six finite cases: each of two directions crossed with damage to the first Initial, Handshake or short-header packet followed by reliable forwarding. Require actual damage, successful Dial and unchanged full transfer assertions. Preserve the proxy, delay, deadlines and payload.
- Original random corruption is enabled only by `QUIC_GO_TEST_RANDOM_CORRUPTION=1`. Preserve its probabilities, draw order, byte replacement, immediate endpoint write/drop semantics and assertions. Verify routing without running random stress.
- Preserve capture bounds, failure retention, successful cleanup, socket observations and hosted artifacts. Document the current contract and commands. No workflow changes.
- The maintainer approved an additional test-only repair after rebased Windows CI failed in `TestServerTokenValidation/expired_retry_token`: replace the four token-expiry setup sleeps in `server_test.go` with a bounded wait for the decoded timestamp age to exceed the configured maximum. Preserve the existing packet rejection assertions and production token validation. The failed job is [Windows / Go 1.27](https://github.com/the-sarge/quic-go-fast/actions/runs/34447438365/job/102775225417).

## Representation and artifacts

The self-test fixture owns selection. Supported inputs are repository-generated QUIC v1/v2 traffic on the existing simulated link and UDP proxy; existing QUIC parsing and typed qlog events own representation. The guarantee is example-level recovery, cancellation and diagnostic coverage, not universal recovery under arbitrary corruption. Tests and diagnostics are maintained verification aids; reports, plans and receipts are traceability metadata. Approval explicitly makes the deterministic suite a required CI deliverable, replacing an unconditional random-success gate with interpretable regressions. Maintain or retire these aids with the corruption fixture. Contract closure is not triggered: focused behavioral tests cover this verification-only change. There is no shipped behavior or required safety-enforcement change.

The blast radius is test selection, fixture-local state, test names, corruption-suite runtime and token-expiry setup in the server test fixture. Production recovery, APIs, wire behavior, dependencies, shared proxy behavior and PR #156's implementation remain unchanged. The existing capture's synchronous I/O observer effect remains disclosed. The investigation's raw inputs are immutable historical evidence; tests use semantic schedules instead of checksum/byte replay across fresh TLS sessions.

## Terminating evidence and review

Approved seams are test selection, the UDP corruption callback and public handshake/transfer outcomes on the simulated link. Use TDD for changed behavior with at most 12 focused development command executions, counting failures. No new randomized campaign, fuzzing, statistical repetition or standalone stress gate is authorized. Final focused invocations are v1 and v2 once each, plus v1 with race detection and scale 3; include inherited corruption/capture regressions and full proxy tests. Run one uncached full native suite and vet, lint, changed-file formatting, repository go-fix checks and root/FIPS module-tidy checks on the final clean pushed candidate. A relevant correction requires appropriate renewed validation; passing checks are not repeated just for confidence.

Review comprises independent Standards/Spec reviews, one fully briefed initial RAS review, verification of independently accepted fixes, and at most one fresh replacement review. The implementing agent judges and fixes; no automated fixer. The maintainer approved retaining the completed RAS review through the clean base reconciliation and the bounded token-expiry fixture amendment. Review that small test-only diff directly, run `TestServerTokenValidation` once natively and once with race detection, and refresh final local certification and hosted checks on the resulting head. Apply the shared finding dispositions and representation/precise-root stops. RAS uses two independent configured reviewers, `codex-astra` and `codex-sol`, with a 900-second per-agent limit and usable synthesis required. Brief reviewers with this contract, its acceptance criteria, artifact/domain boundaries and evidence budget.

The repository uses existing push/PR workflows, including on drafts; it has no `task preflight`, draft-skipping `ci-*` jobs or separate review protocol. Require applicable hosted checks successful and unskipped on the exact live PR head; no blind same-head rerun. Mark ready after review/local certification, then squash merge with a pinned head. Base advancement requires reconciliation and applicable review/certification. Journal only after product merge, without RAS. Then update #46 and its task with the precise resolution, revalidating any deferred findings against merged code. PR #156 and its journal are now merged into the integration base; their implementation is inherited unchanged by this fixture PR.

Stop with concrete evidence if a demonstrated recovery defect needs production changes, metadata cannot reliably map to datagrams in the declared domain, defined recovery controls fail, or accepted work exceeds scope/review/evidence budgets. Preserve work and seek a changed decision; do not reinterpret timeouts as success or extend deadlines.

## Commands and selected examples

`TestMITCorruptPackets` retains the two named directions, each with `initial`, `handshake` and `1RTT` cases. Each case XORs the last protected byte of the first matching packet and makes one immediate replacement write, suppressing the original proxy forward even on write failure. Later datagrams use the ordinary delayed proxy route. A matching packet can be coalesced with other QUIC packets; only its final byte changes. A selected case must introduce one actual mutation and complete the original bidirectional full-payload assertions within the existing scaled deadline. The existing random callback transparency and capture cleanup regressions remain active.

`TestHandshakeCapturedCorruption` uses `Initial starvation`, `release retransmission` and `undamaged` subtests. It changes server Initial CRYPTO while retaining ACK-only feedback; the release case forwards the first retransmission containing CRYPTO offset zero and subsequent traffic reliably. The simulated link has no GSO, one connection per direction, and ordered sender metadata: packet lengths delimit coalesced packets, available checksums bind original datagrams, and `wire.ParsePacket` checks selected Initial boundaries. Some short-header qlog events omit their optional checksum; their ordered lengths still consume the metadata queue. Mapping failures fail the fixture rather than being classified as transport starvation. Typed qlog observations count retries, authentication drops, usable CRYPTO and derived Handshake keys. Synctest settles observers before evidence assertions.

Ordinary focused coverage:

```sh
go test ./integrationtests/self -run '^(TestMITCorruptPackets|TestCorruption|TestHandshakeCorruption|TestHandshakeCapturedCorruption|TestHandshakeDiagnostics)' -version=1 -count=1
go test ./integrationtests/self -run '^(TestMITCorruptPackets|TestCorruption|TestHandshakeCorruption|TestHandshakeCapturedCorruption|TestHandshakeDiagnostics)' -version=2 -count=1
```

Explicit exploratory stress (not an execution request):

```sh
QUIC_GO_TEST_RANDOM_CORRUPTION=1 go test ./integrationtests/self -run '^TestMITCorruptPacketsRandom$' -version=1 -count=1 -v
```

The opt-in path retains its success assertions and can still fail on a permitted random sequence. Preserve its failure capture before deciding whether further exploration is informative. A fixed shuffle seed does not reproduce global `math/rand/v2` corruption choices. Ordinary CI does not set the opt-in variable, and the subprocess gate regression selects no random leaf.
