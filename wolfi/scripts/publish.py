#!/usr/bin/env python3
"""Publish a package's validated native artifacts to R2 and GitHub.

status says whether this build's release is missing, staged or complete;
stage validates both architectures and stages a draft release with its
notes; publish uploads immutable packages and snapshots, merges the signed
package and combined hardened indexes, and makes the release public last.
Package snapshots remain under apk/NAME/; consumers use apk/hardened/.

usage: publish.py status|stage|publish PACKAGE"""
import argparse
import datetime
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import urllib.parse
import zlib
import yaml
from derive import derive
from inputs import ROOT, verify, sha256
import notes as generic_notes
import packages
import releases

ARCHES = ('x86_64', 'aarch64')
ENDPOINT = 'https://c860af766754fb92eec27bc723e6db5d.r2.cloudflarestorage.com'
BUCKET = 'werewolf'
AGENT = 'werewolf-packages-publish (+https://github.com/werewolf-linux/packages)'
HARDENED = packages.PUBLIC + 'apk/hardened/'

def run(*args):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, text=True).stdout

def notes_of(pkg):
    """The package's release notes module: its own, or the shared one."""
    return pkg.hook('notes', generic_notes)

def note_files(pkg):
    """Release note files by media type; the JSON goes last: a draft with it has all."""
    files = {'release-notes.md': 'text/markdown; charset=utf-8'}
    if hasattr(notes_of(pkg), 'page'):
        files['release-notes.html'] = 'text/html; charset=utf-8'
    files['release-notes.json'] = 'application/json'
    return files

