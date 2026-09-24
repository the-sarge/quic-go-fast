# Verbatim accepted criteria

Source: https://github.com/the-sarge/quic-go-fast/issues/618#issuecomment-5820598213

- [ ] A pinned baseline receipt accounts for all six cells, or explicitly records the finite-budget stop and missing evidence.
- [ ] The receipt includes commands, exact trace and measurement boundaries, host/tool/source identity, raw samples, allocation data, tail sample counts, profiles, variation and limitations.
- [ ] Both scans are exercised through valid real recovery behavior; setup and repeated duplicate-ACK shortcuts do not masquerade as ACK work.
- [ ] Any candidate satisfies the trigger and attribution rule, stays within the one-candidate budget, and records paired comparison, bounded storage/update costs and correctness results.
- [ ] The final disposition is keep, recommend a separately scoped production change, or inconclusive, with reasons. Passing local thresholds never claims native qualification.
- [ ] Scratch code and results are retained as immutable investigation evidence with a reproduction recipe, not installed as a maintained harness or CI timing gate. No candidate production changes are merged under this issue.
