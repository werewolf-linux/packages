"""Runtime checks must preserve the appliance's Python restrictions."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile

assert os.geteuid() == 1000
assert os.statvfs(__file__).f_flag & os.ST_RDONLY
assert os.statvfs(sys.executable).f_flag & os.ST_RDONLY
capabilities = dict(line.split(':', 1) for line in Path('/proc/self/status').read_text().splitlines())
for field in ('CapInh', 'CapPrm', 'CapEff', 'CapBnd', 'CapAmb'):
    assert int(capabilities[field], 16) == 0, (field, capabilities[field])

def refused(args, reason):
    result = subprocess.run([sys.executable, *args], capture_output=True, text=True)
    assert result.returncode != 0, args
    assert reason in result.stderr, result.stderr

refused(['-c', 'raise SystemExit(0)'], 'python:inline')
with tempfile.TemporaryDirectory() as directory:
    script = Path(directory) / 'writable.py'
    script.write_text('raise SystemExit(0)\n')
    refused([str(script)], 'python:writable-source')
print('python: unprivileged, read-only code; inline and writable scripts refused')
