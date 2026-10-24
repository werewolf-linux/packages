#!/usr/bin/env python3
"""Merge already validated package repositories into the signed hardened feed.

Normal publication includes its package before completing the GitHub release.
Running this separately backfills existing releases without rebuilding them,
and repairs interrupted publication. It never needs another package to build.

usage: feed.py [PACKAGE ...]    every published package by default
"""
import argparse
import base64
import hashlib
from pathlib import Path
import re
import tempfile
from types import SimpleNamespace
import packages
import publish
from inputs import ROOT

REPOSITORY = SimpleNamespace(prefix='apk/hardened/', public=publish.HARDENED)


def verify_apk(path, stanza, arch, trust):
    """Verify the payload and the signed source index's identity and checksum."""
    fields = dict(line.split(':', 1) for line in stanza.splitlines())
    parts = publish.authenticated(path, trust)
    if len(parts) != 3:
        raise ValueError('Missing APK payload: ' + path.name)
    checksum = 'Q1' + base64.b64encode(hashlib.sha1(parts[1][0]).digest()).decode()
    info = publish.contents(parts[1][1])['.PKGINFO'].decode()
    meta = dict(line.split(' = ', 1) for line in info.splitlines() if ' = ' in line)
    if (fields['A'] != arch or checksum != fields['C'] or int(fields['S']) != path.stat().st_size or
            (meta['pkgname'], meta['pkgver'], meta['arch']) != (fields['P'], fields['V'], arch) or
            meta['datahash'] != hashlib.sha256(parts[2][0]).hexdigest()):
        raise ValueError('APK differs from its signed source index: ' + path.name)


def include(pkg, key=ROOT / 'build/keys/werewolf-packages.rsa', trust=ROOT / 'release/packages.pub'):
    """Include a package's signed stable history, with objects before each index."""
    with tempfile.TemporaryDirectory(prefix='hardened-feed-') as directory:
        work = Path(directory)
        sources = {}
        for arch in publish.ARCHES:
            source = work / (arch + '-source.tar.gz')
            if publish.fetch(pkg, arch + '/APKINDEX.tar.gz', source):
                sources[arch] = publish.entries(publish.authenticated(source, trust))
        if not sources:
            return
        if set(sources) != set(publish.ARCHES):
            raise ValueError(pkg.name + ': published packages are missing an architecture')
        for arch, current in sources.items():
            previous = work / (arch + '-combined.tar.gz')
            existing = (publish.entries(publish.authenticated(previous, trust))
                        if publish.fetch(REPOSITORY, arch + '/APKINDEX.tar.gz', previous) else {})
            _, added = publish.merge(existing, current)
            for name in added:
                fields = dict(line.split(':', 1) for line in current[name].splitlines())
                if (not re.fullmatch(r'[a-zA-Z0-9_.+-]+', name) or
                        name != fields['P'] + '-' + fields['V'] + '.apk'):
                    raise ValueError('Unsafe package filename: ' + name)
                apk = work / name
                if not publish.fetch(pkg, arch + '/' + name, apk):
                    raise ValueError('Published index names a missing APK: ' + name)
                verify_apk(apk, current[name], arch, trust)
                publish.upload(REPOSITORY, apk, arch + '/' + name)
                apk.unlink()
            publish.merge_index(REPOSITORY, current, arch, key, work, trust)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('package', nargs='*')
    args = parser.parse_args()
    failed = []
    for name in args.package or packages.names():
        try:
            include(packages.Package(name))
            print(name + ': included in ' + publish.HARDENED, flush=True)
        except Exception as error:
            # A broken or unavailable package never holds back another feed update.
            failed.append(name)
            print(name + ': ' + type(error).__name__ + ': ' + str(error), flush=True)
    if failed:
        raise SystemExit('Feed updates failed for ' + ', '.join(failed))


if __name__ == '__main__':
    main()
