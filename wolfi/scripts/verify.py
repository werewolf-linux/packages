#!/usr/bin/env python3
"""Verify a package's provenance, and that its derivation relaxes nothing.

usage: verify.py PACKAGE"""
import argparse
import re
import sys
import yaml
from derive import derive
from inputs import verify
import packages
import patching

HEADERS = ('Subject:', 'Upstream-Status:', 'Reference:')

def series(pkg):
    """Assert patches/series lists every patch once, numbered in order, each with its header.

    It may be empty: a package whose policy is its recipe's build options alone."""
    names = (pkg.dir / 'patches/series').read_text().split()
    assert len(names) == len(set(names)), 'patches/series lists each patch once'
    assert set(names) == {p.name for p in (pkg.dir / 'patches').glob('*.patch')}, 'patches/series and patches/ differ'
    for number, name in enumerate(names, 1):
        assert re.fullmatch(r'%04d-[a-z0-9-]+\.patch' % number, name), name
        header = (pkg.dir / 'patches' / name).read_text().split('\ndiff --git', 1)[0]
        for field in HEADERS:
            assert field in header, name + ' has no ' + field

def check(pkg, upstream, derived):
    """Raise AssertionError unless derived keeps what every build needs."""
    fetches = [x for x in derived['pipeline'] if x.get('name') == 'Fetch the locked source commit']
    assert len(fetches) == 1 and 'git' in fetches[0]['needs']['packages'], 'one source fetch, with Git'
    assert not [x for x in derived['pipeline'] if x.get('uses') == 'git-checkout'], "Wolfi's checkout replaced"
    assert derived['update'] == {'enabled': False}, "Wolfi's update automation stays off"
    ours = [x for x in derived['pipeline'] if x.get('name') == 'Apply Werewolf policy patches']
    patched = bool((pkg.dir / 'patches/series').read_text().split())
    assert ours == ([patching.step(pkg.hooks.PATCHES)] if patched else []), \
        'patches apply by the shared step, once, if there are any'
    series(pkg)
    pkg.hooks.check(pkg, upstream, derived)

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('package')
    pkg = packages.Package(parser.parse_args().package)
    lock = verify(pkg)
    assert re.fullmatch('[0-9a-f]{40}', lock['wolfi_commit'])
    assert re.fullmatch('[0-9a-f]{40}', lock['source_commit'])
    check(pkg, yaml.safe_load((pkg.inputs / 'upstream.yaml').read_text()), derive(pkg))
    print('PASS: ' + pkg.name + ': input hashes, upstream hardening and steps, patch series')

if __name__ == '__main__':
    sys.exit(main())
