"""Recovery must run the new publisher against an unchanged historical builder."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
import yaml

import fixture
import inputs

ROOT = Path(__file__).resolve().parents[1]


class RecoveryPolicy(unittest.TestCase):
    def test_workflow_replacement_preserves_policy_and_supports_the_old_builder(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            validated = root / 'validated/wolfi'
            shutil.copytree(ROOT / 'scripts', validated / 'scripts',
                            ignore=shutil.ignore_patterns('__pycache__'))
            for name in inputs.SHARED:
                destination = validated / name
                destination.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(ROOT / name, destination)
            # The API used by the original successful builds, before output_names.
            shutil.copy2(ROOT / 'tests/fixtures/legacy-packages.py', validated / 'scripts/packages.py')
            pkg = fixture.package(validated)
            pkg.inputs.mkdir(parents=True)
            (pkg.inputs / 'upstream.yaml').write_text('package: {name: fixture}\n')
            lock = {'recipe': 'fixture.yaml', 'source_commit': 'a' * 40, 'revision': 1,
                    'sha256': {'upstream.yaml': inputs.sha256(pkg.inputs / 'upstream.yaml')},
                    'mode': inputs.file_modes(pkg.inputs),
                    'policy_sha256': inputs.policy_hashes(pkg, validated)}
            lock['build_id'] = inputs.identity(lock)
            (pkg.inputs / 'upstream.lock.json').write_text(json.dumps(lock))
            self.assertEqual(inputs.verify(pkg), lock)

            publisher = root / 'publisher/wolfi/scripts'
            publisher.mkdir(parents=True)
            for name in ('publish.py', 'packages.py'):
                shutil.copy2(ROOT / 'scripts' / name, publisher / name)
            workflow = yaml.safe_load((ROOT.parent / '.github/workflows/wolfi-refresh.yml').read_text())
            step = next(s for s in workflow['jobs']['recover']['steps']
                        if s.get('name') == 'Validate and stage the original signed artifacts with the repaired publisher')
            copies = [line for line in step['run'].splitlines() if line.startswith('cp ')]
            self.assertEqual(len(copies), 1)
            subprocess.run(['sh', '-eu', '-c', copies[0]], cwd=validated, check=True,
                           capture_output=True)
            # Exercise the real CLI verification before the remote staging boundary.
            code = """import sys
from pathlib import Path
sys.path.insert(0, str(Path('scripts').resolve()))
import publish
publish.stage = lambda pkg, artifacts, assets, lock, tag: print('verified ' + tag)
sys.argv = ['publish.py', 'stage', 'fixture']
publish.main()
"""
            result = subprocess.run([sys.executable, '-c', code], cwd=validated,
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout.strip(), 'verified fixture/build-' + lock['build_id'])
