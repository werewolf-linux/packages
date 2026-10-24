"""Identify the exact upstream and downstream inputs of a package's build."""
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
# The shared files that change what a build makes, beside the package's own
# directory. Publishing, notes, drift reports and discovery are left out:
# changing them rebuilds nothing.
SHARED = ['Makefile', 'requirements.txt', 'tools.lock.json', 'release/packages.pub',
          'scripts/packages.py', 'scripts/inputs.py', 'scripts/refresh.py', 'scripts/derive.py',
          'scripts/patching.py', 'scripts/source.py', 'scripts/prepare.py', 'scripts/verify.py',
          'scripts/build.py', 'scripts/runtime.py', 'scripts/runtime_hardening_log.h', 'scripts/signing-key.py', 'scripts/install-melange.py',
          'scripts/setup-runner.py']
# Within a package's directory, what documents it rather than builds it.
DOCUMENTS = ['README.md', 'docs', 'tools', 'metadata.json']

def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def policy_hashes(pkg, root=ROOT):
    files = [root / name for name in SHARED]
    merged = root == ROOT and (root.parent / 'scripts/catalog.py').is_file()
    if merged:
        files.extend([root.parent / '.github/workflows/wolfi-build.yml', root.parent / 'scripts/catalog.py'])
    for path in pkg.dir.rglob('*'):
        if path.relative_to(pkg.dir).parts[0] not in DOCUMENTS:
            files.append(path)
    anchor = root.parent if root == ROOT and (root.parent / 'scripts/catalog.py').is_file() else root
    return {str(p.relative_to(anchor)): sha256(p) for p in sorted(files)
            if p.is_file() and '__pycache__' not in p.parts and p.suffix != '.pyc'}

def file_modes(directory):
    return {str(p.relative_to(directory)): p.stat().st_mode & 0o7777
            for p in sorted(directory.rglob('*'))
            if p.is_file() and p.name != 'upstream.lock.json'}

def identity(lock):
    # A Wolfi commit changing an unrelated package is provenance, not a rebuild.
    relevant = {key: lock[key] for key in ['recipe', 'source_commit', 'revision', 'sha256', 'mode', 'policy_sha256']}
    return hashlib.sha256(json.dumps(relevant, sort_keys=True, separators=(',', ':')).encode()).hexdigest()

def verify(pkg, directory=None):
    directory = directory or pkg.inputs
    lock = json.loads((directory / 'upstream.lock.json').read_text())
    for name, digest in lock['sha256'].items():
        path = directory / name
        if not path.resolve().is_relative_to(directory.resolve()) or sha256(path) != digest:
            raise ValueError('Upstream input mismatch: ' + name)
    if lock['mode'] != file_modes(directory):
        raise ValueError('Upstream file modes changed in transit')
    if lock['policy_sha256'] != policy_hashes(pkg, pkg.root):
        raise ValueError('Downstream policy changed; refresh the inputs')
    if lock['build_id'] != identity(lock):
        raise ValueError('Build identity mismatch')
    return lock
