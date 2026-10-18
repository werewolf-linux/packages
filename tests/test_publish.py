import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import common
import plan
import publish
import repository


def archive(files):
    output = io.BytesIO()
    with tarfile.open(fileobj=output, mode='w') as tar:
        for name, data in files.items():
            item = tarfile.TarInfo(name)
            item.size, item.mode = len(data), 0o644
            tar.addfile(item, io.BytesIO(data))
    return gzip.compress(output.getvalue(), mtime=0)


def unsigned(path, name, arch, version='1-r0', content=b'hello'):
    data = archive({'usr/share/test/message': content})
    info = ('pkgname = %s\npkgver = %s\narch = %s\npkgdesc = test package\nurl = https://example.test\n'
            'license = MIT\nsize = 5\norigin = %s\nbuilddate = 1700000000\npackager = test\ndatahash = %s\n' %
            (name, version, arch, name, hashlib.sha256(data).hexdigest())).encode()
    path.write_bytes(archive({'.PKGINFO': info}) + data)


class Store:
    def __init__(self):
        self.objects, self.writes = {}, []
        self.before_put = None

    def etag(self, name):
        return '"' + hashlib.sha256(self.objects[name]).hexdigest() + '"'

    def aws(self, operation, *args):
        name = args[args.index('--key') + 1]
        def result(code=0, output='', error=''):
            return subprocess.CompletedProcess([], code, output, error)
        if operation == 'get-object':
            if name not in self.objects:
                return result(1, error='(NoSuchKey)')
            Path(args[-1]).write_bytes(self.objects[name])
            return result(output=json.dumps(dict(ETag=self.etag(name))))
        if operation != 'put-object':
            raise AssertionError(operation)
        if self.before_put:
            callback, self.before_put = self.before_put, None
            callback(name)
        if '--if-none-match' in args and name in self.objects:
            return result(1, error='(PreconditionFailed)')
        if '--if-match' in args and (name not in self.objects or args[args.index('--if-match') + 1] != self.etag(name)):
            return result(1, error='(PreconditionFailed)')
        self.objects[name] = Path(args[args.index('--body') + 1]).read_bytes()
        self.writes.append(name)
        return result()