def release(tag):
    # The tag endpoint excludes drafts. Listing with push access includes them.
    result = subprocess.run(['gh', 'api', '--paginate', '--slurp',
                             'repos/' + os.environ['GITHUB_REPOSITORY'] + '/releases?per_page=100'],
                            capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError(result.stderr)
    matches = [item for page in json.loads(result.stdout) for item in page if item['tag_name'] == tag]
    if len(matches) > 1:
        raise ValueError('Multiple releases use the same build tag')
    return matches[0] if matches else None

def release_target(tag):
    # target_commitish only creates a missing tag. For an existing tag, keep
    # its verified source and omit this redundant field: GitHub otherwise
    # checks historical workflow files against main and rejects Actions tokens.
    source = os.environ['GITHUB_SHA']
    try:
        ref = json.loads(run('gh', 'api', 'repos/' + os.environ['GITHUB_REPOSITORY'] +
                             '/git/ref/tags/' + tag))
    except subprocess.CalledProcessError as error:
        if json.loads(error.stdout or '{}').get('status') != '404':
            raise
        return {'target_commitish': source}
    if ref['object']['type'] != 'commit' or ref['object']['sha'] != source:
        raise ValueError('Release tag does not point to the validated source: ' + tag)
    return {}

def asset(current, name):
    matches = [a for a in current['assets'] if a['name'] == name]
    if len(matches) != 1:
        raise ValueError('Missing or duplicate release asset: ' + name)
    return matches[0]

def download_asset(current, name, destination):
    # Drafts are visible through the release listing, but the tag lookup used
    # by `gh release download/upload` can return 404 for an Actions token.
    found = asset(current, name)
    with destination.open('wb') as output:
        subprocess.run(['gh', 'api', '-H', 'Accept: application/octet-stream',
                        'repos/' + os.environ['GITHUB_REPOSITORY'] +
                        '/releases/assets/' + str(found['id'])],
                       check=True, stdout=output)

def upload_asset(current, path):
    url = current['upload_url'].split('{', 1)[0]
    url += '?' + urllib.parse.urlencode({'name': path.name})
    return json.loads(run('gh', 'api', '--method', 'POST', url,
                         '-H', 'Content-Type: application/octet-stream', '--input', str(path)))

def archive(directory, destination):
    # Retry staging from the same artifacts without depending on extraction times.
    with destination.open('wb') as output, gzip.GzipFile(fileobj=output, mode='wb', filename='', mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode='w') as tar:
            for path in [directory] + sorted(directory.rglob('*')):
                name = '.' if path == directory else './' + path.relative_to(directory).as_posix()
                member = tar.gettarinfo(str(path), arcname=name)
                member.uid = member.gid = member.mtime = 0
                member.uname = member.gname = ''
                if member.isfile():
                    with path.open('rb') as data:
                        tar.addfile(member, data)
                else:
                    tar.addfile(member)

def members(data):
    parts = []
    while data:
        stream = zlib.decompressobj(31)
        raw = stream.decompress(data)
        if not stream.eof:
            raise ValueError('Truncated gzip member')
        length = len(data) - len(stream.unused_data)
        parts.append((data[:length], raw))
        data = stream.unused_data
    return parts

def contents(raw):
    with tarfile.open(fileobj=io.BytesIO(raw)) as archive:
        return {m.name: archive.extractfile(m).read() for m in archive if m.isfile()}

def authenticated(path, key):
    parts = members(path.read_bytes())
    if len(parts) not in (2, 3):
        raise ValueError('Unsigned or malformed APK/index: ' + str(path))
    signature = contents(parts[0][1])
    if set(signature) != {'.SIGN.RSA256.werewolf-packages.rsa.pub'}:
        raise ValueError('Unexpected APK signing key: ' + str(path))
    with tempfile.TemporaryDirectory() as directory:
        signature_path = Path(directory) / 'sig'
        control = Path(directory) / 'control'
        signature_path.write_bytes(next(iter(signature.values())))
        control.write_bytes(parts[1][0])
        run('openssl', 'dgst', '-sha256', '-verify', str(key),
            '-signature', str(signature_path), str(control))
    return parts

def package_names(config):
    # Keep publication independent of new APIs in the authenticated builder:
    # recovery replaces this file alone in the original source snapshot.
    def expand(name):
        return name.replace('${{package.name}}', config['package']['name']).replace(
            '${{package.version}}', str(config['package']['version']))
    names = {config['package']['name']}
    ranges = {item['name']: item['items'] for item in config.get('data', [])}
    for sub in config['subpackages']:
        if sub.get('if'):
            raise ValueError('Review conditional package publication before updating')
        for key in ranges[sub['range']] if 'range' in sub else ['']:
            name = expand(sub['name']).replace('${{range.key}}', key)
            if '${{' in name:
                raise ValueError('Unresolved package name: ' + name)
            names.add(name)
    return names

def validate(pkg, directory, arch, lock, source_sha=None):
    record = json.loads((directory / 'build.json').read_text())
    expected = pkg.hooks.full_version(lock)
    if (record['build_id'], record['arch'], record['version']) != (lock['build_id'], arch, expected):
        raise ValueError('Mixed build inputs or architectures')
    if record['source_sha'] != (source_sha or os.environ['GITHUB_SHA']):
        raise ValueError('Artifact came from a different downstream commit')
    for name, digest in record['sha256'].items():
        path = directory / name
        if not path.resolve().is_relative_to(directory.resolve()) or sha256(path) != digest:
            raise ValueError('Artifact hash mismatch: ' + name)
    actual = {str(p.relative_to(directory)) for p in directory.rglob('*') if p.is_file()}
    if actual != set(record['sha256']) | {'build.json'}:
        raise ValueError('Unrecorded or missing artifact files')
    key = pkg.root / 'release/packages.pub'
    if (directory / 'werewolf-packages.rsa.pub').read_bytes() != key.read_bytes():
        raise ValueError('Ephemeral signing key cannot be published')
    upstream = json.loads((directory / 'upstream.lock.json').read_text())
    if any(upstream[k] != lock[k] for k in ['build_id', 'sha256', 'mode', 'policy_sha256']):
        raise ValueError('Upstream provenance mismatch')
    config = derive(pkg)
    if yaml.safe_load((directory / (pkg.name + '.yaml')).read_text()) != config:
        raise ValueError('Artifact recipe differs from the verified derivation')
    index = authenticated(directory / arch / 'APKINDEX.tar.gz', key)
    text = contents(index[1][1])['APKINDEX'].decode()
    indexed = set()
    for stanza in text.strip().split('\n\n'):
        fields = dict(line.split(':', 1) for line in stanza.splitlines())
        if fields['V'] != expected or fields['A'] != arch:
            raise ValueError('Index contains a different version or architecture')
        indexed.add(fields['P'] + '-' + fields['V'] + '.apk')
    apks = list((directory / arch).glob('*.apk'))
    if not apks or indexed != {p.name for p in apks}:
        raise ValueError('Incomplete APK index')
    if indexed != {name + '-' + expected + '.apk' for name in package_names(config)}:
        raise ValueError('Missing or unexpected derived package')
    check = pkg.hook('check_apk', lambda pkg, name, parts: None)
    names = []
    for apk in apks:
        parts = authenticated(apk, key)
        if len(parts) != 3:
            raise ValueError('Missing APK payload')
        info = contents(parts[1][1])['.PKGINFO'].decode()
        fields = dict(line.split(' = ', 1) for line in info.splitlines() if ' = ' in line)
        if fields['pkgver'] != expected or fields['arch'] != arch:
            raise ValueError('APK metadata mismatch')
        if apk.name != fields['pkgname'] + '-' + fields['pkgver'] + '.apk':
            raise ValueError('APK filename and signed metadata differ')
        if fields['datahash'] != hashlib.sha256(parts[2][0]).hexdigest():
            raise ValueError('APK payload hash mismatch')
        check(pkg, fields['pkgname'], parts)
        names.append(fields['pkgname'])
    pkg.hook('check_apks', lambda pkg, names: None)(pkg, names)
    return record

def status(tag):
    current = release(tag)
    if current is None:
        return 'build'
    if 'release.json' not in {a['name'] for a in current['assets']}:
        if not current['draft']:
            raise ValueError('Published release has no completion manifest')
        return 'build'
    return 'publish' if current['draft'] else 'complete'

def download(url):
    # A public object, or None when it is absent. Any other failure stops the
    # stage, so notes never call a release the first, or unchanged, because a
    # read failed; the next scheduled check stages again.
    # dist.werewolf-linux.org refuses Python's default User-Agent.
    request = urllib.request.Request(url, headers={'User-Agent': AGENT})
    for attempt in range(3):
        try:
            with urllib.request.urlopen(request, timeout=120) as response:
                return response.read()
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return None
            if attempt == 2:
                raise
        except urllib.error.URLError:
            if attempt == 2:
                raise
        time.sleep(15 * (attempt + 1))

def apk_files(data):
    # An APK's payload files by path.
    return contents(members(data)[-1][1])

def stanzas(index):
    # An authenticated APKINDEX.tar.gz's stanzas as {field: value} maps.
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / 'APKINDEX.tar.gz'
        path.write_bytes(index)
        text = contents(authenticated(path, ROOT / 'release/packages.pub')[1][1])['APKINDEX'].decode()
    return [dict(line.split(':', 1) for line in s.splitlines()) for s in text.strip().split('\n\n')]

def lookup(path):
    # A GitHub API read for notes' commit lists; None when GitHub cannot
    # answer, which the notes render as unavailable, never as no change.
    try:
        return json.loads(run('gh', 'api', path))
    except subprocess.CalledProcessError:
        return None

def subjects(commits):
    return [{'sha': c['sha'], 'subject': c['commit']['message'].splitlines()[0]}
            for c in commits if len(c['parents']) == 1]

def gather(pkg, artifacts, lock, records):
    # Facts for the shared notes: this build's packages, the previous release's
    # provenance and Wolfi's advisories. A package may gather its own.
    wolfi = releases.baseline_version(lock)
    source_version = lock['version'] + '-r' + str(lock['epoch'])
    previous = None
    pointer = download(pkg.public + 'current.json')
    if pointer is not None:
        pointer = json.loads(pointer)
        if pointer['build_id'] == lock['build_id']:
            raise ValueError('current.json already names this build')
        old = download(pkg.public + 'builds/' + pointer['build_id'] + '/release-notes.json')
        old = json.loads(old) if old is not None else {}
        previous = {'version': pointer['version'], 'build_id': pointer['build_id'],
                    'commit': pointer['source_sha'],
                    'wolfi_version': old.get('source', {}).get('wolfi_version'),
                    'source_commit': old.get('source', {}).get('source_commit'),
                    'source_version': old.get('source', {}).get('version', old.get('source', {}).get('wolfi_version'))}
    commits = []
    if previous is not None:
        found = lookup('repos/' + os.environ['GITHUB_REPOSITORY'] + '/compare/' + previous['commit'] +
                       '...' + os.environ['GITHUB_SHA'])
        commits = subjects(found['commits']) if found else None
    architectures = {}
    for arch in ARCHES:
        index = (artifacts / (pkg.name + '-' + arch) / arch / 'APKINDEX.tar.gz').read_bytes()
        architectures[arch] = {'packages': sorted(
            [{'name': s['P'], 'version': s['V'], 'size': int(s['S'])} for s in stanzas(index)],
            key=lambda p: p['name'])}
    secdb = json.loads(download(generic_notes.SECDB))
    stream = releases.recipe(pkg, lock)['package']['name']
    return {'title': pkg.title, 'package': pkg.name, 'version': records[ARCHES[0]]['version'],
            'build_id': lock['build_id'], 'context_path': str(pkg.dir.relative_to(ROOT.parent)),
            'date': datetime.datetime.now(datetime.timezone.utc).date().isoformat(),
            'commit': os.environ['GITHUB_SHA'],
            'run': os.environ.get('GITHUB_SERVER_URL', 'https://github.com') + '/' +
                   os.environ['GITHUB_REPOSITORY'] + '/actions/runs/' + os.environ['GITHUB_RUN_ID'],
            'source': {'source_commit': lock['source_commit'], 'wolfi_commit': lock['wolfi_commit'],
                       'version': source_version, 'release': lock.get('release'), 'kind': lock.get('source_kind', 'wolfi'),
                       'wolfi_version': wolfi, 'recipe': lock['recipe'], 'policy_revision': lock['revision'],
                       'upstream': pkg.hook('UPSTREAM')},
            'repository': {'stable': HARDENED, 'snapshot': pkg.public + 'builds/' + lock['build_id'],
                           'key_sha256': sha256(ROOT / 'release/packages.pub')},
            'patches': (pkg.dir / 'patches/series').read_text().split(), 'wolfi_inputs': lock['sha256'],
            'previous': previous, 'commits': commits,
            'security': generic_notes.security(secdb, stream, previous['source_version'] if previous else None, source_version),
            'architectures': architectures}

def stage(pkg, artifacts, assets, lock, tag):
    assets.mkdir(parents=True, exist_ok=True)
    records = {}
    for arch in ARCHES:
        directory = artifacts / (pkg.name + '-' + arch)
        records[arch] = validate(pkg, directory, arch, lock)
        archive(directory, assets / (pkg.name + '-' + arch + '.tar.gz'))
    manifest = {'package': pkg.name, 'build_id': lock['build_id'], 'version': records[ARCHES[0]]['version'],
                'source_sha': os.environ['GITHUB_SHA'], 'run_number': int(os.environ['GITHUB_RUN_NUMBER']),
                'repository': pkg.public + 'builds/' + lock['build_id'],
                'sha256': {p.name: sha256(p) for p in sorted(assets.glob('*.tar.gz'))}}
    (assets / 'SHA256SUMS').write_text(''.join(digest + '  ' + name + '\n' for name, digest in manifest['sha256'].items()))
    (assets / 'release.json').write_text(json.dumps(manifest, indent=2) + '\n')
    current = release(tag)
    staged = {a['name'] for a in current['assets']} if current else set()
    files = note_files(pkg)
    if 'release-notes.json' in staged:
        # A retry keeps the notes first staged, whatever has changed since.
        for name in files:
            download_asset(current, name, assets / name)
    else:
        module = notes_of(pkg)
        made = module.build(pkg.hook('gather', gather)(pkg, artifacts, lock, records))
        (assets / 'release-notes.json').write_bytes(module.encode(made))
        (assets / 'release-notes.md').write_text(module.markdown(made))
        if 'release-notes.html' in files:
            (assets / 'release-notes.html').write_text(module.page(made))
    body = {'body': (assets / 'release-notes.md').read_text()}
    with tempfile.TemporaryDirectory() as directory:
        request = Path(directory) / 'release.json'
        if current is None:
            body.update({'tag_name': tag, 'draft': True, 'make_latest': 'false',
                         'name': pkg.title + ' ' + manifest['version'], **release_target(tag)})
            request.write_text(json.dumps(body))
            current = json.loads(run('gh', 'api', '--method', 'POST',
                                     'repos/' + os.environ['GITHUB_REPOSITORY'] + '/releases',
                                     '--input', str(request)))
        elif current.get('body') != body['body'] and current['draft']:
            request.write_text(json.dumps(body))
            run('gh', 'api', '--method', 'PATCH', 'repos/' + os.environ['GITHUB_REPOSITORY'] +
                '/releases/' + str(current['id']), '--input', str(request))
    if not current['draft']:
        raise ValueError('Refusing to change a published release')
    existing = {a['name'] for a in current['assets']}
    # release.json is last: a staged draft with it is complete.
    for path in [assets / (pkg.name + '-' + a + '.tar.gz') for a in ARCHES] + [assets / 'SHA256SUMS'] + \
            [assets / name for name in files] + [assets / 'release.json']:
        if path.name in existing:
            with tempfile.TemporaryDirectory() as directory:
                download_asset(current, path.name, Path(directory) / path.name)
                if sha256(Path(directory) / path.name) != sha256(path):
                    raise ValueError('Release asset already exists with different bytes: ' + path.name)
        else:
            upload_asset(current, path)

def aws(*arguments):
    return subprocess.run(['aws', '--endpoint-url', ENDPOINT, '--region', 'auto',
                           's3api', *arguments], capture_output=True, text=True)

def fetch(pkg, key, destination):
    result = aws('get-object', '--bucket', BUCKET, '--key', pkg.prefix + key, str(destination))
    if result.returncode == 0:
        return True
    if '(NoSuchKey)' in result.stderr or '(404)' in result.stderr:
        return False
    raise RuntimeError(result.stderr)

def fetch_etag(pkg, key, destination):
    """Read one committed object and its ETag together, without CDN caching."""
    result = aws('get-object', '--bucket', BUCKET, '--key', pkg.prefix + key, str(destination))
    if result.returncode == 0:
        etag = json.loads(result.stdout)['ETag']
        if not isinstance(etag, str) or not etag:
            raise ValueError('R2 returned no object ETag')
        return etag
    if '(NoSuchKey)' in result.stderr or '(404)' in result.stderr:
        return None
    raise RuntimeError(result.stderr)

def raced(result):
    return any(code in result.stderr for code in ('(PreconditionFailed)', '(ConditionalRequestConflict)'))

def upload(pkg, path, key, immutable=True, content_type=None):
    if immutable:
        with tempfile.TemporaryDirectory() as directory:
            previous = Path(directory) / 'object'
            if fetch(pkg, key, previous):
                if sha256(previous) != sha256(path):
                    raise ValueError('R2 object already exists with different bytes: ' + key)
                return
    result = aws('put-object', '--bucket', BUCKET, '--key', pkg.prefix + key, '--body', str(path),
                 '--cache-control', 'public, max-age=31536000, immutable' if immutable else 'no-cache',
                 *(['--if-none-match', '*'] if immutable else []),
                 *(['--content-type', content_type] if content_type else []))
    if result.returncode:
        if immutable and raced(result):
            # Another publisher may have created this immutable object after
            # our read. Verify its bytes rather than replacing them.
            with tempfile.TemporaryDirectory() as directory:
                previous = Path(directory) / 'object'
                if fetch(pkg, key, previous) and sha256(previous) == sha256(path):
                    return
        raise RuntimeError(result.stderr)

def entries(parts):
    # An authenticated index's stanzas, by the APK file each names.
    text = contents(parts[1][1])['APKINDEX'].decode()
    found = {}
    for stanza in text.strip().split('\n\n'):
        fields = dict(line.split(':', 1) for line in stanza.splitlines())
        found[fields['P'] + '-' + fields['V'] + '.apk'] = stanza
    return found

def merge(previous, current):
    # A published version keeps its first build: machines may hold it.
    merged, added = dict(previous), []
    for name, stanza in sorted(current.items()):
        if name not in merged:
            merged[name] = stanza
            added.append(name)
        elif merged[name] != stanza:
            print(name + ': the stable repository keeps its earlier build; increment policy.json revision to ship this one')
    return merged, added

def signed_index(found, key):
    text = ''.join(found[name] + '\n\n' for name in sorted(found)).encode()
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode='w', format=tarfile.USTAR_FORMAT) as archive:
        for name, data in [('APKINDEX', text), ('DESCRIPTION', b'')]:
            item = tarfile.TarInfo(name)
            item.size, item.mode = len(data), 0o644
            archive.addfile(item, io.BytesIO(data))
    control = gzip.compress(buffer.getvalue(), mtime=0)
    signature = subprocess.run(['openssl', 'dgst', '-sha256', '-sign', str(key)], input=control,
                               stdout=subprocess.PIPE, check=True).stdout
    item = tarfile.TarInfo('.SIGN.RSA256.werewolf-packages.rsa.pub')
    item.size, item.mode, item.uname, item.gname = len(signature), 0o644, 'root', 'root'
    # apk reads both members as one tar stream: the signature's has no end marker.
    header = item.tobuf(tarfile.USTAR_FORMAT, tarfile.ENCODING, 'surrogateescape')
    return gzip.compress(header + signature + b'\0' * (-len(signature) % 512), mtime=0) + control

