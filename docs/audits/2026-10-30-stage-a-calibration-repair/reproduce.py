#!/usr/bin/env python3
"""Four finite, isolated Linux/arm64 loopback diagnostics; never a cloud launch."""
import hashlib
import json
import pathlib
import shutil
import subprocess
import tempfile

ROOT = pathlib.Path(__file__).resolve().parent
IMAGE = "sha256:d9b6a1beb9474adc2a1ce842573639b44b2a0dd49b6669f51d3821baf9ec37c9"


def run(*args, cwd=ROOT):
    return subprocess.run(args, cwd=cwd, check=True, text=True, capture_output=True)


def main():
    info = json.loads(run("docker", "info", "--format", "{{json .}}").stdout)
    if info["Architecture"] not in ("aarch64", "arm64") or info["NCPU"] < 6:
        raise SystemExit("requires native Linux/arm64 Docker with at least six CPUs")
    run("docker", "image", "inspect", IMAGE)  # Never pull or silently change images.
    expected = json.loads((ROOT / "diagnostic-summary.json").read_text())["frozen_source_sha256"]
    frozen = ROOT.parent / "2026-10-29-bbrv3-stage-a" / "probe"
    for name, digest in expected.items():
        if hashlib.sha256((frozen / name).read_bytes()).hexdigest() != digest:
            raise SystemExit(f"frozen source changed: {name}")
    (ROOT / "build").mkdir(exist_ok=True)
    output = pathlib.Path(tempfile.mkdtemp(prefix="reproduce-", dir=ROOT / "build"))
    for name in ("go.mod", "go.sum"):
        shutil.copy2(ROOT / name, output / name)
    for variant in ("original", "second", "third", "fourth", "candidate"):
        source = output / variant
        shutil.copytree(ROOT / "probe" if variant == "candidate" else frozen, source)
        offset = {"second": "1 << 25", "third": "1 << 24", "fourth": "3 << 24"}.get(variant)
        if offset:
            path = source / "main.go"
            text = path.read_text()
            if text.count("var seq uint32") != 1:
                raise SystemExit("unexpected sender sequence declaration")
            path.write_text(text.replace("var seq uint32", "var seq uint32 = " + offset))
        result = run("env", "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0", "go", "build", "-trimpath", "-buildvcs=false", "-o", f"{variant}-probe", f"./{variant}", cwd=output)
        (output / f"{variant}-build.log").write_text(result.stdout + result.stderr)
    (output / "run.sh").write_text('''#!/usr/bin/env bash
set -euo pipefail
cd /evidence
for trial in 1 2 3 4; do
 receiver=original
 if (( trial > 2 )); then receiver=candidate; fi
 start=$(($(date +%s%N)+2000000000))
 taskset -c 0,1 timeout 9 ./$receiver-probe -role receive -local 127.0.0.1:41001 -start "$start" -seconds 3 -output "$trial-receive.json" >"$trial-receive.stdout" 2>"$trial-receive.stderr" &
 receiver_pid=$!
 sleep 0.15
 pids=()
 variants=(original second third fourth)
 for i in 0 1 2 3; do
  taskset -c "$((i+2))" timeout 9 ./"${variants[$i]}"-probe -role send -local "127.0.0.1:$((41002+i))" -peer 127.0.0.1:41001 -start "$start" -seconds 3 -rate 0 -output "$trial-send-$i.json" >"$trial-send-$i.stdout" 2>"$trial-send-$i.stderr" &
  pids+=("$!")
 done
 for i in 0 1 2 3; do
  if wait "${pids[$i]}"; then echo 0 >"$trial-send-$i.exit"; else echo $? >"$trial-send-$i.exit"; fi
 done
 if wait "$receiver_pid"; then echo 0 >"$trial-receive.exit"; else echo $? >"$trial-receive.exit"; fi
done
''')
    container = output.name
    try:
        result = run("docker", "run", "--rm", "--pull=never", "--name", container, "--network", "none", "--cpuset-cpus=0-5", "--memory=1g", "--read-only", "--tmpfs", "/tmp", "-v", f"{output}:/evidence", IMAGE, "timeout", "45", "bash", "/evidence/run.sh")
        (output / "container.log").write_text(result.stdout + result.stderr)
    except subprocess.CalledProcessError as error:
        (output / "container-error.log").write_text((error.stdout or "") + (error.stderr or ""))
        raise
    finally:
        subprocess.run(["docker", "rm", "-f", container], capture_output=True, check=False)
        print(output)
    # Diagnostics retain failures: successful orchestration is not calibration success.
    for trial in range(1, 5):
        receipt = json.loads((output / f"{trial}-receive.json").read_text())
        print(trial, {key: receipt[key] for key in ("Received", "SocketDrops", "Invalid")})


if __name__ == "__main__":
    main()