class Publisher(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.keys = tempfile.TemporaryDirectory()
        cls.key, cls.trust = Path(cls.keys.name) / 'werewolf-packages.rsa', Path(cls.keys.name) / 'werewolf-packages.rsa.pub'
        # Melange emits PKCS#1 on every host; OpenSSL 3 genrsa defaults to PKCS#8.
        subprocess.run([os.environ.get('MELANGE', 'melange'), 'keygen', str(cls.key)], check=True, capture_output=True)

    @classmethod
    def tearDownClass(cls):
        cls.keys.cleanup()

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.artifacts = self.root / 'artifacts'
        self.meta = dict(context='example', names=['example'], version='1-r0', inputs='a' * 64)
        self.environment = dict(GITHUB_SHA='b' * 40, GITHUB_RUN_ID='123', GITHUB_RUN_NUMBER='2')
        self.addCleanup(patch.stopall)
        patch.dict(os.environ, self.environment).start()
        self.store = Store()
        patch.object(repository, 'aws', self.store.aws).start()
        patch.object(repository.time, 'sleep').start()
        for arch in common.ARCHES:
            directory = self.artifacts / arch
            directory.mkdir(parents=True)
            path = directory / 'example-1-r0.apk'
            unsigned(path, 'example', arch)
            record = dict(format='werewolf-package-build/1', **self.meta, arch=arch, commit='b' * 40,
                          run_id='123', source_date_epoch=1700000000, sha256={path.name: common.sha256(path)})
            (directory / 'build.json').write_bytes(common.encode(record))

    def test_unsigned_payload_and_metadata_are_checked(self):
        path = self.artifacts / 'aarch64/example-1-r0.apk'
        common.apk(path, 'example', '1-r0', 'aarch64')
        with self.assertRaisesRegex(ValueError, 'metadata'):
            common.apk(path, 'example', '1-r0', 'x86_64')
        path.write_bytes(path.read_bytes() + b'corrupt')
        with self.assertRaisesRegex(ValueError, 'payload hash'):
            common.apk(path, 'example', '1-r0', 'aarch64')

    def test_wrong_commit_run_or_unrecorded_file_is_rejected(self):
        directory = self.artifacts / 'x86_64'
        with self.assertRaisesRegex(ValueError, 'another commit'):
            publish.validate(directory, self.meta, 'x86_64', 'c' * 40, '123')
        with self.assertRaisesRegex(ValueError, 'another commit'):
            publish.validate(directory, self.meta, 'x86_64', 'b' * 40, '456')
        (directory / 'unexpected').touch()
        with self.assertRaisesRegex(ValueError, 'unexpected artifact'):
            publish.validate(directory, self.meta, 'x86_64', 'b' * 40, '123')

    def test_missing_architecture_prevents_all_remote_writes(self):
        shutil.rmtree(self.artifacts / 'aarch64')
        with self.assertRaises(FileNotFoundError):
            publish.publish(self.meta, self.artifacts, self.key, self.trust)
        self.assertEqual([], self.store.writes)

    def test_real_signing_indexing_and_publication_roundtrip(self):
        publish.publish(self.meta, self.artifacts, self.key, self.trust)
        for arch in common.ARCHES:
            apk_key = 'apk/packages/' + arch + '/example-1-r0.apk'
            index_key = 'apk/packages/' + arch + '/APKINDEX.tar.gz'
            self.assertLess(self.store.writes.index(apk_key), self.store.writes.index(index_key))
            path = self.root / 'signed.apk'
            path.write_bytes(self.store.objects[apk_key])
            common.apk(path, 'example', '1-r0', arch, signed=True, trust=self.trust)
        pointer = self.store.objects['apk/packages/contexts/example/current.json']
        found = repository.open_envelope(pointer, self.trust)
        self.assertEqual({'x86_64', 'aarch64'}, set(found['architectures']))
        self.assertEqual('apk/packages/contexts/example/current.json', self.store.writes[-1])

    def test_snapshot_recovers_without_any_build_artifacts(self):
        with patch.object(repository, 'merge_index', side_effect=RuntimeError('upload interrupted')):
            with self.assertRaisesRegex(RuntimeError, 'interrupted'):
                publish.publish(self.meta, self.artifacts, self.key, self.trust)
        shutil.rmtree(self.artifacts)
        publish.publish(self.meta, self.artifacts, self.key, self.trust)
        self.assertIn('apk/packages/contexts/example/current.json', self.store.objects)

    def test_immutable_objects_reject_conflicting_bytes(self):
        first, second = self.root / 'first', self.root / 'second'
        first.write_bytes(b'first')
        second.write_bytes(b'second')
        repository.immutable(first, 'x86_64/test.apk')
        repository.immutable(first, 'x86_64/test.apk')
        with self.assertRaisesRegex(ValueError, 'different bytes'):
            repository.immutable(second, 'x86_64/test.apk')
        self.assertEqual(b'first', self.store.objects['apk/packages/x86_64/test.apk'])

    def test_index_race_preserves_every_existing_version(self):
        name = 'apk/packages/x86_64/APKINDEX.tar.gz'
        old = {'old-1-r0.apk': 'P:old\nV:1-r0\nA:x86_64\nC:Q1old'}
        other = {'other-1-r0.apk': 'P:other\nV:1-r0\nA:x86_64\nC:Q1other'}
        self.store.objects[name] = repository.signed_index(old, self.key)
        self.store.before_put = lambda k: self.store.objects.update({k: repository.signed_index({**old, **other}, self.key)})
        current = {'new-1-r0.apk': 'P:new\nV:1-r0\nA:x86_64\nC:Q1new'}
        repository.merge_index(current, 'x86_64', self.key, self.root, self.trust)
        index = self.root / 'read-index'
        index.write_bytes(self.store.objects[name])
        self.assertEqual(set(old) | set(other) | set(current), set(repository.entries(index, 'x86_64', self.trust)))

    def test_corrupt_or_wrongly_signed_indexes_do_not_reset_the_feed(self):
        name = 'apk/packages/x86_64/APKINDEX.tar.gz'
        self.store.objects[name] = b'corrupt'
        with self.assertRaises(Exception):
            repository.merge_index({}, 'x86_64', self.key, self.root, self.trust)
        self.assertEqual([], self.store.writes)

    def test_signed_manifest_rejects_tampering(self):
        data = repository.envelope(self.meta, self.key)
        self.assertEqual(self.meta, repository.open_envelope(data, self.trust))
        value = json.loads(data)
        import base64
        value['payload'] = base64.b64encode(b'{"context":"malicious"}').decode()
        with self.assertRaises(subprocess.CalledProcessError):
            repository.open_envelope(common.encode(value), self.trust)

    def test_plan_skips_only_a_valid_signed_matching_identity(self):
        data = repository.envelope(dict(**self.meta, run_number=1), self.key)
        with patch.object(plan, 'download', return_value=data), patch.object(common, 'verify',
                side_effect=lambda value, signature, trust=None: self.verify_production(value, signature)):
            self.assertEqual('complete', plan.state(self.meta))
            changed = dict(self.meta, inputs='c' * 64)
            with self.assertRaisesRegex(ValueError, 'bump package.epoch'):
                plan.state(changed)

    def test_plan_releases_new_and_staged_contexts_independently(self):
        output = self.root / 'github-output'
        def metadata(name):
            return dict(self.meta, context=name)
        with patch.object(sys, 'argv', ['plan.py']), \
             patch.object(common, 'contexts', return_value=['new', 'staged', 'existing']), \
             patch.object(common, 'metadata', side_effect=metadata), \
             patch.object(plan, 'state', side_effect=lambda m: {'new': 'build', 'staged': 'resume', 'existing': 'complete'}[m['context']]), \
             patch.dict(os.environ, {'GITHUB_OUTPUT': str(output)}), \
             patch('sys.stdout', new_callable=io.StringIO):
            plan.main()
        found = dict(line.split('=', 1) for line in output.read_text().splitlines())
        self.assertEqual({'include': [{'package': 'new', 'build': True}, {'package': 'staged', 'build': False}]},
                         json.loads(found['releases']))
        self.assertEqual(['new', 'staged'], json.loads(found['publish']))
        self.assertEqual(2, len(json.loads(found['build'])['include']))

    def verify_production(self, value, signature):
        path = self.root / 'sig'
        path.write_bytes(signature)
        subprocess.run(['openssl', 'dgst', '-sha256', '-verify', self.trust, '-signature', path],
                       input=value, check=True, capture_output=True)


if __name__ == '__main__':
    unittest.main()
