#!/usr/bin/env python3
"""Build one recipe on a native Linux runner, without release credentials."""
import argparse
import json
import os
from pathlib import Path
import platform
import subprocess
import common


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('package')
    parser.add_argument('--arch', choices=common.ARCHES, required=True)
    args = parser.parse_args()
    if platform.system() != 'Linux' or platform.machine() != args.arch:
        raise ValueError('CI packages require a native Linux runner for ' + args.arch)
    os.chdir(common.ROOT)
    meta = common.metadata(args.package)
    melange = os.environ.get('MELANGE', 'melange')
    pin = json.loads((common.ROOT / 'tools.lock.json').read_text())['melange']['version']
    if 'v' + pin not in subprocess.check_output([melange, 'version'], text=True):
        raise ValueError('install pinned Melange with scripts/install-melange.py')
    commit = os.environ.get('GITHUB_SHA') or subprocess.check_output(['git', '-c', 'safe.directory=' + str(common.ROOT), 'rev-parse', 'HEAD'], text=True).strip()
    timestamp = subprocess.check_output(['git', '-c', 'safe.directory=' + str(common.ROOT), 'log', '-1', '--format=%ct', '--', args.package], text=True).strip()
    if not timestamp.isdigit():
        raise ValueError('missing source commit timestamp')
    output = common.ROOT / 'build/artifacts' / args.package
    destination = output / args.arch
    if destination.exists():
        raise ValueError('use a clean artifact directory for each build')
    output.mkdir(parents=True, exist_ok=True)
    scratch = common.ROOT / 'build/runner' / args.package
    scratch.mkdir(parents=True, exist_ok=True)
    env = {k: os.environ[k] for k in ['PATH', 'HOME', 'LANG', 'SSL_CERT_FILE'] if k in os.environ}
    env.update(SOURCE_DATE_EPOCH=timestamp, TMPDIR=str(scratch))
    subprocess.run([melange, 'build', str(common.context(args.package)), '--runner', 'bubblewrap', '--arch', args.arch,
                    '--source-dir', str(common.ROOT / args.package), '--out-dir', str(output),
                    '--cache-dir', str(common.ROOT / 'build/cache/melange'),
                    '--apk-cache-dir', str(common.ROOT / 'build/cache/apk'), '--generate-index=false',
                    '--namespace', 'werewolf', '--git-commit', commit,
                    '--cpu', os.environ.get('MELANGE_CPU', str(os.cpu_count() or 2)),
                    '--memory', os.environ.get('MELANGE_MEMORY', '8Gi'), '--timeout', '5h'], check=True, env=env)
    expected = {name + '-' + meta['version'] + '.apk' for name in meta['names']}
    if {p.name for p in destination.glob('*.apk')} != expected:
        raise ValueError('missing or unexpected built packages')
    for name in meta['names']:
        common.apk(destination / (name + '-' + meta['version'] + '.apk'), name, meta['version'], args.arch)
    record = dict(format='werewolf-package-build/1', **meta, arch=args.arch, commit=commit,
                  run_id=os.environ.get('GITHUB_RUN_ID', ''), source_date_epoch=int(timestamp),
                  sha256={name: common.sha256(destination / name) for name in sorted(expected)})
    (destination / 'build.json').write_bytes(common.encode(record))


if __name__ == '__main__':
    main()