def merge_index(repo, current, arch, key, directory, trust=ROOT / 'release/packages.pub'):
    """Commit a union against the exact index read; only the index write serializes.

    All APKs current lists must already exist. A lost race re-reads and merges
    the winner, preserving its additions and every previously published version.
    """
    if isinstance(current, Path):
        current = entries(authenticated(current, trust))
    remote = arch + '/APKINDEX.tar.gz'
    previous_path = directory / (arch + '-cas-previous.tar.gz')
    next_path = directory / (arch + '-cas-next.tar.gz')
    for attempt in range(12):
        etag = fetch_etag(repo, remote, previous_path)
        previous = entries(authenticated(previous_path, trust)) if etag is not None else {}
        merged, added = merge(previous, current)
        if not added and etag is not None:
            return
        next_path.write_bytes(signed_index(merged, key))
        authenticated(next_path, trust)
        condition = ['--if-match', etag] if etag is not None else ['--if-none-match', '*']
        result = aws('put-object', '--bucket', BUCKET, '--key', repo.prefix + remote,
                     '--body', str(next_path), '--cache-control', 'no-cache', *condition)
        if result.returncode == 0:
            return
        if not raced(result):
            raise RuntimeError(result.stderr)
        time.sleep(min(attempt + 1, 5))
    raise RuntimeError('Index kept changing during publication: ' + repo.prefix + remote)

