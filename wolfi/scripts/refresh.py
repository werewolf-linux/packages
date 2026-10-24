#!/usr/bin/env python3
"""Capture each package's Wolfi recipe, unchanged.

A package follows its family's newest stable stream (glibc-2.44.yaml,
php-8.5.yaml, openjdk-27.yaml), not Wolfi's default: the highest numeric
stream whose version is a release, or Wolfi's snapshot of a released branch
(3.15.0_git20261010), never a pre-release such as 3.15.0_rc1. policy.json's
"stream" pins an older one, only where something still needs it.

A package Wolfi keeps in one recipe (libffi.yaml) names it as RECIPE instead.

usage: refresh.py [--wolfi CHECKOUT] [PACKAGE...]    every package by default"""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import yaml
from inputs import identity, policy_hashes, sha256, file_modes
import packages

STREAM = re.compile(r'[0-9]+(?:\.[0-9]+)*')
# A release, its patch level (_p1), or Wolfi's snapshot of its branch (_git20261010).
RELEASE = re.compile(r'[0-9]+(?:\.[0-9]+)*(?:_p[0-9]+|_git[0-9]{8})?')

def version_key(text):
    return tuple(int(x) for x in text.split('.'))

def latest(pkg, checkout):
    """The path and config of pkg's newest stable recipe in checkout.

    A package follows a FAMILY of streams, or one RECIPE that Wolfi versions
    within (libffi.yaml), whose release is the only candidate."""
    recipe = pkg.hook('RECIPE')
    family = pkg.hook('FAMILY')
    pinned = pkg.policy.get('stream')
    stable = pkg.hook('stable', lambda config: True)
    candidates = []
    paths = [checkout / (recipe + '.yaml')] if recipe else checkout.glob(family + '*.yaml')
    for path in paths:
        if not path.is_file():
            continue
        stream = '0' if recipe else path.stem[len(family):]
        if not STREAM.fullmatch(stream) or (pinned and stream != pinned):
            continue
        config = yaml.safe_load(path.read_text())
        package = config.get('package', {})
        version = str(package.get('version', ''))
        if package.get('name') != path.stem or not RELEASE.fullmatch(version):
            continue
        if not recipe and version != stream and not version.startswith(stream + '.'):
            continue
        if stable(config):
            candidates.append((version_key(stream), path, config))
    if not candidates:
        raise ValueError('No stable Wolfi recipe for ' + pkg.name + (' ' + pinned if pinned else ''))
    _, path, config = max(candidates, key=lambda candidate: candidate[0])
    # The newest must be pinned: never fall back to an older stream silently.
    if not re.fullmatch('[0-9a-f]{40}', str(pkg.hooks.source_commit(config) or '')):
        raise ValueError('Latest ' + pkg.name + ' recipe ' + path.name + ' has no exact source pin')
    return path, config

def capture(pkg, checkout, destination=None):
    destination = destination or pkg.inputs
    policy = pkg.policy
    revision = policy['revision']
    if type(revision) is not int or revision < 1:
        raise ValueError('Policy revision must be a positive integer')
    pkg.hook('check_policy', lambda policy: None)(policy)
    path, config = latest(pkg, checkout)
    commit = subprocess.check_output(['git', '-C', str(checkout), 'rev-parse', 'HEAD'], text=True).strip()
    # The build's SOURCE_DATE_EPOCH: when these inputs were current in Wolfi.
    # Melange would take the recipe's commit time, which a captured copy lacks.
    date = int(subprocess.check_output(['git', '-C', str(checkout), 'log', '-1', '--format=%ct'], text=True))
    if subprocess.check_output(['git', '-C', str(checkout), 'status', '--porcelain'], text=True).strip():
        raise ValueError('Wolfi checkout has local changes')
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=destination.parent, prefix='inputs-') as directory:
        candidate = Path(directory)
        shutil.copyfile(path, candidate / 'upstream.yaml')
        if (checkout / path.stem).is_dir():
            shutil.copytree(checkout / path.stem, candidate / 'upstream')
        else:
            (candidate / 'upstream').mkdir()
        shutil.copytree(checkout / 'pipelines', candidate / 'pipelines')
        shutil.copyfile(checkout / 'wolfi-signing.rsa.pub', candidate / 'wolfi-signing.rsa.pub')
        lock = {'package': pkg.name, 'repository': 'https://github.com/wolfi-dev/os', 'recipe': path.name,
                'wolfi_commit': commit, 'source_date_epoch': date, 'source_commit': pkg.hooks.source_commit(config),
                'version': str(config['package']['version']), 'epoch': config['package']['epoch'],
                'revision': revision,
                'sha256': {str(p.relative_to(candidate)): sha256(p) for p in sorted(candidate.rglob('*')) if p.is_file()},
                'mode': file_modes(candidate),
                'policy_sha256': policy_hashes(pkg, pkg.root)}
        lock['build_id'] = identity(lock)
        (candidate / 'upstream.lock.json').write_text(json.dumps(lock, indent=2) + '\n')
        if destination.exists():
            shutil.rmtree(destination)
        shutil.copytree(candidate, destination)
    return lock

def clone(directory):
    checkout = Path(directory) / 'os'
    subprocess.run(['git', 'clone', '--depth=1', 'https://github.com/wolfi-dev/os.git', str(checkout)], check=True)
    return checkout

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--wolfi', type=Path, help='clean existing Wolfi checkout for offline use')
    parser.add_argument('package', nargs='*')
    args = parser.parse_args()
    failed = []
    with tempfile.TemporaryDirectory(prefix='neutered-wolfi-') as directory:
        checkout = args.wolfi or clone(directory)
        for name in args.package or packages.names():
            try:
                lock = capture(packages.Package(name), checkout)
            except Exception as error:  # each package is captured, or reported, alone
                print(name + ': ' + type(error).__name__ + ': ' + str(error), file=sys.stderr)
                failed.append(name)
                continue
            print('{}: Wolfi {}-r{}, build {}'.format(name, lock['version'], lock['epoch'], lock['build_id']))
    if failed:
        raise SystemExit('refresh failed for ' + ', '.join(failed))

if __name__ == '__main__':
    main()
