#!/usr/bin/env python3
"""Check out a package's locked source and apply Wolfi's patches, then ours.

Patches apply as Wolfi applies its own (scripts/patching.py). One of ours that
needs fuzz is named "applied with fuzz"; one that will not apply stops here,
naming itself and patch's reason. scripts/drift.py files either as an issue.

usage: prepare.py PACKAGE [DESTINATION] [--source LOCAL-REPOSITORY]"""
import argparse
import json
from pathlib import Path
import yaml
import patching
from source import checkout
import packages

def upstream_patches(config):
    """Wolfi's patches, in the order its recipe applies them."""
    names = []
    for step in config['pipeline']:
        if step.get('uses') == 'patch':
            names.extend(step['with']['patches'].split())
    return names

def apply(destination, patch, label, ours=True):
    try:
        fuzzed = patching.apply(destination, patch)
    except patching.PatchError as error:
        raise SystemExit('prepare: ' + label + ' does not apply\n' + str(error))
    hunks = ', '.join('hunk {} at line {} (fuzz {})'.format(*f) for f in fuzzed)
    if fuzzed and ours:
        print('prepare: ' + label + ' applied with fuzz: ' + hunks, flush=True)
    elif fuzzed:
        print('prepare: applied ' + label + ", as Wolfi does, with fuzz: " + hunks, flush=True)
    else:
        print('prepare: applied ' + label, flush=True)

def prepare(pkg, destination, sources=None):
    if destination.exists():
        raise ValueError('Use a new destination; never reset an existing checkout')
    lock = json.loads((pkg.inputs / 'upstream.lock.json').read_text())
    print('prepare: {} {}-r{} from Wolfi {}, source {}'.format(
        pkg.name, lock['version'], lock['epoch'], lock['wolfi_commit'], lock['source_commit']), flush=True)
    checkout(destination, lock['source_commit'], sources or pkg.sources(lock))
    config = yaml.safe_load((pkg.inputs / 'upstream.yaml').read_text())
    for name in upstream_patches(config):
        apply(destination, pkg.inputs / 'upstream' / name, "Wolfi's " + name, ours=False)
    for name in (pkg.dir / 'patches/series').read_text().split():
        apply(destination, pkg.dir / 'patches' / name, 'patches/' + name)
    pkg.hook('prepared', lambda pkg, destination: None)(pkg, destination)
    print('Source prepared at', destination)

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('package')
    parser.add_argument('destination', nargs='?', type=Path)
    parser.add_argument('--source', type=Path, help='local git repository for offline validation')
    args = parser.parse_args()
    pkg = packages.Package(args.package)
    prepare(pkg, args.destination or pkg.source, (str(args.source),) if args.source else None)

if __name__ == '__main__':
    main()