def stable(pkg, root, arch, key, directory):
    # The stable repository's next index, verified, and the APKs it adds.
    trust = ROOT / 'release/packages.pub'
    previous = {}
    published = directory / (arch + '-previous.tar.gz')
    if fetch(pkg, arch + '/APKINDEX.tar.gz', published):
        previous = entries(authenticated(published, trust))
    merged, added = merge(previous, entries(authenticated(root / arch / 'APKINDEX.tar.gz', trust)))
    index = directory / (arch + '-APKINDEX.tar.gz')
    index.write_bytes(signed_index(merged, key))
    authenticated(index, trust)
    return index, added

def advance_current(pkg, manifest, path):
    """Never let an older concurrent publication move the release pointer back."""
    with tempfile.TemporaryDirectory() as directory:
        previous = Path(directory) / 'current.json'
        for attempt in range(12):
            etag = fetch_etag(pkg, 'current.json', previous)
            if etag is not None and json.loads(previous.read_text())['run_number'] >= manifest['run_number']:
                return
            condition = ['--if-match', etag] if etag is not None else ['--if-none-match', '*']
            result = aws('put-object', '--bucket', BUCKET, '--key', pkg.prefix + 'current.json',
                         '--body', str(path), '--cache-control', 'no-cache',
                         '--content-type', 'application/json', *condition)
            if result.returncode == 0:
                return
            if not raced(result):
                raise RuntimeError(result.stderr)
            time.sleep(min(attempt + 1, 5))
    raise RuntimeError('Current release kept changing: ' + pkg.name)

