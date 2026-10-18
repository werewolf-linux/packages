#!/usr/bin/env python3
"""Sign validated native artifacts, upload immutable snapshots, then merge the feed."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import common
import repository


def validate(directory, meta, arch, commit, run_id):
    record = json.loads((directory / 'build.json').read_text())
    expected = {name + '-' + meta['version'] + '.apk' for name in meta['names']}
    if record['format'] != 'werewolf-package-build/1' or record['arch'] != arch:
        raise ValueError('unexpected artifact format or architecture')
    if any(record[k] != meta[k] for k in ['context', 'inputs', 'names', 'version']):
        raise ValueError('artifact build inputs differ from this checkout')
    if record['commit'] != commit or record['run_id'] != run_id:
        raise ValueError('artifact came from another commit or workflow run')
    if set(record['sha256']) != expected or {p.name for p in directory.iterdir()} != expected | {'build.json'}:
        raise ValueError('missing or unexpected artifact files')
    for name in meta['names']:
        path = directory / (name + '-' + meta['version'] + '.apk')
        if path.is_symlink() or common.sha256(path) != record['sha256'][path.name]:
            raise ValueError('artifact hash mismatch')
        common.apk(path, name, meta['version'], arch)
    return record


def signing_key():
    secret = os.environ.pop('PACKAGES_APK_SIGNING_KEY', '')
    if not secret:
        raise ValueError('release environment needs WEREWOLF_PACKAGES_KEY')
    directory = common.ROOT / 'build/keys'
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    key = directory / 'werewolf-packages.rsa'
    fd = os.open(key, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w') as output:
        subprocess.run(['openssl', 'rsa', '-traditional', '-passin', 'pass:'], input=secret,
                       text=True, stdout=output, stderr=subprocess.PIPE, check=True)
    key.chmod(0o600)
    public = subprocess.check_output(['openssl', 'pkey', '-in', str(key), '-pubout'], stderr=subprocess.PIPE)
    if public != common.TRUST.read_bytes():
        key.unlink()
        raise ValueError('signing key differs from release/packages.pub')
    return key


def signed_apk(source, destination, name, meta, arch, key, trust=common.TRUST):
    control, _ = common.apk(source, name, meta['version'], arch)
    with destination.open('wb') as output, source.open('rb') as data:
        output.write(common.signed_member(common.sign(control, key)))
        shutil.copyfileobj(data, output, length=1 << 20)
    common.apk(destination, name, meta['version'], arch, signed=True, trust=trust)


def verify_index(directory, meta, arch, trust=common.TRUST):
    found = repository.entries(directory / 'APKINDEX.tar.gz', arch, trust)
    expected = {name + '-' + meta['version'] + '.apk' for name in meta['names']}
    if set(found) != expected:
        raise ValueError('signed index does not cover every package')
    for name in meta['names']:
        path = directory / (name + '-' + meta['version'] + '.apk')
        control, _ = common.apk(path, name, meta['version'], arch, signed=True, trust=trust)
        fields = dict(line.split(':', 1) for line in found[path.name].splitlines())
        checksum = 'Q1' + base64.b64encode(hashlib.sha1(control).digest()).decode()
        if fields['C'] != checksum or int(fields['S']) != path.stat().st_size:
            raise ValueError('signed index differs from package bytes: ' + path.name)
    return found


def prepare(meta, artifacts, work, key, trust=common.TRUST):
    commit, run_id = os.environ['GITHUB_SHA'], os.environ['GITHUB_RUN_ID']
    # Validate both architectures before any signing or remote write.
    for arch in common.ARCHES:
        validate(artifacts / arch, meta, arch, commit, run_id)
    manifest = dict(format='werewolf-package-manifest/1', **meta, commit=commit,
                    run_number=int(os.environ['GITHUB_RUN_NUMBER']), run_id=run_id, architectures={})
    for arch in common.ARCHES:
        output = work / arch
        output.mkdir()
        for name in meta['names']:
            filename = name + '-' + meta['version'] + '.apk'
            signed_apk(artifacts / arch / filename, output / filename, name, meta, arch, key, trust)
        subprocess.run([os.environ.get('MELANGE', 'melange'), 'index', '--arch', arch,
                        '--signing-key', str(key), '--output', str(output / 'APKINDEX.tar.gz'),
                        *(str(p) for p in sorted(output.glob('*.apk')))], check=True)
        indexed = verify_index(output, meta, arch, trust)
        expected = {p.name for p in output.glob('*.apk')}
        if set(indexed) != expected:
            raise ValueError('signed index does not cover every package')
        manifest['architectures'][arch] = {p.name: common.sha256(p) for p in sorted(output.iterdir())}
    return manifest


def restore(meta, work, trust=common.TRUST):
    prefix = 'builds/' + meta['context'] + '/' + meta['inputs'] + '/'
    release = work / 'release.json'
    if repository.fetch(prefix + 'release.json', release) is None:
        return None
    manifest = repository.open_envelope(release.read_bytes(), trust)
    if manifest['format'] != 'werewolf-package-manifest/1' or any(manifest[k] != meta[k] for k in ['context', 'inputs', 'names', 'version']):
        raise ValueError('staged release differs from this checkout')
    if set(manifest['architectures']) != set(common.ARCHES):
        raise ValueError('staged release is missing a native architecture')
    for arch in common.ARCHES:
        output = work / arch
        output.mkdir()
        expected = {name + '-' + meta['version'] + '.apk' for name in meta['names']} | {'APKINDEX.tar.gz'}
        if set(manifest['architectures'][arch]) != expected:
            raise ValueError('staged release has unexpected package files')
        for name, digest in manifest['architectures'][arch].items():
            destination = output / name
            if repository.fetch(prefix + arch + '/' + name, destination) is None or common.sha256(destination) != digest:
                raise ValueError('missing or altered staged package: ' + name)
        for name in meta['names']:
            common.apk(output / (name + '-' + meta['version'] + '.apk'), name, meta['version'], arch, signed=True, trust=trust)
        if set(verify_index(output, meta, arch, trust)) != expected - {'APKINDEX.tar.gz'}:
            raise ValueError('staged index differs from package coverage')
    return manifest


def publish(meta, artifacts, key, trust=common.TRUST):
    with tempfile.TemporaryDirectory(prefix='packages-publish-') as temporary:
        work = Path(temporary)
        manifest = restore(meta, work, trust)
        if manifest is None:
            manifest = prepare(meta, artifacts, work, key, trust)
            release = work / 'release.json'
            release.write_bytes(repository.envelope(manifest, key))
            prefix = 'builds/' + meta['context'] + '/' + meta['inputs'] + '/'
            for arch in common.ARCHES:
                for path in [*sorted((work / arch).glob('*.apk')), work / arch / 'APKINDEX.tar.gz']:
                    repository.immutable(path, prefix + arch + '/' + path.name)
            # The signed snapshot is a recovery point before any mutable feed update.
            repository.immutable(release, prefix + 'release.json')
        for arch in common.ARCHES:
            directory = work / arch
            for apk in sorted(directory.glob('*.apk')):
                repository.immutable(apk, arch + '/' + apk.name)
            repository.merge_index(verify_index(directory, meta, arch, trust), arch, key, directory, trust)
        # Both native architectures must be indexed before advancing the release.
        repository.advance(manifest, (work / 'release.json').read_bytes(), work, trust)
    print(meta['context'] + ': published ' + meta['version'] + ' to ' + common.PUBLIC)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('package')
    parser.add_argument('--artifacts', type=Path, default=Path('artifacts'))
    args = parser.parse_args()
    meta = common.metadata(args.package)
    key = signing_key()
    try:
        publish(meta, args.artifacts, key)
    finally:
        key.unlink(missing_ok=True)


if __name__ == '__main__':
    main()
