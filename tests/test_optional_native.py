import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


@unittest.skipUnless(shutil.which('node'), 'Node is required')
class OptionalNative(unittest.TestCase):
    def test_pruning_preserves_workflow_backends_and_removes_nested_profilers(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copyfile(ROOT / 'www/n8n/package-lock.json', root / 'package-lock.json')
            removed = ['@sentry/node-native', '@sentry/node-native-stacktrace', '@sentry/profiling-node',
                       '@parcel/watcher', '@parcel/watcher-linux-arm64-glibc',
                       'n8n-core/node_modules/@sentry/profiling-node']
            kept = ['sqlite3', 'isolated-vm', '@napi-rs/canvas', 'n8n', 'n8n-core', '@sentry/node']
            for name in removed + kept:
                package = root / 'node_modules' / name
                package.mkdir(parents=True, exist_ok=True)
                (package / 'package.json').write_text(json.dumps({'name': name.split('node_modules/')[-1]}))
                (package / 'native.node').write_bytes(b'native code')
            subprocess.run(['node', str(ROOT / 'www/n8n/prune-optional.cjs')], cwd=root, check=True)
            self.assertTrue(all(not (root / 'node_modules' / name).exists() for name in removed))
            self.assertTrue(all((root / 'node_modules' / name / 'native.node').is_file() for name in kept))

    def test_new_consumer_prevents_pruning(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            lock = {'packages': {'node_modules/workflow': {'dependencies': {'@parcel/watcher': '2'}}}}
            (root / 'package-lock.json').write_text(json.dumps(lock))
            result = subprocess.run(['node', str(ROOT / 'www/n8n/prune-optional.cjs')], cwd=root, capture_output=True)
            self.assertNotEqual(0, result.returncode)
            self.assertIn(b'review new consumer', result.stderr)
