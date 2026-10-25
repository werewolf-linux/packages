"""Manual recovery can refresh one package without scheduling unrelated builds."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import yaml

WORKFLOW = Path(__file__).resolve().parents[2] / '.github/workflows/wolfi-refresh.yml'


class RefreshDispatch(unittest.TestCase):
    def test_selector_reaches_discovery_as_one_literal_package_argument(self):
        workflow = yaml.safe_load(WORKFLOW.read_text())
        step = next(step for step in workflow['jobs']['discover']['steps']
                    if step.get('id') == 'discover')
        self.assertEqual('${{ inputs.package }}', step['env']['PACKAGE'])
        for package in ('', 'postgresql', '--local', 'postgresql; touch unexpected'):
            with self.subTest(package=package), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                interpreter = root / 'build/venv/bin/python'
                interpreter.parent.mkdir(parents=True)
                interpreter.write_text('#!' + sys.executable + '\n' +
                    'import json, os, pathlib, sys\n'
                    'pathlib.Path(os.environ["RECORDED_ARGS"]).write_text(json.dumps(sys.argv[1:]))\n')
                interpreter.chmod(0o755)
                output = root / 'args.json'
                subprocess.run(['bash', '-eu', '-c', step['run']], cwd=root, check=True,
                               env={**os.environ, 'PACKAGE': package, 'RECORDED_ARGS': str(output)})
                expected = ['scripts/discover.py'] + (['--', package] if package else [])
                self.assertEqual(expected, json.loads(output.read_text()))
                self.assertFalse((root / 'unexpected').exists())


if __name__ == '__main__':
    unittest.main()
