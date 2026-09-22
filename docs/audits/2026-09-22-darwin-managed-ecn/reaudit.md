# D1 Darwin family-mapping re-audit

## Trigger and evidence

Preflight at `0449f0bdd3f7bc75528275b9938e028022152409` found that the Linux-delivered family gate assumes IPv4 metadata always requires IP_RECVTOS. On m4mini (macOS 26.6.2 build 25G83, Darwin 25.6.0 arm64, Go 1.27.0), a UDP dual-stack AF_INET6 socket has IPV6_V6ONLY=0, accepts mapped IPv4 datagrams, rejects IP_RECVTOS with EINVAL, and successfully enables IPV6_RECVTCLASS. Native ReadMsgUDP returns IPV6_TCLASS carrying the exact IPv4 ECN value for mapped traffic. This is a setup-policy mismatch, not an unsupported kernel result.

A finite native probe observed all four receive values for udp4, udp6, dual-stack mapped IPv4 and dual-stack IPv6. Existing OOB ordinary sends and the endpoint's native batch writer preserved ECT(0) and CE in all four rows. These preliminary observations are not exact-head certification. The raw characterization is retained in [native-characterization.log](native-characterization.log).

## Scoped disposition and gates

Retain child #504, its supported-or-unsupported outcome, one product PR, universal typed lease/wrapper domain, authoritative native OOB/parser representation and the shared L1 correlation/singleton owner. Clarify only the endpoint-owned family-to-option mapping in the normative plan. No alternate parser, raw public authority, second adapter, sendmsg_x redesign, neighboring platform behavior change or transitional seam is admitted.

The invariant remains exact incoming and outgoing ECN on every admitted route. The plan's semantic matrix remains the coverage authority; its family row now names Darwin's required options. Required-option failures must disable the endpoint, while irrelevant option failures must not. The existing finite budget covers this distinction; no extra repetitions, platforms, mutations or tests beyond that budget are added. Preservation, failure, lease cleanup and wrapper gates remain pending product certification. Linux inspection may be factored mechanically, but its qualification policy must stay identical.

Splitting receive from send would still publish a half-capability; adding a platform track or a second owner remains unnecessary. The current owner can enforce the corrected family policy centrally and the native matrix terminates the evidence obligation. The provisional implementation is preserved and paused until this clarification reaches main and the child pointer is synchronized.

Darwin 27 sendmsg_x qualification is tracked separately in [#516](https://github.com/the-sarge/quic-go-fast/issues/516); D1's native qualification uses the already admitted Darwin 25 host.

## Dispatch fit

The named post-L1 dispatch manifest measures 102,134 bytes / 12,928 whitespace words, giving 25,856 input tokens by `max(bytes/4, words*2)`, within 26,000. The plan input is Decision plus the exact D1 slice, acceptance criteria and validation gates; duplicate introductory narrative is excluded. Other inputs are program lines 1–80, ADRs 0006/0007, both shared baselines, overlay, the named source declarations/ranges and eight named preservation tests. No L1 test helper is reused. Re-measure when these inputs change; this receipt is not code certification.
