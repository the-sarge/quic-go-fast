#!/usr/bin/env python3
"""Regenerate this record's cross-run tables from the pinned raw archives.

Run it from anywhere inside the repository:

    python3 -I crossrun.py          # print the tables
    python3 -I crossrun.py --check  # compare them with README.md

The archives are read with `git show` at the pinned record commits, which live
on the record branches `codex/bbr-l6-coexistence` and `codex/bbr-l6-diagnosis`.
Members are read in memory; nothing is extracted. Standard library only.

Definitions (see README.md, "Evidence"):
- The BBRv3 focal flow is an observation whose meta.json controller is bbrv3.
- Relay drops are relay2's forward focal Dropped counter; its 100 ms samples
  hold the cumulative value (column 5) and delivered bytes (column 7).
- The relay estimate of the switch is the first sample at which delivered bytes
  divided by the run's mean delivered packet size, plus drops, reaches the
  flow's ECT(0) arrival count. Every ECT(0) packet precedes the switch.
- The sender count sums the #792 trace's per-round lost packets (round rows,
  argument 1) over the rounds completed before the first row that records an
  ECN failure (a post row whose change mask, argument 0, has bit 6).
"""

import io
import json
import os
import subprocess
import sys
import tarfile

ARCHIVES = [
    ("#777", "71e88789e8b89dcfec960d4f54d20027ef1f77b9", "docs/audits/2026-10-17-bbr-l6-coexistence/raw.tar.gz"),
    ("#792", "89e71a60e652d4627cac41ee35b9ed3e718fcfa2", "docs/audits/2026-10-18-bbr-l6-diagnosis/raw.tar.gz"),
]
BEGIN, END = "<!-- crossrun:begin -->", "<!-- crossrun:end -->"
HERE = os.path.dirname(os.path.abspath(__file__))


def load(sha, path):
    data = subprocess.run(["git", "show", f"{sha}:{path}"], check=True, capture_output=True).stdout
    observations = {}
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:gz") as tar:
        for member in tar.getmembers():
            parts = os.path.normpath(member.name).split(os.sep)
            if member.isfile() and len(parts) == 3 and parts[0] == "observations" and parts[2] in ("meta.json", "relay.json", "focal-trace.json"):
                observations.setdefault(parts[1], {})[parts[2]] = json.load(tar.extractfile(member))
    return observations


def relay_switch(relay):
    focal = relay["Forward"]["Focal"]
    ect0 = focal["ECNIn"][2]
    size = focal["DeliveredBytes"] / focal["Delivered"]
    for sample in relay["Samples"]:
        if sample[7] / size + sample[5] >= ect0:
            return sample[0] / 1000, sample[5]
    return None, None


def trace_failure(trace, relay):
    lost = 0
    for row in trace["rows"]:
        if row[0] == 1:
            lost += row[41]
        if row[0] == 3 and row[40] & (1 << 6):
            seconds = (row[1] - relay["StartUnixNS"]) / 1e9
            drops = next(s[5] for s in relay["Samples"] if s[0] / 1000 >= seconds)
            return seconds, lost, drops
    return None, lost, None


def num(n):
    return f"{n:,}"


