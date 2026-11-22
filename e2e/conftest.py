"""End-to-end rig: the REAL host binary against a throwaway instance of the REAL Jarvis server code
(the support-host branch of project-jarvis) on a spare port, with its own empty home, no Redis,
no docker. Nothing here touches a running Jarvis.

    JARVIS_SERVER_SRC   checkout of project-jarvis on the feat/support-host branch
                        (default: ~/Documents/Projects/project-jarvis-support-host)
    JARVIS_HOST_BIN     host binary to test (default: dist/jarvis-host-linux-amd64, built if missing)
Run with the Jarvis Python environment, e.g.:
    PYTHONPATH=$JARVIS_SERVER_SRC/server:$JARVIS_SERVER_SRC \\
      ~/miniconda3/envs/jarvis/bin/python -m pytest e2e -v
"""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parent.parent
SERVER_SRC = Path(os.environ.get("JARVIS_SERVER_SRC", Path.home() / "Documents/Projects/project-jarvis-support-host"))
for extra in (SERVER_SRC / "server", SERVER_SRC):
    if str(extra) not in sys.path:
        sys.path.insert(0, str(extra))


@pytest.fixture(scope="session")
def host_bin() -> Path:
    given = os.environ.get("JARVIS_HOST_BIN")
    path = Path(given) if given else ROOT / "dist" / "jarvis-host-linux-amd64"
    if not path.exists():
        subprocess.run([str(ROOT / "scripts" / "build.sh"), "linux/amd64"], check=True)
    assert path.exists(), path
    return path
