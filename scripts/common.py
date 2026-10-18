"""Recipe identities and strict APK validation shared by builders and publishers."""
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import zlib

ROOT = Path(__file__).resolve().parents[1]
ARCHES = ('x86_64', 'aarch64')
NAME = re.compile(r'[a-z0-9][a-z0-9+_.-]*\Z')
KEYNAME = 'werewolf-packages.rsa.pub'
TRUST = ROOT / 'release/packages.pub'
PUBLIC = 'https://dist.werewolf-linux.org/apk/packages/'


def encode(value):
    return (json.dumps(value, sort_keys=True, separators=(',', ':')) + '\n').encode()


def sha256(path):
    value = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            value.update(block)
    return value.hexdigest()


def contexts():
    return sorted(p.name for p in ROOT.iterdir() if p.is_dir() and NAME.fullmatch(p.name) and list(p.glob('*.yaml')))


def context(name):
    if not NAME.fullmatch(name) or name not in contexts():
        raise ValueError('unknown package context: ' + name)
    recipes = list((ROOT / name).glob('*.yaml'))
    if len(recipes) != 1:
        raise ValueError('one recipe per context is required: ' + name)
    return recipes[0]


def input_digest(name):
    recipe = context(name)
    files = list(recipe.parent.rglob('*')) + [ROOT / 'tools.lock.json', ROOT / 'scripts/build.py', ROOT / 'scripts/common.py']
    records = []
    for path in sorted(files):
        if any(part in {'.git', '__pycache__'} for part in path.relative_to(ROOT).parts):
            continue
        if path.is_symlink():
            raise ValueError('build contexts must not contain symlinks: ' + str(path))
        if path.is_file():
            records.append(dict(path=str(path.relative_to(ROOT)), sha256=sha256(path), mode=path.stat().st_mode & 0o7777))
    return hashlib.sha256(encode(records)).hexdigest()


def metadata(name):
    recipe = context(name)
    template = '{"name":{{printf "%q" .Package.Name}},"version":{{printf "%q" .Package.Version}},"epoch":{{.Package.Epoch}},"subpackages":[' + \
        '{{range $i,$s := .Subpackages}}{{if $i}},{{end}}{{printf "%q" $s.Name}}{{end}}]}'
    found = json.loads(subprocess.check_output([os.environ.get('MELANGE', 'melange'), 'query', str(recipe), template], text=True))
    if not NAME.fullmatch(found['name']) or not re.fullmatch(r'[a-zA-Z0-9_.+~-]+', found['version']):
        raise ValueError('unsafe APK name or version')
    if name.endswith('-vuln-demo') and found['name'] != name:
        raise ValueError('vulnerable APKs must use the context name: ' + name)
    names = [found['name']]
    for sub in found['subpackages']:
        sub = sub.replace('${{package.name}}', found['name']).replace('${{package.version}}', found['version'])
        if not NAME.fullmatch(sub):
            raise ValueError('unresolved subpackage name: ' + sub)
        names.append(sub)
    if len(set(names)) != len(names):
        raise ValueError('duplicate package names')
    return dict(context=name, names=sorted(names), version=found['version'] + '-r' + str(found['epoch']), inputs=input_digest(name))


def members(data):
    parts = []
    while data:
        stream = zlib.decompressobj(31)
        raw = stream.decompress(data)
        if not stream.eof:
            raise ValueError('truncated gzip member')
        size = len(data) - len(stream.unused_data)
        parts.append((data[:size], raw))
        data = stream.unused_data
    return parts


def contents(raw):
    with tarfile.open(fileobj=io.BytesIO(raw)) as archive:
        found = {}
        for item in archive:
            if not item.isfile():
                continue
            name = item.name.removeprefix('./')
            if name in found:
                raise ValueError('duplicate control member: ' + name)
            found[name] = archive.extractfile(item).read()
        return found


def signed_member(signature):
    item = tarfile.TarInfo('.SIGN.RSA256.' + KEYNAME)
    item.size, item.mode, item.uname, item.gname = len(signature), 0o644, 'root', 'root'
    # APK signature members have no tar end marker; control and data follow.
    return gzip.compress(item.tobuf(tarfile.USTAR_FORMAT) + signature + b'\0' * (-len(signature) % 512), mtime=0)


def sign(data, key):
    return subprocess.run(['openssl', 'dgst', '-sha256', '-sign', str(key)], input=data, stdout=subprocess.PIPE, check=True).stdout


def verify(data, signature, trust=TRUST):
    import tempfile
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / 'signature'
        path.write_bytes(signature)
        subprocess.run(['openssl', 'dgst', '-sha256', '-verify', str(trust), '-signature', str(path)],
                       input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True)


def authenticated(path, trust=TRUST):
    parts = members(Path(path).read_bytes())
    if len(parts) not in (2, 3):
        raise ValueError('unsigned or malformed APK/index')
    signature = contents(parts[0][1])
    if set(signature) != {'.SIGN.RSA256.' + KEYNAME}:
        raise ValueError('unexpected APK signing key')
    verify(parts[1][0], next(iter(signature.values())), trust)
    return parts


def read_member(stream, limit=16 << 20):
    """Read one small gzip control member, leaving a large payload on disk."""
    decoder = zlib.decompressobj(31)
    compressed, raw, size = [], [], 0
    while not decoder.eof:
        chunk = stream.read(64 << 10)
        if not chunk:
            raise ValueError('truncated gzip control member')
        value = decoder.decompress(chunk, limit - size + 1)
        size += len(value)
        if size > limit or decoder.unconsumed_tail:
            raise ValueError('oversized gzip control member')
        raw.append(value)
        used = len(chunk) - len(decoder.unused_data)
        compressed.append(chunk[:used])
        if decoder.unused_data:
            stream.seek(-len(decoder.unused_data), 1)
    return b''.join(compressed), b''.join(raw)


def apk(path, name, version, arch, signed=False, trust=TRUST):
    with Path(path).open('rb') as stream:
        signature = None
        if signed:
            signature = contents(read_member(stream)[1])
            if set(signature) != {'.SIGN.RSA256.' + KEYNAME}:
                raise ValueError('unexpected APK signing key')
        control, raw = read_member(stream)
        if signature is not None:
            verify(control, next(iter(signature.values())), trust)
        info = contents(raw)['.PKGINFO'].decode()
        fields = dict(line.split(' = ', 1) for line in info.splitlines() if ' = ' in line)
        if (fields.get('pkgname'), fields.get('pkgver'), fields.get('arch')) != (name, version, arch):
            raise ValueError('APK metadata differs from the expected package: ' + str(path))
        offset = stream.tell()
        if stream.read(2) != b'\x1f\x8b':
            raise ValueError('missing APK payload')
        stream.seek(offset)
        digest = hashlib.sha256()
        for block in iter(lambda: stream.read(1 << 20), b''):
            digest.update(block)
        value = digest.hexdigest()
        if fields.get('datahash') != value:
            raise ValueError('APK payload hash mismatch: ' + str(path))
    return control, offset
