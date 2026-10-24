import base64
import gzip
import hashlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import feed
import publish


class Store:
    """S3 committed objects, including conditional writes and injected races."""
    def __init__(self):
        self.objects = {}
        self.writes = []
        self.before_put = None

    def aws(self, operation, *args):
        key = args[args.index('--key') + 1]
        def result(code=0, output='', error=''):
            return subprocess.CompletedProcess([], code, output, error)
        if operation == 'get-object':
            if key not in self.objects:
                return result(1, error='(NoSuchKey)')
            Path(args[-1]).write_bytes(self.objects[key])
            return result(output=json.dumps({'ETag': self.etag(key)}))
        if operation != 'put-object':
            raise AssertionError(operation)
        if self.before_put:
            callback, self.before_put = self.before_put, None
            callback(key)
        if '--if-none-match' in args and key in self.objects:
            return result(1, error='(PreconditionFailed)')
        if '--if-match' in args and (key not in self.objects or
                args[args.index('--if-match') + 1] != self.etag(key)):
            return result(1, error='(PreconditionFailed)')
        self.objects[key] = Path(args[args.index('--body') + 1]).read_bytes()
        self.writes.append(key)
        return result()

    def etag(self, key):
        return '"' + hashlib.md5(self.objects[key]).hexdigest() + '"'


