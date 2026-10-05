"""Redirect #712's byte-identical aids (run.py and the modules importing it) to #715's artifact directory.

Import this before anything imports names from run, so `from run import ART, OBS`
binds the redirected paths. The Mac runner (mac/run.py, #711's) is redirected
separately by mac_matrix.py.
"""
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import run  # noqa: E402

run.ART = HERE.parents[2] / '.local/bbr-linux-redemonstration'
run.OBS = run.ART / 'observations'