def publish(pkg, assets, lock, tag, key=ROOT / 'build/keys/werewolf-packages.rsa'):
    assets.mkdir(parents=True, exist_ok=True)
    current = release(tag)
    if current is None:
        raise ValueError('Release has not been staged')
    seen = set()
    for found in current['assets']:
        name = found['name']
        if not name or Path(name).name != name or name in ('.', '..') or name in seen:
            raise ValueError('Unsafe or duplicate release asset name')
        seen.add(name)
        download_asset(current, name, assets / name)
    manifest = json.loads((assets / 'release.json').read_text())
    if manifest['build_id'] != lock['build_id'] or not re.fullmatch('[0-9a-f]{40}', manifest['source_sha']):
        raise ValueError('Release manifest does not match captured inputs')
    if set(manifest['sha256']) != {pkg.name + '-' + a + '.tar.gz' for a in ARCHES}:
        raise ValueError('Both native architectures are required')
    snapshot = 'builds/' + lock['build_id'] + '/'
    with tempfile.TemporaryDirectory() as directory:
        work = Path(directory)
        roots, indexes = {}, {}
        # Validate and sign everything before the first upload.
        for name, digest in manifest['sha256'].items():
            if sha256(assets / name) != digest:
                raise ValueError('Release archive hash mismatch')
            arch = next(a for a in ARCHES if name == pkg.name + '-' + a + '.tar.gz')
            roots[arch] = work / arch
            with tarfile.open(assets / name) as archive:
                archive.extractall(roots[arch], filter='data')
            validate(pkg, roots[arch], arch, lock, source_sha=manifest['source_sha'])
            indexes[arch] = stable(pkg, roots[arch], arch, key, work)
        for arch, root in roots.items():
            # Objects first, then signed index; readers never see a partial index.
            for apk in sorted((root / arch).glob('*.apk')):
                upload(pkg, apk, snapshot + arch + '/' + apk.name)
            upload(pkg, root / arch / 'APKINDEX.tar.gz', snapshot + arch + '/APKINDEX.tar.gz')
            upload(pkg, assets / (pkg.name + '-' + arch + '.tar.gz'), snapshot + pkg.name + '-' + arch + '.tar.gz')
        upload(pkg, assets / 'SHA256SUMS', snapshot + 'SHA256SUMS')
        # A draft staged before release notes existed has none.
        noted = (assets / 'release-notes.json').is_file()
        for name, kind in note_files(pkg).items() if noted else ():
            upload(pkg, assets / name, snapshot + name, content_type=kind)
        upload(pkg, assets / 'release.json', snapshot + 'release.json')
        # The stable repository machines follow: every published APK, kept, and one index.
        for arch, (index, added) in indexes.items():
            for name in added:
                upload(pkg, roots[arch] / arch / name, arch + '/' + name)
            merge_index(pkg, index, arch, key, work)
        # Include both validated architectures in the consumer feed before
        # completing this package's release. Retries also repair partial uploads.
        import feed
        feed.include(pkg, key)
    advance_current(pkg, manifest, assets / 'release.json')
    if noted and hasattr(notes_of(pkg), 'releases'):
        update_index(pkg, json.loads((assets / 'release-notes.json').read_text()))
    # The public release is the completion marker, after both R2 repositories exist.
    request = assets / 'publish-request.json'
    request.write_text(json.dumps({'draft': False, 'make_latest': 'false'}))
    run('gh', 'api', '--method', 'PATCH', 'repos/' + os.environ['GITHUB_REPOSITORY'] +
        '/releases/' + str(current['id']), '--input', str(request))
    print(manifest['repository'])