class Feed(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.keys = tempfile.TemporaryDirectory()
        cls.key = Path(cls.keys.name) / 'key'
        cls.trust = Path(cls.keys.name) / 'key.pub'
        subprocess.run(['openssl', 'genrsa', '-out', str(cls.key), '2048'], check=True, capture_output=True)
        subprocess.run(['openssl', 'rsa', '-in', str(cls.key), '-pubout', '-out', str(cls.trust)],
                       check=True, capture_output=True)

    @classmethod
    def tearDownClass(cls):
        cls.keys.cleanup()

    def setUp(self):
        self.work = tempfile.TemporaryDirectory()
        self.addCleanup(self.work.cleanup)
        self.root = Path(self.work.name)
        self.store = Store()
        self.addCleanup(patch.stopall)
        patch.object(publish, 'aws', side_effect=self.store.aws).start()
        patch.object(publish.time, 'sleep').start()

    def signed(self, found):
        return publish.signed_index(found, self.key)

    def index(self, key):
        path = self.root / 'read-index'
        path.write_bytes(self.store.objects[key])
        return publish.entries(publish.authenticated(path, self.trust))

    def stanza(self, name, version='1-r1000'):
        return f'P:{name}\nV:{version}\nA:aarch64\nC:Q1test'

    def merge(self, current):
        publish.merge_index(feed.REPOSITORY, current, 'aarch64', self.key, self.root, self.trust)

    def test_existing_index_race_preserves_both_publishers_and_all_old_versions(self):
        key = 'apk/hardened/aarch64/APKINDEX.tar.gz'
        old = {'python-1-r1000.apk': self.stanza('python')}
        self.store.objects[key] = self.signed(old)
        other = {**old, 'glibc-1-r1000.apk': self.stanza('glibc')}
        self.store.before_put = lambda k: self.store.objects.update({k: self.signed(other)})
        self.merge({'python-2-r1000.apk': self.stanza('python', '2-r1000')})
        self.assertEqual(set(other) | {'python-2-r1000.apk'}, set(self.index(key)))

    def test_initial_creation_race_re_reads_the_winner(self):
        key = 'apk/hardened/aarch64/APKINDEX.tar.gz'
        other = {'glibc-1-r1000.apk': self.stanza('glibc')}
        self.store.before_put = lambda k: self.store.objects.update({k: self.signed(other)})
        self.merge({'php-1-r1000.apk': self.stanza('php')})
        self.assertEqual({'glibc-1-r1000.apk', 'php-1-r1000.apk'}, set(self.index(key)))

    def test_retry_keeps_the_first_build_and_makes_no_redundant_write(self):
        key = 'apk/hardened/aarch64/APKINDEX.tar.gz'
        old = {'php-1-r1000.apk': self.stanza('php')}
        self.store.objects[key] = self.signed(old)
        self.merge({'php-1-r1000.apk': self.stanza('php') + '\nT:different build'})
        self.assertEqual(old, self.index(key))
        self.assertEqual([], self.store.writes)

    def test_invalid_signature_and_read_errors_never_reset_the_index(self):
        key = 'apk/hardened/aarch64/APKINDEX.tar.gz'
        self.store.objects[key] = b'corrupted'
        with self.assertRaises(Exception):
            self.merge({'php-1-r1000.apk': self.stanza('php')})
        self.assertEqual(b'corrupted', self.store.objects[key])
        with patch.object(publish, 'aws', return_value=subprocess.CompletedProcess([], 1, '', '(AccessDenied)')):
            with self.assertRaisesRegex(RuntimeError, 'AccessDenied'):
                self.merge({'php-1-r1000.apk': self.stanza('php')})
        self.assertEqual([], self.store.writes)

    def test_an_immutable_creation_race_checks_the_committed_bytes(self):
        path = self.root / 'package.apk'
        path.write_bytes(b'ours')
        self.store.before_put = lambda key: self.store.objects.update({key: b'ours'})
        publish.upload(feed.REPOSITORY, path, 'aarch64/package.apk')
        path.write_bytes(b'different')
        with self.assertRaisesRegex(ValueError, 'different bytes'):
            publish.upload(feed.REPOSITORY, path, 'aarch64/package.apk')
        self.assertEqual(b'ours', self.store.objects['apk/hardened/aarch64/package.apk'])

    def test_an_older_concurrent_publication_never_rolls_back_current(self):
        pkg = SimpleNamespace(name='php', prefix='apk/php/')
        key = pkg.prefix + 'current.json'
        self.store.objects[key] = json.dumps({'run_number': 1}).encode()
        path = self.root / 'release.json'
        path.write_text(json.dumps({'run_number': 2}))
        self.store.before_put = lambda k: self.store.objects.update({k: json.dumps({'run_number': 3}).encode()})
        publish.advance_current(pkg, {'run_number': 2}, path)
        self.assertEqual(3, json.loads(self.store.objects[key])['run_number'])
        self.assertEqual([], self.store.writes)

    def member(self, files):
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode='w') as archive:
            for name, data in files.items():
                entry = tarfile.TarInfo(name)
                entry.size = len(data)
                archive.addfile(entry, io.BytesIO(data))
        return gzip.compress(buffer.getvalue(), mtime=0)

    def source(self, pkg, version):
        for arch in publish.ARCHES:
            payload = self.member({'usr/bin/' + pkg.name: b'program'})
            info = (f'pkgname = {pkg.name}\npkgver = {version}\narch = {arch}\n'
                    f'datahash = {hashlib.sha256(payload).hexdigest()}\n').encode()
            control = self.member({'.PKGINFO': info})
            signature = subprocess.run(['openssl', 'dgst', '-sha256', '-sign', str(self.key)],
                                       input=control, check=True, capture_output=True).stdout
            data = self.member({'.SIGN.RSA256.werewolf-packages.rsa.pub': signature}) + control + payload
            name = pkg.name + '-' + version + '.apk'
            checksum = base64.b64encode(hashlib.sha1(control).digest()).decode()
            stanza = f'P:{pkg.name}\nV:{version}\nA:{arch}\nC:Q1{checksum}\nS:{len(data)}'
            self.store.objects[pkg.prefix + arch + '/' + name] = data
            self.store.objects[pkg.prefix + arch + '/APKINDEX.tar.gz'] = self.signed({name: stanza})

    def test_backfill_copies_verified_packages_before_indexes_and_retains_failed_families(self):
        python = SimpleNamespace(name='python', prefix='apk/python/')
        glibc = SimpleNamespace(name='glibc', prefix='apk/glibc/')
        self.source(python, '1-r1000')
        self.source(glibc, '1-r1000')
        feed.include(python, self.key, self.trust)
        # A future failed Python build publishes nothing; an independent glibc
        # publication still retains the previously validated Python version.
        feed.include(glibc, self.key, self.trust)
        for arch in publish.ARCHES:
            prefix = 'apk/hardened/' + arch + '/'
            self.assertEqual({'python-1-r1000.apk', 'glibc-1-r1000.apk'},
                             set(self.index(prefix + 'APKINDEX.tar.gz')))
            self.assertLess(self.store.writes.index(prefix + 'python-1-r1000.apk'),
                            self.store.writes.index(prefix + 'APKINDEX.tar.gz'))
        writes = list(self.store.writes)
        feed.include(python, self.key, self.trust)
        self.assertEqual(writes, self.store.writes)

    def test_missing_architecture_or_payload_never_publishes_an_unusable_index(self):
        pkg = SimpleNamespace(name='python', prefix='apk/python/')
        self.source(pkg, '1-r1000')
        self.store.objects.pop(pkg.prefix + 'x86_64/APKINDEX.tar.gz')
        with self.assertRaisesRegex(ValueError, 'missing an architecture'):
            feed.include(pkg, self.key, self.trust)
        self.assertEqual([], self.store.writes)
        self.source(pkg, '1-r1000')
        self.store.objects.pop(pkg.prefix + 'x86_64/python-1-r1000.apk')
        with self.assertRaisesRegex(ValueError, 'missing APK'):
            feed.include(pkg, self.key, self.trust)
        self.assertEqual([], self.store.writes)

    def test_tampered_payload_is_rejected_before_upload_or_index_commit(self):
        pkg = SimpleNamespace(name='php', prefix='apk/php/')
        self.source(pkg, '1-r1000')
        path = pkg.prefix + 'x86_64/php-1-r1000.apk'
        data = self.store.objects[path]
        parts = publish.members(data)
        self.store.objects[path] = parts[0][0] + parts[1][0] + self.member({'usr/bin/php': b'tampered'})
        with self.assertRaisesRegex(ValueError, 'differs from its signed source index'):
            feed.include(pkg, self.key, self.trust)
        self.assertEqual([], self.store.writes)


if __name__ == '__main__':
    unittest.main()
