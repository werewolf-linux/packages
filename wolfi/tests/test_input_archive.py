import json
import os
from types import SimpleNamespace
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'scripts'))
import inputs

class InputTransport(unittest.TestCase):
    def test_archive_preserves_executable_and_changed_modes_fail_verification(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source, transport, restored = root/'inputs', root/'transport', root/'restored'
            source.mkdir()
            restored.mkdir()
            helper = source/'compatibility-gate'
            helper.write_text('#!/bin/sh\nexit 0\n')
            helper.chmod(0o755)
            lock = {'recipe': 'fixture.yaml', 'source_commit': '1'*40, 'revision': 6,
                    'sha256': {helper.name: inputs.sha256(helper)},
                    'mode': inputs.file_modes(source), 'policy_sha256': {}}
            lock['build_id'] = inputs.identity(lock)
            (source/'upstream.lock.json').write_text(json.dumps(lock))
            subprocess.run(['tar', '-czf', str(root/'wolfi-inputs.tar.gz'), '-C', str(source), '.'], check=True, env=dict(os.environ, COPYFILE_DISABLE='1'))
            with zipfile.ZipFile(root/'artifact.zip', 'w') as archive:
                archive.write(root/'wolfi-inputs.tar.gz', 'wolfi-inputs.tar.gz')
            with zipfile.ZipFile(root/'artifact.zip') as archive:
                archive.extractall(transport)
            with tarfile.open(transport/'wolfi-inputs.tar.gz') as archive:
                archive.extractall(restored, filter='data')
            subprocess.run([str(restored/helper.name)], check=True)
            with patch.object(inputs, 'policy_hashes', return_value={}):
                pkg = SimpleNamespace(inputs=restored, root=root)
                inputs.verify(pkg)
                (restored/helper.name).chmod(0o644)
                with self.assertRaisesRegex(ValueError, 'file modes'):
                    inputs.verify(pkg)
            different = dict(lock, mode={helper.name: 0o644})
            self.assertNotEqual(lock['build_id'], inputs.identity(different))

if __name__ == '__main__':
    unittest.main()
