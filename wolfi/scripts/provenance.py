#!/usr/bin/env python3
"""Record a successful native build; this runs only after all package tests.

usage: provenance.py PACKAGE --arch ARCH"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
from inputs import ROOT, verify, sha256
import packages

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('package')
    parser.add_argument('--arch', choices=['x86_64', 'aarch64'], required=True)
    args = parser.parse_args()
    pkg = packages.Package(args.package)
    lock = verify(pkg)
    out = pkg.packages
    if not (out / args.arch / 'APKINDEX.tar.gz').is_file():
        raise ValueError('Missing signed package index')
    shutil.copyfile(pkg.inputs / 'upstream.lock.json', out / 'upstream.lock.json')
    for path in [ROOT / 'tools.lock.json', pkg.dir / 'policy.json', pkg.recipe, ROOT / 'release/packages.pub']:
        shutil.copyfile(path, out / path.name)
    shutil.copyfile(ROOT / 'build/keys/werewolf-packages.rsa.pub', out / 'werewolf-packages.rsa.pub')
    shutil.copytree(pkg.dir / 'patches', out / 'patches')
    subprocess.run(['tar', '-czf', str(out / (pkg.name + '-source.tar.gz')),
                    '--exclude=.git', '-C', str(pkg.source), '.'], check=True)
    subprocess.run(['tar', '-czf', str(out / 'wolfi-inputs.tar.gz'), '-C', str(pkg.inputs), '.'], check=True)
    record = {'package': pkg.name, 'build_id': lock['build_id'], 'arch': args.arch,
              'source_sha': os.environ['GITHUB_SHA'], 'run_id': os.environ['GITHUB_RUN_ID'],
              'version': pkg.hooks.full_version(lock),
              'sha256': {str(p.relative_to(out)): sha256(p) for p in sorted(out.rglob('*')) if p.is_file()}}
    (out / 'build.json').write_text(json.dumps(record, indent=2) + '\n')

if __name__ == '__main__':
    main()
