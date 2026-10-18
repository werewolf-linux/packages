"""Authenticated, immutable APK storage and conditional index updates in R2."""
import base64
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import common

PREFIX = 'apk/packages/'
ENDPOINT = 'https://c860af766754fb92eec27bc723e6db5d.r2.cloudflarestorage.com'


def aws(operation, *args):
    return subprocess.run(['aws', '--endpoint-url', os.environ.get('PACKAGES_R2_ENDPOINT', ENDPOINT),
                           '--region', 'auto', 's3api', operation, '--bucket', os.environ.get('PACKAGES_BUCKET', 'werewolf'),
                           *args], capture_output=True, text=True)


def missing(result):
    return any(code in result.stderr for code in ['(NoSuchKey)', '(404)'])


def raced(result):
    return any(code in result.stderr for code in ['(PreconditionFailed)', '(ConditionalRequestConflict)'])


def fetch(key, path):
    result = aws('get-object', '--key', PREFIX + key, str(path))
    if result.returncode == 0:
        return json.loads(result.stdout)['ETag']
    if missing(result):
        return None
    raise RuntimeError(result.stderr)


def immutable(path, key):
    with tempfile.TemporaryDirectory() as temporary:
        old = Path(temporary) / 'old'
        if fetch(key, old) is not None:
            if common.sha256(old) != common.sha256(path):
                raise ValueError('published object has different bytes; bump the recipe epoch: ' + key)
            return
        result = aws('put-object', '--key', PREFIX + key, '--body', str(path), '--if-none-match', '*',
                     '--cache-control', 'public, max-age=31536000, immutable')
        if result.returncode == 0:
            return
        if raced(result) and fetch(key, old) is not None and common.sha256(old) == common.sha256(path):
            return
        raise RuntimeError(result.stderr)


def entries(path, arch, trust=common.TRUST):
    parts = common.authenticated(path, trust)
    text = common.contents(parts[1][1])['APKINDEX'].decode()
    found = {}
    for stanza in text.strip().split('\n\n') if text.strip() else []:
        fields = dict(line.split(':', 1) for line in stanza.splitlines())
        if fields['A'] != arch or not common.NAME.fullmatch(fields['P']):
            raise ValueError('unexpected package identity in signed index')
        name = fields['P'] + '-' + fields['V'] + '.apk'
        if '/' in name or name in found:
            raise ValueError('duplicate or unsafe indexed filename')
        found[name] = stanza
    return found


def signed_index(found, key):
    import gzip
    import io
    import tarfile
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode='w', format=tarfile.USTAR_FORMAT) as archive:
        for name, data in [('APKINDEX', ''.join(found[n] + '\n\n' for n in sorted(found)).encode()), ('DESCRIPTION', b'')]:
            item = tarfile.TarInfo(name)
            item.size, item.mode = len(data), 0o644
            archive.addfile(item, io.BytesIO(data))
    control = gzip.compress(buffer.getvalue(), mtime=0)
    return common.signed_member(common.sign(control, key)) + control


def merge_index(current, arch, key, directory, trust=common.TRUST):
    destination = arch + '/APKINDEX.tar.gz'
    previous, result_path = directory / 'previous-index', directory / 'next-index'
    for attempt in range(12):
        etag = fetch(destination, previous)
        merged = entries(previous, arch, trust) if etag is not None else {}
        for name, stanza in current.items():
            if name in merged and merged[name] != stanza:
                raise ValueError('published package identity changed; bump the recipe epoch: ' + name)
            merged[name] = stanza
        result_path.write_bytes(signed_index(merged, key))
        entries(result_path, arch, trust)
        condition = ['--if-match', etag] if etag is not None else ['--if-none-match', '*']
        result = aws('put-object', '--key', PREFIX + destination, '--body', str(result_path),
                     '--cache-control', 'no-cache', *condition)
        if result.returncode == 0:
            return
        if not raced(result):
            raise RuntimeError(result.stderr)
        time.sleep(min(attempt + 1, 5))
    raise RuntimeError('package index kept changing')


def envelope(manifest, key):
    payload = common.encode(manifest)
    return common.encode(dict(format='werewolf-package-release/1', payload=base64.b64encode(payload).decode(),
                              signature=base64.b64encode(common.sign(payload, key)).decode()))


def open_envelope(data, trust=common.TRUST):
    value = json.loads(data)
    if value['format'] != 'werewolf-package-release/1':
        raise ValueError('unknown release manifest format')
    payload, signature = (base64.b64decode(value[k], validate=True) for k in ['payload', 'signature'])
    common.verify(payload, signature, trust)
    return json.loads(payload)


def advance(manifest, signed, directory, trust=common.TRUST):
    remote = 'contexts/' + manifest['context'] + '/current.json'
    previous, next_path = directory / 'previous-release', directory / 'next-release'
    next_path.write_bytes(signed)
    for attempt in range(12):
        etag = fetch(remote, previous)
        if etag is not None:
            old = open_envelope(previous.read_bytes(), trust)
            if old['run_number'] >= manifest['run_number']:
                return
        condition = ['--if-match', etag] if etag is not None else ['--if-none-match', '*']
        result = aws('put-object', '--key', PREFIX + remote, '--body', str(next_path),
                     '--cache-control', 'no-cache', '--content-type', 'application/json', *condition)
        if result.returncode == 0:
            return
        if not raced(result):
            raise RuntimeError(result.stderr)
        time.sleep(min(attempt + 1, 5))
    raise RuntimeError('release pointer kept changing')
