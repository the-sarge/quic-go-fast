# Receive-queue experiment evidence closeout

The owner authorized review, validation and merge of evidence PRs #13, #14 and #15 after adopting the independent DATAGRAM parser-copy removal in #16. These PRs preserve completed investigations; none promotes a receive-storage candidate. Earlier protocols and reports describe their historical baselines and decisions, including the then-outstanding review gates. This closeout records the subsequent authorization to merge the evidence without reopening completed D2.

## Accepted criteria

- Experimental tools remain opt-in.
- Results and limitations are accurately documented.
- No experimental queue implementation enters production.

Keep shipped runtime and public API identical to the live integration base. Candidate patches remain inert evidence for their exact frozen baselines. Preserve original manifests, timing samples, profiles and recorded hashes unchanged; do not relabel synthetic queue delivery as application/network throughput or inconclusive intervals as passed gates. Preserve the decision to keep the existing runtime queue.

The archived Linux Go measurement aids require both the `queue_experiment` build tag and `QUEUE_EXPERIMENT=1` to execute measurements. Python collectors and host-isolation scripts require explicit invocation and remain outside normal build, test and CI paths. These are retained investigation sources, not a supported benchmark framework or a new product dependency. Future measurements require a separately authorized investigation; this closeout does not run collectors, reserve CPUs, or repeat a performance campaign.

## Representation and evidence boundary

The representation domain is the fixed manifests and source snapshots produced by the recorded experiments, with their documented commands and Linux environment. Python's JSON and gzip readers own decoding; each existing analyzer owns the checks and summary calculation for its recorded matrix. No universal importer, arbitrary manifest compatibility or recursive assurance of verification tools is claimed. The normal partial-refill FIFO regression is a product-behavior characterization; the tagged benchmarks and scripts are verification aids, and the reports, patches and raw artifacts are evidence metadata.

The Linux ring/head-index experiment was inconclusive under its fixed all-workload timing gate. The paced follow-up found mixed latency and host interference. The isolated burst diagnosis did not reproduce the large high-rate advantage and retained a low-rate tail regression. Merging the records preserves these limitations; it does not change their acceptance thresholds or imply a runtime adoption.

## Finite completion plan

For each PR, integrate the current main branch before review, inspect the runtime diff, reproduce its saved analysis from the unchanged compressed manifest, validate compressed artifacts and local Markdown links, and check patch applicability against each recorded baseline. Do not regenerate plots or profiles unless a concrete inconsistency requires it. Verify ordinary root tests and the relevant FIFO/DATAGRAM race tests, repository static analysis and formatting; verify tagged Linux compilation and no-opt-in skips plus a bounded correctness smoke when a measurement aid changes. The Linux-only tools do not imply a Windows/macOS measurement guarantee. Hosted repository workflows provide the ordinary platform checks before exact-head merge.

Use one fully briefed fresh review with three configured reviewers per PR, independent finding disposition, exact-head verification of accepted code fixes and at most one replacement review. Cheap documentation-only corrections use the shared no-rerun policy. Stronger tool-hardening ideas are follow-ups unless they prevent this finite archival outcome. Stop for a material boundary change or a demonstrated unsafe merge; do not widen the experiment or chase reviewer exhaustion.

Merge #13, then #14, then the #15 follow-up, preserving the stack's evidence and the current production code. Journal the completed evidence closeout after the product merges. No completed program issue or task is reopened, and no runtime candidate branch is merged.

## Opt-in source packaging

PR #13 originally retained ordinary Go benchmarks, which normal CI benchmark enumeration would discover. Closeout adds the Linux/build-tag and environment guards to those two benchmark functions and sets the explicit collector environment for newly built archival sources. The original measured commits, binaries, samples and hashes are unchanged. To compile the current retained sources on Linux use `go test -tags=queue_experiment -c`; set `QUEUE_EXPERIMENT=1` only for an explicitly requested diagnostic. To reproduce historical binary identities, use the recorded frozen commits and original toolchain instead.
