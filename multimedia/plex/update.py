#!/usr/bin/env python3
"""Refresh Plex from its signed APT repository, retaining a verified codec ABI.

The caller runs this in a scratch context. A changed codec ABI stops the update
for review, as a failed source patch does for Wolfi-derived packages.
"""
import argparse
import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import urllib.request

import verify

ROOT = Path(__file__).resolve().parent
REPOSITORY = 'https://repo.plex.tv/deb/'


def download(url, destination, limit=1 << 30):
    if not url.startswith('https://repo.plex.tv/deb/'):
        raise ValueError('unexpected vendor download host')
    request = urllib.request.Request(url, headers={'User-Agent': 'werewolf-packages-update/1'})
    digest, size = hashlib.sha256(), 0
    with urllib.request.urlopen(request, timeout=120) as source, destination.open('wb') as target:
        if not source.url.startswith('https://repo.plex.tv/deb/'):
            raise ValueError('unexpected vendor redirect')
        while True:
            block = source.read(1 << 20)
            if not block:
                break
            size += len(block)
            if size > limit:
                raise ValueError('oversized vendor artifact')
            digest.update(block)
            target.write(block)
    return digest.hexdigest()


def signed_release(directory):
    with tempfile.TemporaryDirectory(prefix='plex-key-') as temporary:
        base = ['gpg', '--homedir', temporary, '--batch']
        subprocess.run(base + ['--import', str(directory / 'PlexSign.v2.key')], check=True, capture_output=True)
        keys = subprocess.check_output(base + ['--with-colons', '--fingerprint'], text=True)
        fingerprints = [line.split(':')[9] for line in keys.splitlines() if line.startswith('fpr:')]
        if not fingerprints or fingerprints[0] != verify.PRIMARY:
            raise ValueError('unexpected Plex signing key')
        return subprocess.check_output(base + ['--decrypt', str(directory / 'InRelease')], stderr=subprocess.PIPE, text=True)


def package_fields(raw):
    for stanza in raw.decode().strip().split('\n\n'):
        fields = dict(line.split(': ', 1) for line in stanza.splitlines() if ': ' in line)
        if fields.get('Package') == 'plexmediaserver':
            if not re.fullmatch(r'pool/main/p/plexmediaserver/[A-Za-z0-9_.+-]+\.deb', fields.get('Filename', '')):
                raise ValueError('unsafe vendor filename')
            if not re.fullmatch(r'[0-9a-f]{64}', fields.get('SHA256', '')):
                raise ValueError('missing vendor checksum')
            return fields
    raise ValueError('Plex package absent from signed index')


def check_codec_abi(deb, codec_set):
    # ar emits only the requested member; tar reads the archive without writing
    # any vendor path or invoking package maintainer scripts.
    members = subprocess.check_output(['ar', 't', str(deb)], text=True).splitlines()
    payloads = [name for name in members if name in ('data.tar.xz', 'data.tar.gz', 'data.tar.zst')]
    if len(payloads) != 1 or payloads[0].endswith('.zst'):
        raise ValueError('review changed vendor archive compression')
    data = subprocess.check_output(['ar', 'p', str(deb), payloads[0]])
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:*') as archive:
        for member in archive:
            if member.name.removeprefix('./') == 'usr/lib/plexmediaserver/Plex Transcoder' and member.isfile():
                transcoder = archive.extractfile(member).read()
                if codec_set.encode() not in transcoder:
                    raise ValueError('Plex codec ABI changed or cannot be verified; refresh and review codec manifests before publishing')
                return
    raise ValueError('Plex Transcoder absent from vendor archive')


def refresh(directory, expected_version, fetch=download, codec_check=check_codec_abi):
    old = verify.verify(directory)
    fetch(REPOSITORY + 'dists/public/InRelease', directory / 'InRelease', 8 << 20)
    release = signed_release(directory)
    hashes = {}
    for line in release.split('\nSHA256:\n', 1)[1].splitlines():
        if not line.startswith(' '):
            break
        digest, size, name = line.split()
        hashes[name] = (digest, int(size))
    sources = json.loads(json.dumps(old))
    for arch, apt in (('aarch64', 'arm64'), ('x86_64', 'amd64')):
        name = 'main/binary-' + apt + '/Packages'
        path = directory / ('Packages.' + apt)
        digest = fetch(REPOSITORY + 'dists/public/' + name, path, 8 << 20)
        if (digest, path.stat().st_size) != hashes[name]:
            raise ValueError('vendor index does not match signed release')
        fields = package_fields(path.read_bytes())
        if fields['Version'] != expected_version:
            raise ValueError('architectures do not match the discovered release')
        source = {'url': REPOSITORY + fields['Filename'], 'sha256': fields['SHA256']}
        with tempfile.TemporaryDirectory(prefix='plex-deb-') as temporary:
            deb = Path(temporary) / 'plex.deb'
            if fetch(source['url'], deb) != source['sha256']:
                raise ValueError('vendor DEB checksum mismatch')
            codec_check(deb, old['codec_set'])
        sources['architectures'][arch]['deb'] = source
    if not re.fullmatch(r'[0-9]+(?:\.[0-9]+)+-[0-9a-f]+', expected_version):
        raise ValueError('invalid vendor release version')
    sources['version'] = expected_version
    recipe = (directory / 'plex.yaml').read_text()
    recipe = recipe.replace(old['version'], expected_version)
    for arch in sources['architectures']:
        before, after = old['architectures'][arch]['deb'], sources['architectures'][arch]['deb']
        recipe = recipe.replace(before['url'], after['url']).replace(before['sha256'], after['sha256'])
    version = expected_version.split('-', 1)[0]
    previous = old['version'].split('-', 1)[0]
    recipe = re.sub(r'(?m)^  version: .*$', '  version: ' + version, recipe, count=1)
    epoch = re.search(r'(?m)^  epoch: ([0-9]+)$', recipe)
    revision = int(epoch[1]) + 1 if version == previous else 0
    recipe = re.sub(r'(?m)^  epoch: [0-9]+$', '  epoch: ' + str(revision), recipe, count=1)
    (directory / 'sources.json').write_text(json.dumps(sources, indent=2) + '\n')
    (directory / 'plex.yaml').write_text(recipe)
    verify.verify(directory)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--expected-version', required=True)
    args = parser.parse_args()
    refresh(ROOT, args.expected_version)
