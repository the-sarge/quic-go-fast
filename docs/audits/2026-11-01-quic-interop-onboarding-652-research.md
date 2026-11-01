# QUIC interop runner onboarding and evidence ownership

Research date: 2026-11-01, America/New_York. This answers [Research what registering in the QUIC interop runner requires](https://github.com/the-sarge/quic-go-fast/issues/652), part of the fork-independence map. Local baseline: `902b2a5b4012393218cb918283b9d2cefa6e608b`. Runner source: `a224e5ecb600284b1a6a604f15d87cfccfcd6315`; network simulator source: `e557a54510e3578868f8c14cf3aa37e0fc6c76d0`. These were read on the research date. No endpoint was built or run, no interoperability result was produced, and no registration, publishing destination, CI requirement, release policy, or evidence owner was selected.

## Answer

Distinct public participation requires a runnable endpoint image, a publicly pullable image reference, and a maintainer-reviewed addition to the runner's implementation registry. Local testing does not require public registration. The existing fork already has the endpoint packaging and a build-only workflow, but the inspected registry has no `quic-go-fast` entry and the fork workflow deliberately does not publish an image. Registration would make results attributable to the selected fork image; it would not transfer upstream quic-go's pass results to that image. [Runner onboarding][runner-readme], [registry][registry], [fork workflow][fork-workflow]

This report distinguishes **requirements** stated by runner documentation, **observations** from pinned source, and **inferences/recommendations** for this fork. The remaining choices belong to [Decide independent CI, interoperability and review evidence ownership](https://github.com/the-sarge/quic-go-fast/issues/1067); this research does not resolve that human decision.

## Endpoint and registration requirements

The documented endpoint contract is a container controlled through environment variables and mounted directories. The simulator's reference setup uses a `Dockerfile` and executable `run_endpoint.sh`; the entrypoint calls `/setup.sh` to route traffic through the simulator, dispatches on `ROLE=client|server`, and waits for `sim:57832` before starting the client. The published endpoint base image supplies that machinery. These files are the documented integration pattern, not a requirement to expose a permanent Internet-accessible QUIC server. [Simulator endpoint instructions][sim-readme]

| Contract | Required behavior or documented convention |
| --- | --- |
| Roles and cases | Read `ROLE` and `TESTCASE`; exit **127** for an unsupported or unknown test case. The runner actively probes this behavior for each selected role before running tests. |
| Server | Listen on UDP port 443; serve generated files from `/www`; load `/certs/cert.pem` and `/certs/priv.key`. |
| Client | Download the space-separated URLs in `REQUESTS` into `/downloads`; exit 0 on completion or 1 on error. |
| Application protocol | Use HTTP/0.9 for ordinary QUIC cases and HTTP/3 for the HTTP/3 case. |
| Logs | Write endpoint logs under `/logs`; if exporting TLS secrets, use NSS key-log format at `SSLKEYLOGFILE`; if exporting qlog, use `QLOGDIR`. |
| Architecture | The online service requires `linux/amd64`; the documentation recommends publishing both amd64 and arm64. |

Sources: [QUIC endpoint contract][quic-doc], [runner onboarding/log conventions][runner-readme], [unknown-case probe][interop-code]. TLS secrets are especially useful: several current tests return unsupported when the trace cannot be decrypted. qlog is a diagnostic output convention, not a documented prerequisite for every test. [Test implementations][tests]

The onboarding README says to publish on Docker Hub and submit a PR adding the implementation to `implementations_quic.json`. **Observed qualification:** the current registry already contains public GHCR and Google Artifact Registry references, so Docker Hub is not the only registry accepted in practice. A fork-owned publicly pullable reference is the practical requirement; choice of registry and naming remains a human decision. The registry entry has a distinct name mapping to `image`, `url`, and `role` (`both`, `client`, or `server`). It has no per-test capability list: unsupported behavior is reported by the running endpoint. No requirement to pass every test, pay a fee, or obtain a separate conformance certificate is stated in these onboarding sources. Acceptance of a new fork entry remains the runner maintainers' decision. [README][runner-readme], [current entries][registry], [registry parser][implementations]

## Local use versus public participation

Local use requires Python and the runner requirements, Docker with Compose, and Wireshark 4.5.0 or newer. Its Linux IPv6 setup loads `ip6table_filter`. The CLI selects peers and cases with `-s`, `-c`, and `-t`, writes JSON with `-j`, and can replace an existing implementation's image with `-r name=image`. The replacement must name an already registered key in the local JSON file; a new distinct local name requires a local registry edit. These are local operations, independent of a registration PR. [Prerequisites][runner-readme], [CLI implementation][run]

For example, after independently preparing a local fork image, a diagnostic run can use `python3 run.py -s ngtcp2 -c quic-go -r quic-go=quic-go-fast-interop:local -t handshake,transfer -j fork-client.json`, then swap the roles to exercise the fork server. This command was **not executed**. Its output still labels the replacement `quic-go`, so retained evidence must explicitly record that substitution; a distinct local registry key is clearer for lasting fork evidence. Inference from [replacement and role selection code][run].

Public participation adds image publication, a distinct registry entry, and the service's scheduled execution/public results. Publishing an image alone does not register it, and adding an entry does not make a build a passing interoperability run. The existing `quic-go` entry points to `martenseemann/quic-go-interop:latest` and upstream's repository URL, not this fork. [Registry][registry], [scheduled runner][schedule], [execution workflow][workflow]

## Coverage and the actual fork adapter

The pinned runner enables **22 QUIC test cases and two measurements**. The older unknown-version negotiation case exists in source but is absent from the active list; the documentation explicitly calls it disabled. `v2` is a different test: start with v1 and negotiate v2 using compatible version negotiation, not merely connect directly using v2. The table below follows the executable test-to-endpoint mapping, which is more precise than comparing the documentation's names with adapter switch statements. [Active list and mappings][tests], [documented cases][quic-doc]

| Active matrix cases | Endpoint `TESTCASE` (client / server) | Fork adapter observation at baseline |
| --- | --- | --- |
| `handshake`, `longrtt` | `handshake` / `handshake` | Accepted in both roles. |
| `transfer`, `multiplexing`, `amplificationlimit`, `blackhole`, `transferloss`, `transfercorruption`, `ipv6` | `transfer` / `transfer` | Accepted in both roles. |
| `rebind-port`, `rebind-addr` | `transfer` / `transfer` | Accepted in both roles; the simulator changes the address/port. |
| `chacha20`, `retry`, `resumption`, `zerortt`, `http3` | Corresponding name / same name | Accepted in both roles; dedicated behavior exists. |
| `handshakeloss`, `handshakecorruption` | `multiconnect` / `multiconnect` | Accepted in both roles. |
| `keyupdate` | `keyupdate` / `transfer` | Accepted; client lowers its first key-update interval. |
| `ecn` | `ecn` / `ecn` | Rejected with 127 in both roles. |
| `v2` | `v2` / `v2` | Rejected with 127 in both roles. |
| `connectionmigration` | `transfer` / `connectionmigration` | Client accepts `transfer`; server rejects with 127. Client acceptance does not establish preferred-address migration. |
| Measurements: `goodput`, `crosstraffic` | `transfer` / `transfer` | Accepted in both roles. |

Sources: [runner mappings and checks][tests], [default mapping][testcase], [fork client][fork-client], [fork server][fork-server]. Thus 19 active cases have recognized modes on both sides; this is **not 19 passing tests**, nor proof that all required semantics are implemented. In particular, missing `rebind-*` literals in the fork are not an adapter gap because those matrix cases pass `transfer`. Conversely, library ECN or QUIC v2 support does not make the corresponding runner case supported when its endpoint mode exits 127.

The suite exercises ordinary handshake and file integrity, concurrent streams and stream limits, retry tokens, resumption/early data, ChaCha20, key updates, basic HTTP/3, long RTT, loss/corruption/blackout conditions, IPv6, NAT rebinding and path validation, compatible v2 negotiation, and server-preferred-address migration. Its packet checks are deliberately bounded. For example, ECN checks for marking and ACK-ECN presence but explicitly does not validate the reported ACK-ECN counters; amplification testing includes a tolerance up to four times received bytes in its accounting. These are useful scenario observations, not an exhaustive RFC conformance argument. [Exact assertions][tests]

The two measurements use a nominal 10 Mbps environment, with cross-traffic adding TCP CUBIC competition. Goodput is calculated from known transferred file bytes and the first-to-last server 1-RTT packet capture timestamps, with five repetitions. It is not an application acceptance/deadline metric, a high-rate benchmark, or a general congestion-control qualification. [Measurement code][measurements]

The active QUIC list has no dedicated QUIC DATAGRAM, multipath, WebTransport, PMTU discovery, or exhaustive HTTP/3/QPACK semantics case. WebTransport has a separate registry and suite; QUIC registration does not enroll this endpoint there. Some unlisted behavior can occur incidentally during transfers, but incidental execution is not a targeted assertion. For this fork, native platform/socket paths, batching/buffer ownership, race behavior, long-running recovery, adversarial security behavior, and BBR coexistence still need their own evidence if they are part of the eventual acceptance policy. This is an inference about the boundaries of the [active tests][tests], [separate protocol registries][runner-readme], and [fork's BBR evidence contract][bbr-adr], not a proposal to add new mandatory gates now.

## What exists here, and what publication would add

The fork's image workflow triggers on main pushes, `v*` tags and pull requests, targets amd64/arm64, and sets `push: false` with local `quic-go-interop` tags. Its comment explicitly defers publication until a fork-owned destination is selected. The Dockerfile builds this checkout's client/server, records `git rev-parse HEAD` in `commit.txt`, embeds a descriptive version for qlog, and uses the moving simulator endpoint `latest` base. The shell prints the commit and configures simulator routing. These are concrete starting assets; this research has not verified that a particular workflow run built or that an image can execute. [Workflow][fork-workflow], [Dockerfile][fork-docker], [entrypoint][fork-entrypoint]

The integration workflow runs repository tools, version-negotiation, FIPS and self tests across configured Go/platform variants, including selected race/GSO/ECN variants. A search of all pinned workflow files found no runner invocation; the interop-specific workflow only builds. Self tests and a Docker build are valuable but do not supply cross-implementation results from a separately built peer. [Integration workflow][fork-integration], [pinned workflow tree][fork-workflows]

Both endpoint binaries construct ordinary `quic.Config` values without selecting BBR. Given the accepted default-Reno contract, registering this image as-is would exercise that default configuration, not qualify the opt-in BBR configuration. A wire-compatible fork can still change recovery, packet emission, timing and congestion behavior in ways that affect peer outcomes; wire compatibility is a maintained contract, not a result receipt. [Client][fork-client], [server][fork-server], [default controller contract][bbr-adr], [compatibility contract][compatibility]

## Publication and interpreting results

The runner repository schedules QUIC runs at 03:15 and 15:15 UTC. Its workflows pull each configured image, inspect and archive the pulled image for the run, run client/server pairs, aggregate JSON, upload per-pair logs and results to `interop.seemann.io`, and update a `latest` link. The website offers historical run selection. This is observed scheduling and publication machinery, not a promised execution SLA or immutable retention guarantee. [Schedule][schedule], [image pull and aggregation][workflow], [pair execution][pair-workflow], [public site](https://interop.seemann.io/)

Read the per-case JSON and logs, not merely the GitHub Actions conclusion: the public pair workflow uses `continue-on-error` and `|| true` around runner invocations. Core runner results distinguish succeeded, failed, and unsupported. Several tests can be unsupported because TLS keys are unavailable; an explicit 127 can also produce unsupported. Core multi-peer postprocessing can relabel universal failures as unsupported, although the current public workflow runs one pair per invocation and the CLI exempts single-peer selections from that rewriting. A grey cell is therefore not a clean pass or a complete feature inventory. [Pair workflow][pair-workflow], [result processing][interop-code], [single-peer exemption][run]

Normal retained test logs include endpoint outputs and simulator pcaps; unsupported cases are not copied through the same per-case retention path. The public workflow removes simulator/client/server detail directories for performance measurements before upload. This makes the published matrix useful for discovery, but insufficient by itself as a complete, durable qualification archive. Recommendation: any future fork evidence policy should retain the actual fork SHA/image digest, runner and peer identities, selected roles/cases, environment, raw outcomes and needed logs, with an explicit distinction between failure, unsupported and not run. The existing commit marker helps, but moving image tags and base images are not full reproducibility pins. [Retention code][interop-code], [measurement cleanup][pair-workflow], [fork image provenance][fork-docker]

## What upstream evidence transfers

| Evidence | What can be reused | What must be established for this fork |
| --- | --- | --- |
| Upstream endpoint design and inherited code/tests | Integration knowledge, protocol intent, test cases and debugging history, with provenance. The runner docs explicitly offer upstream quic-go as an example. | That this fork's built endpoint obeys the contract and executes the intended configuration. |
| Upstream public matrix cells | Historical evidence about the upstream image, peer, role, scenario and run that actually produced them; useful comparison data. | Passing results for the fork image in each selected direction/case. Common ancestry and preserved wire format do not change the tested artifact's identity. |
| Unchanged wire/API contracts | A reason to expect peers and adapters to remain reusable, and a baseline for regression comparison. | Behavior after fork-specific recovery, emission, buffering and controller changes; compatibility intent does not prove those transitions. |
| Upstream fixes and assurance practices | Technical rationale and possible reusable tests, subject to source-level review. | Fork-specific CI results, review/security ownership and whatever recurring evidence the owner chooses; upstream's participation does not execute those responsibilities for the fork. |

This table is an evidence-attribution inference from [the registry's image identity][registry], [runner execution against selected images][interop-code], [upstream adapter example][quic-doc], and the fork's [compatibility][compatibility] and [controller/recovery contracts][bbr-adr]. No current upstream green-cell count is claimed, and no upstream result is presented as a fork result. A fork-versus-upstream run is useful regression evidence; agreement between related implementations alone can miss a shared defect, so it does not replace comparisons with independently implemented peers.

## Decisions left for the human

The existing [independent evidence ownership decision](https://github.com/the-sarge/quic-go-fast/issues/1067) already covers the next questions: whether to join the public service, run a local/private lane, or do both; who maintains the image and investigates failures; which peer/role/configuration/case results establish an initial baseline; what must recur for changes or releases; how ECN/v2/migration adapter gaps are treated; and which non-matrix evidence supports DATAGRAM, native performance and opt-in BBR claims. A registry choice, image-update cadence, evidence retention policy and relationship to release requirements remain decisions, not onboarding facts.

Recommendation: use this research to resolve that existing decision before authorizing publication or registration. If participation is chosen, first make the selected fork image and its outcome provenance concrete through local runs in both roles, then submit the distinct registry entry. This adds no new frontier ticket, implementation, approval gate, or CI budget; it identifies the evidence the already-open decision needs.

[runner-readme]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/README.md
[sim-readme]: https://github.com/quic-interop/quic-network-simulator/blob/e557a54510e3578868f8c14cf3aa37e0fc6c76d0/README.md
[quic-doc]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/quic.md
[registry]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/implementations_quic.json
[implementations]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/implementations.py
[run]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/run.py
[interop-code]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/interop.py
[tests]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/testcases_quic.py
[testcase]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/testcase.py#L108
[measurements]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/testcases_quic.py#L1327
[schedule]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/.github/workflows/interop-quic.yml
[workflow]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/.github/workflows/interop.yml
[pair-workflow]: https://github.com/quic-interop/quic-interop-runner/blob/a224e5ecb600284b1a6a604f15d87cfccfcd6315/.github/workflows/interop-test.yml
[fork-workflow]: https://github.com/the-sarge/quic-go-fast/blob/902b2a5b4012393218cb918283b9d2cefa6e608b/.github/workflows/build-interop-docker.yml
[fork-docker]: https://github.com/the-sarge/quic-go-fast/blob/902b2a5b4012393218cb918283b9d2cefa6e608b/interop/Dockerfile
[fork-entrypoint]: https://github.com/the-sarge/quic-go-fast/blob/902b2a5b4012393218cb918283b9d2cefa6e608b/interop/run_endpoint.sh
[fork-client]: https://github.com/the-sarge/quic-go-fast/blob/902b2a5b4012393218cb918283b9d2cefa6e608b/interop/client/main.go
[fork-server]: https://github.com/the-sarge/quic-go-fast/blob/902b2a5b4012393218cb918283b9d2cefa6e608b/interop/server/main.go
[fork-integration]: https://github.com/the-sarge/quic-go-fast/blob/902b2a5b4012393218cb918283b9d2cefa6e608b/.github/workflows/integration.yml
[fork-workflows]: https://github.com/the-sarge/quic-go-fast/tree/902b2a5b4012393218cb918283b9d2cefa6e608b/.github/workflows
[compatibility]: https://github.com/the-sarge/quic-go-fast/blob/902b2a5b4012393218cb918283b9d2cefa6e608b/docs/adr/0001-upstream-compatibility.md
[bbr-adr]: https://github.com/the-sarge/quic-go-fast/blob/902b2a5b4012393218cb918283b9d2cefa6e608b/docs/adr/0009-opt-in-bbrv3.md
