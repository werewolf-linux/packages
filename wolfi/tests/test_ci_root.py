import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / 'scripts/ci-root'


class CIRoot(unittest.TestCase):
    def run_helper(self, environment, uid=1000):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            for tool in ('sudo', 'unshare'):
                executable = work / tool
                executable.write_text('#!/bin/sh\nprintf "%s\\n" "' + tool + '" "$@"\n')
                executable.chmod(0o755)
            executable = work / 'id'
            executable.write_text('#!/bin/sh\nprintf "%s\\n" "' + str(uid) + '"\n')
            executable.chmod(0o755)
            env = {**os.environ, 'PATH': str(work)}
            env.pop('RUNNER_ENVIRONMENT', None)
            if environment is not None:
                env['RUNNER_ENVIRONMENT'] = environment
            return subprocess.run(['/bin/sh', str(SCRIPT), 'probe', 'argument with spaces'],
                                  env=env, text=True, capture_output=True)

    def test_self_hosted_uses_subordinate_ids_without_sudo(self):
        result = self.run_helper('self-hosted')
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(['unshare', '--user', '--map-auto', '--map-root-user',
                          'probe', 'argument with spaces'], result.stdout.splitlines())

    def test_hosted_uses_noninteractive_sudo(self):
        result = self.run_helper('github-hosted')
        self.assertEqual(0, result.returncode, result.stderr)
        self.assertEqual(['sudo', '-n', 'probe', 'argument with spaces'], result.stdout.splitlines())

    def test_self_hosted_agent_cannot_run_as_host_root(self):
        result = self.run_helper('self-hosted', uid=0)
        self.assertNotEqual(0, result.returncode)
        self.assertEqual('', result.stdout)
        self.assertIn('dedicated non-root account', result.stderr)

    def test_unknown_environment_cannot_fall_back_to_host_root(self):
        for environment in (None, '', 'other'):
            with self.subTest(environment=environment):
                result = self.run_helper(environment)
                self.assertNotEqual(0, result.returncode)
                self.assertEqual('', result.stdout)


if __name__ == '__main__':
    unittest.main()
