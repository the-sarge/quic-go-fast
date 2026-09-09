# Execution ledger

M1 — predeclared: H1/H2/H3, unchanged fresh random callback, macOS arm64 Go 1.27.0, no race, scale 1. Capture all decisions and endpoint traces. A matching Dial timeout stops all further fresh random cases.

M1 — exit 0; see `raw/M1.log` and `raw/M1.jsonl`.

M2 — predeclared: M1 selected no handshake corruption; H1/H2/H3 remain unresolved for Dial. Capture the unchanged random sequence with the same binary and environment, expecting either a correlated handshake drop/recovery sequence or an explicitly nondiscriminating pass. Last macOS random allocation.

M2 — exit 0; see `raw/M2.log` and `raw/M2.jsonl`.

L1 — predeclared: both macOS cases completed Dial without selected handshake corruption. H1/H2/H3 remain unresolved. Run the unchanged instrumented callback on Linux Go 1.27.1 with race, GSO disabled, scale 3; seek checksum-correlated handshake drops/recovery, replacement errors or a recovery stall. Stop fresh random executions if Dial times out.

L1 — exit 0; Dial succeeded at 25.264 ms from capture start; neither server handshake datagram was selected. See `raw/L1*`. This does not falsify H1/H2/H3 for a failing sequence.

L2 — predeclared: final random allocation; L1 did not exercise handshake corruption. Same Linux binary/environment. Seek a checksum-correlated handshake replacement and its write/receive/recovery consequences for H1/H2/H3. A pass without this evidence remains nondiscriminating.

L2 — exit 0. Dial succeeded at 185.606 ms from capture start. Selected outgoing datagrams 4, 6, 7 and 11 before Dial completed; all four mutations are inside Handshake packet bytes and all direct writes returned the full datagram length without error. Random allocation exhausted: macOS 2/2, Linux 2/2, total 4/4. No matching random timeout.

F1 — predeclared mechanism probe, additional focused allocation 1/8. On macOS scale 1, replace the last byte of the first QUIC packet in each of the first eleven outgoing datagrams that start with a Handshake packet; XOR that byte with 1. Forward every other datagram normally, with no random draws in corruption decisions. Preserve the original direct-send/drop path, delay, Dial deadline and transfer assertions. L2 showed consecutive Handshake replacements and recovery; H1 predicts a sufficiently long fixed burst can keep TLS incomplete until the unchanged deadline despite successful writes and continuing PTOs. H2 predicts failed writes or absent receiver evidence; H3 predicts missing recovery despite outstanding data. Expect authentication drops and exponential PTOs, then Dial expiry if eleven replacements cover the deadline. This is a constructed possible mechanism, not a retained historical schedule or an exact replay of L2.

F2 — predeclared matched control, additional focused allocation 2/8, contingent on F1 capture being usable. Same first-eleven Handshake selection rule and direct-send/drop path, but replace the byte with itself (an allowed no-op in the original random distribution). No corruption-decision random draws. H1 predicts completed Dial/transfer with usable handshake data; failure or unexplained drops would preserve H2/H3 alternatives. Live timing, packetization and TLS bytes will differ. Stop after these cases if they support a possible mechanism and a finite next action.

F1 — exit 1; see `raw/F1.log` and `raw/F1.jsonl`.

F2 — exit 0; see `raw/F2.log` and `raw/F2.jsonl`.

Final interpretation: F1 reproduces the Dial assertion timeout only under a constructed eleven-Handshake corruption schedule; all eleven direct writes and matching authentication drops were captured, with the next server PTO after context expiry. F2 passes through the same direct-send path with a no-op replacement. Supported possible mechanism; historical cause unresolved. Stop with additional focused usage 2/8. The maintainer subsequently offered budget flexibility if useful; no extension was used. Active instrumentation removed after retaining both standalone patches. Build-only checks: random macOS, random Linux race, mechanism macOS all passed. No full-suite or CI execution was invoked.
