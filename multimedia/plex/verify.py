#!/usr/bin/env python3
"""Verify the captured Plex repository signature and every recipe input pin.

No network. Optional --deb FILE also verifies a downloaded vendor package.
Run this before accepting a version update, together with a package build.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent
PRIMARY = '6EFFEB478A6559D75C7C4FE706C521790B9CFFDE'


def verify(directory=ROOT):
    sources = json.loads((directory / 'sources.json').read_text())
    with tempfile.TemporaryDirectory(prefix='plex-provenance-') as temporary:
        base = ['gpg', '--homedir', temporary, '--batch']
        subprocess.run(base + ['--import', str(directory / 'PlexSign.v2.key')],
                       check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        keys = subprocess.check_output(base + ['--with-colons', '--fingerprint'], text=True)
        fingerprints = [line.split(':')[9] for line in keys.splitlines() if line.startswith('fpr:')]
        if sum(line.startswith('pub:') for line in keys.splitlines()) != 1 or not fingerprints or fingerprints[0] != PRIMARY:
            raise ValueError('Unexpected Plex signing key')
        release = subprocess.check_output(base + ['--decrypt', str(directory / 'InRelease')],
                                          stderr=subprocess.PIPE, text=True)
    section = release.split('\nSHA256:\n', 1)[1]
    hashes = {}
    for line in section.splitlines():
        if not line.startswith(' '):
            break
        digest, size, name = line.split()
        hashes[name] = (digest, int(size))
    recipe = (directory / 'plex.yaml').read_text()
    for arch, deb in (('aarch64', 'arm64'), ('x86_64', 'amd64')):
        raw = (directory / ('Packages.' + deb)).read_bytes()
        expected = hashes['main/binary-' + deb + '/Packages']
        if (hashlib.sha256(raw).hexdigest(), len(raw)) != expected:
            raise ValueError('Unsigned or altered package index: ' + arch)
        fields = dict(line.split(': ', 1) for line in raw.decode().splitlines() if ': ' in line)
        source = sources['architectures'][arch]
        if fields['Version'] != sources['version'] or source['deb'] != {
                'url': 'https://repo.plex.tv/deb/' + fields['Filename'], 'sha256': fields['SHA256']}:
            raise ValueError('Vendor package identity changed: ' + arch)
        entries = [(source['deb']['url'], source['deb']['sha256'])]
        for codec in source['codecs']:
            expected_url = ('https://downloads.plex.tv/codecs/' + sources['codec_set'] +
                            '/linux-' + arch + '-standard/' + codec['fileName'])
            if codec['url'] != expected_url or not re.fullmatch(r'lib[a-z0-9_]+\.so', codec['fileName']):
                raise ValueError('Unsafe codec input')
            entries.append((codec['url'], codec['fileSha256']))
        for url, digest in entries:
            if not re.fullmatch('[0-9a-f]{64}', digest) or recipe.count('uri: ' + url + '\n') != 1:
                raise ValueError('Missing or duplicate recipe source: ' + url)
            if 'uri: ' + url + '\n      expected-sha256: ' + digest + '\n' not in recipe:
                raise ValueError('Recipe checksum differs: ' + url)
    return sources


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--deb', type=Path)
    args = parser.parse_args()
    sources = verify()
    if args.deb:
        digest = hashlib.sha256()
        with args.deb.open('rb') as source:
            for chunk in iter(lambda: source.read(1 << 20), b''):
                digest.update(chunk)
        if digest.hexdigest() not in {a['deb']['sha256'] for a in sources['architectures'].values()}:
            raise SystemExit('Vendor DEB differs from the signed package index')
    print('Plex provenance: signature, both architectures and all codec recipe pins verified')


if __name__ == '__main__':
    main()
