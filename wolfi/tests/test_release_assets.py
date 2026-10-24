import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import publish

class ReleaseAssets(unittest.TestCase):
    def setUp(self):
        self.current = {'id': 42, 'draft': True, 'tag_name': 'node/build-example',
            'upload_url': 'https://uploads.github.com/repos/test/runtime/releases/42/assets{?name,label}',
            'assets': [{'id': 77, 'name': 'node-aarch64.tar.gz'}]}
        self.env = patch.dict(os.environ, {'GITHUB_REPOSITORY': 'test/runtime', 'GITHUB_SHA': '1' * 40})
        self.env.start()
        self.addCleanup(self.env.stop)

    def test_download_draft_by_asset_id_preserves_binary(self):
        def api(argv, **kwargs):
            self.assertEqual(argv, ['gh', 'api', '-H', 'Accept: application/octet-stream',
                'repos/test/runtime/releases/assets/77'])
            kwargs['stdout'].write(b'\x00\xffsigned archive')
            return subprocess.CompletedProcess(argv, 0)
        with tempfile.TemporaryDirectory() as d, patch.object(publish.subprocess, 'run', side_effect=api):
            destination = Path(d) / 'archive'
            publish.download_asset(self.current, 'node-aarch64.tar.gz', destination)
            self.assertEqual(destination.read_bytes(), b'\x00\xffsigned archive')

    def test_upload_draft_by_release_id_and_escaped_name(self):
        with patch.object(publish, 'run', return_value=json.dumps({'id': 88})) as run:
            self.assertEqual(publish.upload_asset(self.current, Path('/tmp/notes #1.md')), {'id': 88})
            run.assert_called_once_with('gh', 'api', '--method', 'POST',
                'https://uploads.github.com/repos/test/runtime/releases/42/assets?name=notes+%231.md',
                '-H', 'Content-Type: application/octet-stream', '--input', '/tmp/notes #1.md')

    def test_missing_or_duplicate_asset_fails_before_download(self):
        with patch.object(publish.subprocess, 'run') as run:
            for assets in ([], self.current['assets'] * 2):
                with self.subTest(assets=assets), self.assertRaisesRegex(ValueError, 'Missing or duplicate'):
                    publish.download_asset({**self.current, 'assets': assets}, 'node-aarch64.tar.gz', Path('/tmp/unused'))
            run.assert_not_called()

    def test_existing_tag_omits_the_unused_target_after_verifying_source(self):
        ref = {'object': {'type': 'commit', 'sha': '1' * 40}}
        with patch.object(publish, 'run', return_value=json.dumps(ref)) as run:
            self.assertEqual(publish.release_target('node/build-example'), {})
            run.assert_called_once_with('gh', 'api', 'repos/test/runtime/git/ref/tags/node/build-example')

    def test_missing_tag_keeps_the_exact_build_commit(self):
        missing = subprocess.CalledProcessError(1, ['gh', 'api'], output='{"status":"404"}')
        with patch.object(publish, 'run', side_effect=missing):
            self.assertEqual(publish.release_target('node/build-example'), {'target_commitish': '1' * 40})

    def test_wrong_or_indirect_tag_fails_before_creating_a_release(self):
        for kind, sha in [('commit', '2' * 40), ('tag', '1' * 40)]:
            with self.subTest(kind=kind), patch.object(publish, 'run', return_value=json.dumps(
                    {'object': {'type': kind, 'sha': sha}})) as run:
                with self.assertRaisesRegex(ValueError, 'validated source'):
                    publish.release_target('node/build-example')
                self.assertEqual(run.call_count, 1)

    def test_ref_access_failure_is_not_treated_as_a_missing_tag(self):
        denied = subprocess.CalledProcessError(1, ['gh', 'api'], output='{"status":"403"}')
        with patch.object(publish, 'run', side_effect=denied), self.assertRaises(subprocess.CalledProcessError):
            publish.release_target('node/build-example')
