import hashlib
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import build
import common
from test_publish import unsigned


class Builder(unittest.TestCase):
    def test_only_native_linux_is_accepted(self):
        with patch.object(sys, 'argv', ['build.py', 'example', '--arch', 'aarch64']), \
             patch.object(build.platform, 'system', return_value='Darwin'), \
             patch.object(build.subprocess, 'run') as run:
            with self.assertRaisesRegex(ValueError, 'native Linux'):
                build.main()
        run.assert_not_called()

    def test_builder_drops_credentials_and_produces_a_validated_record(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'example').mkdir()
            (root / 'example/recipe.yaml').touch()
            (root / 'tools.lock.json').write_text('{"melange":{"version":"0.62.1"}}')
            melange = str(root / 'tools/melange')
            meta = dict(context='example', names=['example'], version='1-r0', inputs='a' * 64)
            calls = []
            def run(args, **kwargs):
                calls.append((args, kwargs))
                out = Path(args[args.index('--out-dir') + 1]) / 'aarch64'
                out.mkdir()
                unsigned(out / 'example-1-r0.apk', 'example', 'aarch64')
            def capture(args, **kwargs):
                if args == [melange, 'version']:
                    return 'GitVersion: v0.62.1'
                self.assertEqual('git', args[0])
                return '1700000000'
            old = Path.cwd()
            try:
                with patch.object(sys, 'argv', ['build.py', 'example', '--arch', 'aarch64']), \
                     patch.object(common, 'ROOT', root), patch.object(common, 'metadata', return_value=meta), \
                     patch.object(build.platform, 'system', return_value='Linux'), \
                     patch.object(build.platform, 'machine', return_value='aarch64'), \
                     patch.object(build.subprocess, 'run', side_effect=run), \
                     patch.object(build.subprocess, 'check_output', side_effect=capture), \
                     patch.dict(os.environ, dict(MELANGE=melange, GITHUB_SHA='b' * 40, GITHUB_RUN_ID='123', GH_TOKEN='PRIVATE_SENTINEL',
                         PACKAGES_APK_SIGNING_KEY='PRIVATE_SENTINEL', AWS_SECRET_ACCESS_KEY='PRIVATE_SENTINEL')):
                    build.main()
            finally:
                os.chdir(old)
            self.assertEqual(1, len(calls))
            args, kwargs = calls[0]
            self.assertEqual(melange, args[0])
            self.assertNotIn('--signing-key', args)
            self.assertNotIn('PRIVATE_SENTINEL', str(kwargs['env']))
            self.assertEqual('1700000000', kwargs['env']['SOURCE_DATE_EPOCH'])
            import json
            record = json.loads((root / 'build/artifacts/example/aarch64/build.json').read_text())
            self.assertEqual('123', record['run_id'])
            self.assertEqual('a' * 64, record['inputs'])
