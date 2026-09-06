#!/usr/bin/env python3
"""Temporary, exclusive cpuset partition for this experiment only (run via sudo)."""

import json
import os
from pathlib import Path
import pwd
import sys

ROOT = Path("/sys/fs/cgroup")
GROUP = ROOT / "codex-d2-burst-20260906"
CPUS = "8-10,24-26"


def cpu_set(value):
    result = set()
    for item in value.strip().split(","):
        if not item:
            continue
        lo, _, hi = item.partition("-")
        result.update(range(int(lo), int(hi or lo) + 1))
    return result


def snapshot():
    return {str(path.relative_to(ROOT)): path.read_text().strip()
            for path in [ROOT / "cpuset.cpus.effective", GROUP / "cpuset.cpus.partition",
                         GROUP / "cpuset.cpus.effective", GROUP / "cpuset.cpus.exclusive.effective",
                         GROUP / "cgroup.procs"] if path.exists()}


def check():
    assert (GROUP / "cpuset.cpus.partition").read_text().strip() == "root"
    assert cpu_set((GROUP / "cpuset.cpus.effective").read_text()) == cpu_set(CPUS)
    assert not cpu_set((ROOT / "cpuset.cpus.effective").read_text()) & cpu_set(CPUS)


def cleanup():
    if GROUP.exists():
        # Return the CPUs before removal even if a failed child is still exiting.
        (GROUP / "cpuset.cpus.partition").write_text("member")
        GROUP.rmdir()  # Refuse silently deleting/moving any surviving process.
    assert cpu_set(CPUS) <= cpu_set((ROOT / "cpuset.cpus.effective").read_text())


action = sys.argv[1]
assert os.geteuid() == 0, "sudo required"
if action == "setup":
    assert not GROUP.exists(), "Refusing to reuse an existing partition"
    assert cpu_set(CPUS) <= cpu_set((ROOT / "cpuset.cpus.effective").read_text())
    GROUP.mkdir()
    try:
        (GROUP / "cpuset.cpus").write_text(CPUS)
        (GROUP / "cpuset.cpus.partition").write_text("root")
        check()
    except BaseException:
        cleanup()
        raise
    print(json.dumps(snapshot()))
elif action == "check":
    check()
    print(json.dumps(snapshot()))
elif action == "enter":
    check()
    user = pwd.getpwnam("josh")
    (GROUP / "cgroup.procs").write_text(str(os.getpid()))
    os.initgroups(user.pw_name, user.pw_gid)
    os.setgid(user.pw_gid)
    os.setuid(user.pw_uid)
    os.execvp(sys.argv[2], sys.argv[2:])
elif action == "cleanup":
    cleanup()
    print(json.dumps(snapshot()))
else:
    raise SystemExit("setup | check | enter COMMAND... | cleanup")
