import io
import gzip
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import MagicMock, patch
import yaml

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT / 'scripts'))
import inputs
import packages
import publish
import refresh
from test_notes import facts

GLIBC = packages.Package('glibc')
notes = GLIBC.hooks.notes

class LatestInputs(unittest.TestCase):
    def recipe(self, root, version, source='1'*40, primary=True):
        config = {'package': {'name': 'glibc-' + version, 'version': version,
                              'dependencies': {'provides': ['glibc=${{package.full-version}}'] if primary else []}},
                  'pipeline': [{'uses': 'git-checkout', 'with': {'expected-commit': source}}]}
        (root / ('glibc-' + version + '.yaml')).write_text(yaml.safe_dump(config))

    def test_latest_is_numeric_and_stable(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for version in ['2.9', '2.10', '2.11_rc1']:
                self.recipe(root, version)
            self.recipe(root, '99.0', primary=False)
            self.assertEqual('glibc-2.10.yaml', refresh.latest(GLIBC, root)[0].name)

    def test_invalid_newest_source_pin_never_falls_back(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            self.recipe(root, '2.9')
            self.recipe(root, '2.10', source='floating')
            with self.assertRaises(ValueError):
                refresh.latest(GLIBC, root)

    def test_unrelated_commit_does_not_rebuild_but_source_and_policy_do(self):
        lock = {'recipe': 'example.yaml', 'source_commit': '1'*40, 'revision': 6,
                'sha256': {'upstream.yaml': '2'*64}, 'mode': {'upstream.yaml': 0o644},
                'policy_sha256': {'patch': '3'*64},
                'wolfi_commit': '4'*40}
        first = inputs.identity(lock)
        lock['wolfi_commit'] = '5'*40
        self.assertEqual(first, inputs.identity(lock))
        lock['source_commit'] = '6'*40
        self.assertNotEqual(first, inputs.identity(lock))
        lock['source_commit'] = '1'*40
        lock['policy_sha256']['patch'] = '7'*64
        self.assertNotEqual(first, inputs.identity(lock))

class ReleaseGate(unittest.TestCase):
    def test_draft_lookup_uses_paginated_listing_and_preserves_api_errors(self):
        draft = {'tag_name': 'build-example', 'draft': True, 'assets': []}
        response = subprocess.CompletedProcess([], 0, json.dumps([[{'tag_name': 'unrelated'}], [draft]]), '')
        with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'example/repo'}):
            with patch.object(subprocess, 'run', return_value=response) as api:
                self.assertEqual(draft, publish.release('build-example'))
                command = api.call_args.args[0]
                self.assertIn('--paginate', command)
                self.assertIn('repos/example/repo/releases?per_page=100', command)
            response.stdout = '[]'
            with patch.object(subprocess, 'run', return_value=response):
                self.assertIsNone(publish.release('build-example'))
            response.returncode, response.stderr = 1, 'HTTP 404: repository unavailable'
            with patch.object(subprocess, 'run', return_value=response), self.assertRaises(RuntimeError):
                publish.release('build-example')

    def test_release_archive_bytes_survive_changed_extraction_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            packages = root / 'packages'
            packages.mkdir()
            (packages / 'example.apk').write_bytes(b'signed package')
            first, second = root / 'first.tar.gz', root / 'second.tar.gz'
            publish.archive(packages, first)
            os.utime(packages / 'example.apk', (42, 42))
            os.utime(packages, (43, 43))
            publish.archive(packages, second)
            self.assertEqual(first.read_bytes(), second.read_bytes())
            with tarfile.open(first) as archive:
                self.assertEqual(b'signed package', archive.extractfile('./example.apk').read())

    def test_partial_staging_resumes_without_a_second_create_or_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            artifacts, assets = root / 'artifacts', root / 'assets'
            for arch in publish.ARCHES:
                packages = artifacts / ('glibc-' + arch)
                packages.mkdir(parents=True)
                (packages / 'example.apk').write_bytes(arch.encode())
            current = None
            uploaded, calls = {}, []
            fail = True
            def run(*args):
                nonlocal current, fail
                calls.append(args)
                if args == ('gh', 'api', 'repos/example/repo/git/ref/tags/build-example'):
                    return json.dumps({'object': {'type': 'commit', 'sha': '1' * 40}})
                if args[:5] == ('gh', 'api', '--method', 'POST', 'repos/example/repo/releases'):
                    request = json.loads(Path(args[-1]).read_text())
                    # GitHub rejects an explicit historical workflow target even
                    # though the already-existing tag is correct.
                    if 'target_commitish' in request:
                        raise RuntimeError('Resource not accessible by integration (HTTP 403)')
                    self.assertTrue(request['draft'])
                    self.assertEqual('glibc 2.44-r16', request['name'])
                    self.assertEqual('false', request['make_latest'])
                    self.assertIn('## Changes since 2.44-r20', request['body'])
                    current = {'draft': True, 'assets': [], 'body': request['body'], 'id': 1,
                               'upload_url': 'https://uploads.github.com/repos/example/repo/releases/1/assets{?name,label}'}
                    return json.dumps(current)
                if args[:4] == ('gh', 'api', '--method', 'POST'):
                    self.assertTrue(args[4].startswith(current['upload_url'].split('{')[0] + '?name='))
                    path = Path(args[-1])
                    if path.name == 'glibc-aarch64.tar.gz' and fail:
                        fail = False
                        raise RuntimeError('transient upload failure')
                    uploaded[path.name] = path.read_bytes()
                    item = {'id': len(current['assets']) + 1, 'name': path.name}
                    current['assets'].append(item)
                    return json.dumps(item)
                return ''
            def download(current, name, destination):
                destination.write_bytes(uploaded[name])
            environment = {'GITHUB_REPOSITORY': 'example/repo', 'GITHUB_SHA': '1' * 40,
                           'GITHUB_RUN_NUMBER': '1'}
            gather = MagicMock(side_effect=lambda *a: facts())
            with patch.dict(os.environ, environment), \
                 patch.object(publish, 'validate', return_value={'version': '2.44-r16'}), \
                 patch.object(publish, 'release', side_effect=lambda tag: current), \
                 patch.object(publish, 'download_asset', side_effect=download), \
                 patch.object(GLIBC.hooks, 'gather', gather), \
                 patch.object(publish, 'run', side_effect=run):
                with self.assertRaisesRegex(RuntimeError, 'transient upload'):
                    publish.stage(GLIBC, artifacts, assets, {'build_id': 'example'}, 'build-example')
                self.assertNotIn('release.json', uploaded)
                original = uploaded['glibc-x86_64.tar.gz']
                os.utime(artifacts / 'glibc-x86_64' / 'example.apk', (99, 99))
                publish.stage(GLIBC, artifacts, assets, {'build_id': 'example'}, 'build-example')
                self.assertEqual(original, uploaded['glibc-x86_64.tar.gz'])
                self.assertEqual(1, sum(args[:5] == ('gh', 'api', '--method', 'POST',
                                                   'repos/example/repo/releases') for args in calls))
                self.assertEqual('release.json', calls[-1][-1].split('/')[-1])
                self.assertEqual('gh', calls[-1][0])
                manifest = json.loads(uploaded['release.json'])
                self.assertEqual({'glibc-' + arch + '.tar.gz' for arch in publish.ARCHES},
                                 set(manifest['sha256']))
                self.assertEqual(''.join(digest + '  ' + name + '\n'
                                        for name, digest in manifest['sha256'].items()),
                                 uploaded['SHA256SUMS'].decode())
                self.assertFalse(any(args[:3] == ('gh', 'release', 'edit') for args in calls))
                names = [Path(args[-1]).name for args in calls
                         if args[:4] == ('gh', 'api', '--method', 'POST') and '/assets?' in args[4]]
                self.assertEqual(['release-notes.md', 'release-notes.html', 'release-notes.json', 'release.json'], names[-4:])
                uploaded['glibc-x86_64.tar.gz'] = b'conflicting bytes'
                with self.assertRaisesRegex(ValueError, 'different bytes'):
                    publish.stage(GLIBC, artifacts, assets, {'build_id': 'example'}, 'build-example')
                # Once staged, notes are kept, never gathered again.
                self.assertEqual(2, gather.call_count)

    def test_publish_uses_renamed_archives_through_completion(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            assets = root / 'assets'
            assets.mkdir()
            digests = {}
            for arch in publish.ARCHES:
                packages = root / arch
                (packages / arch).mkdir(parents=True)
                (packages / arch / 'APKINDEX.tar.gz').write_bytes(b'signed index')
                name = 'glibc-' + arch + '.tar.gz'
                publish.archive(packages, assets / name)
                digests[name] = inputs.sha256(assets / name)
            manifest = {'build_id': 'example', 'source_sha': '1' * 40,
                        'run_number': 1, 'repository': GLIBC.public + 'builds/example',
                        'sha256': digests}
            (assets / 'release.json').write_text(json.dumps(manifest))
            (assets / 'SHA256SUMS').write_text(''.join(
                digest + '  ' + name + '\n' for name, digest in digests.items()))
            def stable(pkg, packages, arch, key, work):
                self.assertTrue((packages / arch / 'APKINDEX.tar.gz').is_file())
                return packages / arch / 'APKINDEX.tar.gz', []
            current = {'id': 42, 'assets': [{'name': p.name} for p in assets.iterdir()]}
            with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'example/repo'}), \
                 patch.object(publish, 'run') as run, \
                 patch.object(publish, 'release', return_value=current), \
                 patch.object(publish, 'download_asset') as download, \
                 patch.object(publish, 'validate') as validate, \
                 patch.object(publish, 'stable', side_effect=stable), \
                 patch.object(publish, 'merge_index'), \
                 patch.object(publish, 'advance_current') as advance, \
                 patch.object(publish, 'fetch', return_value=False), \
                 patch.object(publish, 'upload') as upload:
                publish.publish(GLIBC, assets, {'build_id': 'example'}, 'build-example')
                self.assertEqual(2, validate.call_count)
                for arch in publish.ARCHES:
                    name = 'glibc-' + arch + '.tar.gz'
                    self.assertIn(((GLIBC, assets / name, 'builds/example/' + name), {}),
                                  upload.call_args_list)
                self.assertEqual((GLIBC, manifest, assets / 'release.json'), advance.call_args.args)
                self.assertEqual(('gh', 'api', '--method', 'PATCH',
                                  'repos/example/repo/releases/42',
                                  '--input', str(assets / 'publish-request.json')), run.call_args.args)
                self.assertEqual({'draft': False, 'make_latest': 'false'},
                                 json.loads((assets / 'publish-request.json').read_text()))
                self.assertEqual({p['name'] for p in current['assets']},
                                 {call.args[1] for call in download.call_args_list})
                run.reset_mock()
                upload.reset_mock()
                manifest['sha256'].pop('glibc-aarch64.tar.gz')
                (assets / 'release.json').write_text(json.dumps(manifest))
                with self.assertRaisesRegex(ValueError, 'Both native architectures'):
                    publish.publish(GLIBC, assets, {'build_id': 'example'}, 'build-example')
                upload.assert_not_called()
                run.assert_not_called()

    def test_public_reads_name_themselves_and_treat_only_404_as_absent(self):
        response = MagicMock()
        response.__enter__.return_value.read.return_value = b'body'
        with patch.object(publish.urllib.request, 'urlopen', return_value=response) as urlopen:
            self.assertEqual(b'body', publish.download('https://dist.example/current.json'))
            self.assertIn('werewolf-packages-publish', urlopen.call_args.args[0].get_header('User-agent'))
        missing = publish.urllib.error.HTTPError('https://dist.example/x', 404, 'Not Found', {}, None)
        with patch.object(publish.urllib.request, 'urlopen', side_effect=missing):
            self.assertIsNone(publish.download('https://dist.example/x'))
        refused = publish.urllib.error.HTTPError('https://dist.example/x', 403, 'Forbidden', {}, None)
        with patch.object(publish.urllib.request, 'urlopen', side_effect=refused), \
             patch.object(publish.time, 'sleep'), self.assertRaises(publish.urllib.error.HTTPError):
            publish.download('https://dist.example/x')

    def test_publish_puts_notes_beside_the_snapshot_and_lists_the_release(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            assets = root / 'assets'
            assets.mkdir()
            digests = {}
            for arch in publish.ARCHES:
                (root / arch / arch).mkdir(parents=True)
                (root / arch / arch / 'APKINDEX.tar.gz').write_bytes(b'signed index')
                publish.archive(root / arch, assets / ('glibc-' + arch + '.tar.gz'))
                digests['glibc-' + arch + '.tar.gz'] = inputs.sha256(assets / ('glibc-' + arch + '.tar.gz'))
            (assets / 'release.json').write_text(json.dumps({'build_id': 'example', 'source_sha': '1' * 40,
                                                             'run_number': 1, 'sha256': digests,
                                                             'repository': GLIBC.public + 'builds/example'}))
            (assets / 'SHA256SUMS').write_text('')
            made = notes.build(facts())
            (assets / 'release-notes.json').write_bytes(notes.encode(made))
            (assets / 'release-notes.md').write_text(notes.markdown(made))
            (assets / 'release-notes.html').write_text(notes.page(made))
            order, uploads = [], {}
            def upload(pkg, path, key, immutable=True, content_type=None):
                order.append(key)
                uploads[key] = (Path(path).read_bytes(), immutable, content_type)
            current = {'id': 42, 'assets': [{'name': p.name} for p in assets.iterdir()]}
            with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'example/repo'}), \
                 patch.object(publish, 'run', side_effect=lambda *args: order.append(args[:4])), \
                 patch.object(publish, 'release', return_value=current), \
                 patch.object(publish, 'download_asset'), \
                 patch.object(publish, 'validate'), \
                 patch.object(publish, 'stable', side_effect=lambda pkg, p, a, k, w: (p / a / 'APKINDEX.tar.gz', [])), \
                 patch.object(publish, 'merge_index'), \
                 patch.object(publish, 'advance_current'), \
                 patch.object(publish, 'fetch', return_value=False), \
                 patch.object(publish, 'upload', side_effect=upload):
                publish.publish(GLIBC, assets, {'build_id': 'example'}, 'build-example')
            for name, kind in publish.note_files(GLIBC).items():
                self.assertEqual(((assets / name).read_bytes(), True, kind), uploads['builds/example/' + name])
            listing, immutable, kind = uploads['releases.json']
            self.assertEqual(([notes.entry(made)], False, 'application/json'), (json.loads(listing), immutable, kind))
            self.assertEqual('text/html; charset=utf-8', uploads['index.html'][2])
            self.assertLess(order.index('index.html'), order.index(('gh', 'api', '--method', 'PATCH')))

    def test_control_signature_is_required_and_tampering_is_rejected(self):
        def member(files):
            buffer = io.BytesIO()
            with tarfile.open(fileobj=buffer, mode='w') as archive:
                for name, data in files.items():
                    item = tarfile.TarInfo(name)
                    item.size = len(data)
                    archive.addfile(item, io.BytesIO(data))
            return gzip.compress(buffer.getvalue())
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            private, public = root/'key', root/'key.pub'
            subprocess.run(['openssl', 'genrsa', '-out', str(private), '2048'], check=True, capture_output=True)
            subprocess.run(['openssl', 'rsa', '-in', str(private), '-pubout', '-out', str(public)], check=True, capture_output=True)
            control = member({'APKINDEX': b'P:example\n'})
            (root/'control').write_bytes(control)
            subprocess.run(['openssl', 'dgst', '-sha256', '-sign', str(private), '-out', str(root/'sig'), str(root/'control')], check=True, capture_output=True)
            signed = member({'.SIGN.RSA256.werewolf-packages.rsa.pub': (root/'sig').read_bytes()})
            archive = root/'APKINDEX.tar.gz'
            archive.write_bytes(signed + control)
            self.assertEqual(2, len(publish.authenticated(archive, public)))
            archive.write_bytes(signed + member({'APKINDEX': b'P:attacker\n'}))
            with self.assertRaises(subprocess.CalledProcessError):
                publish.authenticated(archive, public)
            archive.write_bytes(control)
            with self.assertRaises(ValueError):
                publish.authenticated(archive, public)

    def test_stable_index_keeps_every_version_and_each_first_build(self):
        def stanza(version, checksum):
            return f'C:{checksum}\nP:glibc-neutered-2.44\nV:{version}\nA:aarch64'
        previous = {'glibc-neutered-2.44-2.44-r17.apk': stanza('2.44-r17', 'old')}
        current = {'glibc-neutered-2.44-2.44-r17.apk': stanza('2.44-r17', 'rebuilt'),
                   'glibc-neutered-2.44-2.44-r18.apk': stanza('2.44-r18', 'new')}
        merged, added = publish.merge(previous, current)
        self.assertEqual(['glibc-neutered-2.44-2.44-r18.apk'], added)
        self.assertIn('C:old', merged['glibc-neutered-2.44-2.44-r17.apk'])
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            private, public = root/'key', root/'key.pub'
            subprocess.run(['openssl', 'genrsa', '-out', str(private), '2048'], check=True, capture_output=True)
            subprocess.run(['openssl', 'rsa', '-in', str(private), '-pubout', '-out', str(public)], check=True, capture_output=True)
            index = root/'APKINDEX.tar.gz'
            index.write_bytes(publish.signed_index(merged, private))
            self.assertEqual(index.read_bytes(), publish.signed_index(merged, private))
            parts = publish.authenticated(index, public)
            self.assertEqual(merged, publish.entries(parts))
            # One tar stream across both members: no end marker after the signature.
            self.assertEqual(1024, len(parts[0][1]))

    def results(self, passed, failed):
        files = {f'{i}.test-result': b'PASS: case\n' for i in range(passed)}
        files.update({f'f{i}.test-result': b'FAIL: case\n' for i in range(failed)})
        files['usr/share/glibc-testresults/policy-report.json'] = b'{}'
        return files

    def test_current_raw_results_pass_55_but_fail_20(self):
        files = self.results(6355, 345)
        GLIBC.hooks.raw_gate(files, 55)
        with self.assertRaises(ValueError):
            GLIBC.hooks.raw_gate(files, 20)

    def test_ceiling_is_exact_and_unsupported_does_not_dilute_it(self):
        files = self.results(6332, 368)
        GLIBC.hooks.raw_gate(files, 55)
        files['extra.test-result'] = b'FAIL: another\n'
        files.update({f'u{i}.test-result': b'UNSUPPORTED: case\n' for i in range(10000)})
        with self.assertRaises(ValueError):
            GLIBC.hooks.raw_gate(files, 55)

    def test_empty_results_and_missing_policy_evidence_fail(self):
        with self.assertRaises(ValueError):
            GLIBC.hooks.raw_gate(self.results(0, 0), 55)
        files = self.results(100, 0)
        del files['usr/share/glibc-testresults/policy-report.json']
        with self.assertRaises(ValueError):
            GLIBC.hooks.raw_gate(files, 55)

    def test_existing_r2_object_must_have_identical_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'object'
            path.write_bytes(b'new')
            def existing(pkg, key, destination):
                destination.write_bytes(b'old')
                return True
            with patch.object(publish, 'fetch', existing), patch.object(publish, 'aws') as aws:
                with self.assertRaises(ValueError):
                    publish.upload(GLIBC, path, 'builds/example/aarch64/example.apk')
                aws.assert_not_called()

    def test_missing_r2_object_uploads_and_matching_object_skips(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'object'
            path.write_bytes(b'new')
            with patch.object(publish, 'fetch', return_value=False), patch.object(publish, 'aws') as aws:
                aws.return_value.returncode = 0
                publish.upload(GLIBC, path, 'builds/example/aarch64/example.apk')
                aws.assert_called_once()
            def existing(pkg, key, destination):
                destination.write_bytes(path.read_bytes())
                return True
            with patch.object(publish, 'fetch', existing), patch.object(publish, 'aws') as aws:
                publish.upload(GLIBC, path, 'builds/example/aarch64/example.apk')
                aws.assert_not_called()

    def test_auth_error_is_not_treated_as_absent_release(self):
        result = subprocess.CompletedProcess([], 1, '', 'HTTP 403: Forbidden')
        with patch.dict(os.environ, {'GITHUB_REPOSITORY': 'example/repo'}):
            with patch.object(subprocess, 'run', return_value=result):
                with self.assertRaises(RuntimeError):
                    publish.release('build-example')

    def test_status_only_reuses_complete_drafts(self):
        with patch.object(publish, 'release', return_value=None):
            self.assertEqual('build', publish.status('example'))
        with patch.object(publish, 'release', return_value={'draft': True, 'assets': []}):
            self.assertEqual('build', publish.status('example'))
        with patch.object(publish, 'release', return_value={'draft': True, 'assets': [{'name': 'release.json'}]}):
            self.assertEqual('publish', publish.status('example'))
        with patch.object(publish, 'release', return_value={'draft': False, 'assets': []}):
            with self.assertRaises(ValueError):
                publish.status('example')

if __name__ == '__main__':
    unittest.main()
