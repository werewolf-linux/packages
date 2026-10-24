import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest import mock

SCRIPT = Path(__file__).resolve().parents[1] / 'scripts/setup-runner.py'
spec = importlib.util.spec_from_file_location('runner_setup', SCRIPT)
runner_setup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner_setup)

TEMPLATE = '''abi <abi/5.0>,
profile bwrap /usr/bin/bwrap flags=(attach_disconnected,mediate_deleted) {
  allow capability,
  allow file rwlkm /{**,},
  allow pix /** -> &bwrap//&unpriv_bwrap,
  include if exists <local/bwrap-userns-restrict>
}
profile unpriv_bwrap flags=(attach_disconnected,mediate_deleted) {
  allow file rwlkm /{**,},
  allow pix /** -> &unpriv_bwrap,
  audit deny capability,
  include if exists <local/unpriv_bwrap>
}
'''


class RunnerProfile(unittest.TestCase):
    def test_runner_without_ubuntu_profile_keeps_system_confinement(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            executable = root / 'build/tools/bwrap'
            with mock.patch.object(runner_setup, 'ROOT', root), \
                 mock.patch.object(runner_setup, 'PROFILE', root / 'missing-profile'), \
                 mock.patch.object(runner_setup.os, 'geteuid', return_value=0), \
                 mock.patch.object(runner_setup.shutil, 'copyfile',
                                   side_effect=lambda source, target: target.write_bytes(b'runner')) as copy, \
                 mock.patch.object(runner_setup.subprocess, 'run') as run:
                runner_setup.main()
            copy.assert_called_once_with('/usr/bin/bwrap', executable)
            self.assertEqual(0o755, executable.stat().st_mode & 0o777)
            self.assertFalse(executable.with_suffix('.apparmor').exists())
            run.assert_not_called()

    def test_capability_is_scoped_to_the_private_runner_child(self):
        executable = Path('/home/runner/work/project/build/tools/bwrap')
        result = runner_setup.profile(TEMPLATE, executable)
        self.assertIn('profile hardened_packages_bwrap "' + str(executable.resolve()) + '"', result)
        child = result.split('profile hardened_packages_bwrap_child flags=', 1)[1]
        self.assertIn('allow capability sys_chroot,', child)
        self.assertIn('allow capability setuid,', child)
        self.assertIn('allow capability setgid,', child)
        self.assertNotIn('allow capability,', child)
        self.assertNotIn('unconfined', result)
        self.assertNotIn('local/', result)
        self.assertNotIn('/usr/bin/bwrap', result)
        self.assertIn('audit deny capability,', TEMPLATE)

    def test_new_child_capabilities_and_unknown_profiles_fail_closed(self):
        for template in (TEMPLATE.replace('audit deny capability,', 'allow capability,'),
                         TEMPLATE.replace('audit deny capability,', 'audit deny capability,\nallow capability sys_admin,'),
                         TEMPLATE.replace('audit deny capability,', ''),
                         TEMPLATE.replace('profile bwrap', 'profile new_bwrap'),
                         TEMPLATE.replace('attach_disconnected', 'unconfined')):
            with self.subTest(template=template), self.assertRaises(ValueError):
                runner_setup.profile(template, Path('/build/tools/bwrap'))

    def test_attachment_rejects_policy_metacharacters(self):
        for path in ('/build/*/bwrap', '/build/{a,b}/bwrap', '/build/"/bwrap', '/build/space name/bwrap'):
            with self.subTest(path=path), self.assertRaises(ValueError):
                runner_setup.profile(TEMPLATE, Path(path))


if __name__ == '__main__':
    unittest.main()
