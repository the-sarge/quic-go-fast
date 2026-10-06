"""Redirect #715's byte-identical aids (run.py and the modules importing it) to #734's artifact directory.

Import this before anything imports names from run, so `from run import ART, OBS`
binds the redirected paths.
"""
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import run  # noqa: E402

run.ART = HERE.parents[2] / '.local/bbr-pacing-wake'
run.OBS = run.ART / 'observations'
