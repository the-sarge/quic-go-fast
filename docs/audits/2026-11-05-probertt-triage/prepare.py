"""Install the disposable issue #1210 diagnostic into a clean base worktree."""

import argparse
import json
import subprocess
from pathlib import Path

BASE = "466198e5b1a7b462cfc271ce1beb6b66299143be"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", type=Path)
    parser.add_argument("--source", choices=["source-v1", "source"], default="source")
    args = parser.parse_args()
    target = args.target.resolve()
    source = Path(__file__).resolve().parent / args.source
    def git(*arguments):
        return subprocess.check_output(["git", "-C", str(target), *arguments], text=True).strip()
    if git("rev-parse", "HEAD") != BASE or git("status", "--porcelain"):
        raise SystemExit("Target must be clean and pinned to " + BASE)
    if git("branch", "--show-current") in ("", "main", "master", "trunk"):
        raise SystemExit("Use a dedicated feature branch and worktree")
    files = json.loads((source / "files.json").read_text())
    for destination in files:
        if (target / destination).exists():
            raise SystemExit("Refusing to overwrite " + destination)
    patch = source / "instrumentation.patch"
    git("apply", "--check", str(patch))
    git("apply", str(patch))
    for destination, stored in files.items():
        (target / destination).write_bytes((source / Path(stored).name).read_bytes())
    print("Prepared disposable diagnostic at", target)


if __name__ == "__main__":
    main()
