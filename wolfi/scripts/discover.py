#!/usr/bin/env python3
"""The scheduled check: what each package needs, from one Wolfi snapshot.

For every package, alone: capture its newest stable Wolfi recipe, verify and
derive it, and apply every patch to the locked source. A step that fails is
filed as an issue (scripts/drift.py) and stops that package only; a package
that passes has its issues closed, and is built when its release is missing
or published when its release is staged.

Each package's inputs go to build/bundles/NAME.tar.gz for the build jobs. In
GitHub Actions, GITHUB_OUTPUT gets build, publish and failed, as JSON lists.

usage: discover.py [--wolfi CHECKOUT] [--local] [PACKAGE...]

--local tells GitHub nothing and asks it nothing: no issues, and every
package that passes is to be built, as for a pull request or a laptop."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import drift
import packages
import publish
import refresh

ROOT = Path(__file__).resolve().parents[1]
STEPS = [('refresh', ['refresh.py', '--wolfi', '{wolfi}', '{name}']),
         ('derivation', ['verify.py', '{name}']),
         ('derivation', ['derive.py', '{name}']),
         ('patches', ['prepare.py', '{name}'])]

def step(pkg, kind, command, wolfi):
    """Run one step for pkg, echoing and keeping its output; return (passed, output)."""
    argv = [sys.executable, str(ROOT / 'scripts' / command[0])] + \
           [a.format(wolfi=wolfi, name=pkg.name) for a in command[1:]]
    print('::group::' + pkg.name + ': ' + ' '.join(argv[1:]), flush=True)
    result = subprocess.run(argv, cwd=ROOT, capture_output=True, text=True)
    out = result.stdout + result.stderr
    print(out, end='' if out.endswith('\n') else '\n', flush=True)
    print('::endgroup::', flush=True)
    log = pkg.build / (kind + '.log')
    log.parent.mkdir(parents=True, exist_ok=True)
    with log.open('a') as kept:
        kept.write(out)
    return result.returncode == 0, out

def check(pkg, wolfi, github):
    """pkg's state this run: 'failed', or its release's: 'build', 'publish', 'complete'."""
    for log in pkg.build.glob('*.log'):
        log.unlink()
    if pkg.source.exists():
        shutil.rmtree(pkg.source)
    output = ''
    for kind, command in STEPS:
        passed, said = step(pkg, kind, command, wolfi)
        output += said
        if not passed:
            if github:
                lock = None
                if kind != 'refresh' and (pkg.inputs / 'upstream.lock.json').is_file():
                    lock = json.loads((pkg.inputs / 'upstream.lock.json').read_text())
                title, body, marker = drift.report(pkg, kind, (pkg.build / (kind + '.log')).read_text(),
                                                   lock, drift.run_url())
                print('drift:', *drift.file(pkg, kind, title, body, marker), '-', title, flush=True)
            return 'failed'
    lock = json.loads((pkg.inputs / 'upstream.lock.json').read_text())
    if github:
        said = "Wolfi's {} {}-r{} builds with these protections again{}.".format(
            pkg.title, lock['version'], lock['epoch'], ': ' + drift.run_url() if drift.run_url() else '')
        drift.resolve(pkg, False, said)
        if drift.fuzzy_patches(output):
            title, body, marker = drift.report(pkg, 'fuzz', output, lock, drift.run_url())
            print('drift:', *drift.file(pkg, 'fuzz', title, body, marker), '-', title, flush=True)
        else:
            drift.resolve(pkg, True, 'Every patch applies exactly again{}.'.format(
                ': ' + drift.run_url() if drift.run_url() else ''))
    bundles = pkg.root / 'build/bundles'
    bundles.mkdir(parents=True, exist_ok=True)
    with tarfile.open(bundles / (pkg.name + '.tar.gz'), 'w:gz') as bundle:
        bundle.add(pkg.inputs, arcname='.')
    if not github:
        return 'build'
    return publish.status(pkg.tag(lock['build_id']))

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--wolfi', type=Path, help='clean existing Wolfi checkout for offline use')
    parser.add_argument('--local', action='store_true', help='tell GitHub nothing and ask it nothing')
    parser.add_argument('package', nargs='*')
    args = parser.parse_args()
    states = {}
    with tempfile.TemporaryDirectory(prefix='neutered-wolfi-') as directory:
        wolfi = args.wolfi or refresh.clone(directory)
        for name in args.package or packages.names():
            states[name] = check(packages.Package(name), str(wolfi), not args.local)
            print(name + ': ' + states[name], flush=True)
    lists = {'build': [n for n, s in states.items() if s == 'build'],
             'publish': [n for n, s in states.items() if s in ('build', 'publish')],
             'failed': [n for n, s in states.items() if s == 'failed']}
    if os.environ.get('GITHUB_OUTPUT'):
        with open(os.environ['GITHUB_OUTPUT'], 'a') as out:
            for key, value in lists.items():
                out.write(key + '=' + json.dumps(value) + '\n')
    print(json.dumps(lists))

if __name__ == '__main__':
    main()