def tables():
    out, sender, relays, others = [], [], [], {}
    for label, sha, path in ARCHIVES:
        for name, files in sorted(load(sha, path).items()):
            meta, relay = files.get("meta.json"), files.get("relay.json")
            if meta is None or relay is None:
                continue
            focal = relay["Forward"]["Focal"]
            notect = focal["ECNIn"][0] / focal["Received"]
            if meta["controller"] != "bbrv3":
                group = others.setdefault((label, meta["arm"]), [])
                group.append((focal["Dropped"], focal["ECNIn"][0]))
                continue
            seconds, drops = relay_switch(relay)
            relays.append((label, name, focal["Received"], focal["ECNIn"][2], focal["ECNIn"][0], focal["Dropped"], focal["Dropped"] / focal["Received"], notect, seconds, drops))
            trace = files.get("focal-trace.json")
            if trace is not None:
                if trace["overflow"] != 0:
                    raise SystemExit(f"{label} {name}: trace overflowed by {trace['overflow']} rows")
                fail, lost, fail_drops = trace_failure(trace, relay)
                sender.append((label, name, fail, lost, fail_drops, focal["Dropped"], notect))

    out.append("**Sender traces.** Every #792 observation with a focal trace:")
    out.append("")
    out.append("| Observation | First ECN failure (s) | Packets the sender declared lost before it | Relay drops by then | Relay drops, whole run | Not-ECT share |")
    out.append("| --- | ---: | ---: | ---: | ---: | ---: |")
    for label, name, fail, lost, fail_drops, dropped, notect in sender:
        out.append(f"| {label} `{name}` | {fail:.1f} | {num(lost)} | {num(fail_drops)} | {num(dropped)} | {100 * notect:.1f}% |")
    out.append("")
    out.append(f"Range: failure at {min(s[2] for s in sender):.1f}–{max(s[2] for s in sender):.1f} s, after {num(min(s[3] for s in sender))}–{num(max(s[3] for s in sender))} declared losses ({len(sender)} observations).")
    out.append("")
    out.append("**Relay counters.** Every observation of a BBRv3 focal flow in either archive:")
    out.append("")
    out.append("| Observation | Forward packets | ECT(0) | Not-ECT | Drops | Drop share | Not-ECT share | Estimated switch (s) | Drops before the switch (est.) |")
    out.append("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
    for label, name, received, ect0, notect_n, dropped, share, notect, seconds, drops in relays:
        if seconds is None:
            out.append(f"| {label} `{name}` | {num(received)} | {num(ect0)} | {num(notect_n)} | {num(dropped)} | {100 * share:.2f}% | {100 * notect:.1f}% | no switch | — |")
        else:
            out.append(f"| {label} `{name}` | {num(received)} | {num(ect0)} | {num(notect_n)} | {num(dropped)} | {100 * share:.2f}% | {100 * notect:.1f}% | {seconds:.1f} | {num(drops)} |")
    switched = [r for r in relays if r[8] is not None]
    out.append("")
    out.append(f"Range over the {len(switched)} observations that switched: drop share {100 * min(r[6] for r in switched):.2f}–{100 * max(r[6] for r in switched):.2f}%, switch at {min(r[8] for r in switched):.1f}–{max(r[8] for r in switched):.1f} s, after {num(min(r[9] for r in switched))}–{num(max(r[9] for r in switched))} drops. Observations that never switched dropped {', '.join(num(r[5]) for r in relays if r[8] is None) or 'none'} packets in total.")
    out.append("")
    out.append("**Other focal flows** (frozen Reno), whole runs:")
    out.append("")
    out.append("| Archive and arm | Observations | Drops | Most Not-ECT packets |")
    out.append("| --- | ---: | ---: | ---: |")
    for (label, arm), group in sorted(others.items()):
        out.append(f"| {label} `{arm}` | {len(group)} | {num(min(g[0] for g in group))}–{num(max(g[0] for g in group))} | {num(max(g[1] for g in group))} |")
    return "\n".join(out) + "\n"


def main():
    os.chdir(subprocess.run(["git", "rev-parse", "--show-toplevel"], check=True, capture_output=True, text=True, cwd=HERE).stdout.strip())
    generated = tables()
    if sys.argv[1:] == ["--check"]:
        readme = open(os.path.join(HERE, "README.md"), encoding="utf-8").read()
        recorded = readme.split(BEGIN, 1)[1].split(END, 1)[0].strip("\n") + "\n"
        if recorded != generated:
            sys.stdout.write(generated)
            raise SystemExit("README.md tables differ from the archives (regenerated tables above)")
        print("README.md tables match the archives")
        return
    if sys.argv[1:]:
        raise SystemExit("usage: crossrun.py [--check]")
    sys.stdout.write(generated)


if __name__ == "__main__":
    main()
