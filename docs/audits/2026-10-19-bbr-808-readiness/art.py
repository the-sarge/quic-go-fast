"""Redirect #712's byte-identical aids (run.py and the modules importing it) to this record's artifact directory.

Copied from slice 6's art.py (9b38f0ab, itself #739's); only the directory and this sentence change.

Import this before anything imports names from run, so `from run import ART, OBS`
binds the redirected paths.
"""
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import run  # noqa: E402

run.ART = HERE.parents[2] / '.local/bbr-808-readiness'
run.OBS = run.ART / 'observations'