def update_index(pkg, made):
    # A package whose notes list releases gets a front page: newest first.
    module = notes_of(pkg)
    with tempfile.TemporaryDirectory() as directory:
        listing = Path(directory) / 'releases.json'
        existing = json.loads(listing.read_text()) if fetch(pkg, 'releases.json', listing) else []
        if not isinstance(existing, list) or not all({'build_id', 'version'} <= set(r) for r in existing):
            raise ValueError('releases.json is malformed')
        found = module.releases(existing, made)
        listing.write_bytes(module.encode(found))
        upload(pkg, listing, 'releases.json', immutable=False, content_type='application/json')
        page = Path(directory) / 'index.html'
        page.write_text(module.index(found))
        upload(pkg, page, 'index.html', immutable=False, content_type='text/html; charset=utf-8')

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('command', choices=['status', 'stage', 'publish'])
    parser.add_argument('package')
    parser.add_argument('--artifacts', type=Path, default=Path('artifacts'))
    parser.add_argument('--assets', type=Path)
    args = parser.parse_args()
    pkg = packages.Package(args.package)
    lock = verify(pkg)
    tag = pkg.tag(lock['build_id'])
    assets = args.assets or pkg.build / 'release'
    if args.command == 'status':
        state = status(tag)
        if os.environ.get('GITHUB_OUTPUT'):
            with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
                output.write('state=' + state + '\nbuild_id=' + lock['build_id'] + '\n')
        print(state)
    elif args.command == 'stage':
        stage(pkg, args.artifacts, assets, lock, tag)
    else:
        publish(pkg, assets, lock, tag)

if __name__ == '__main__':
    main()
