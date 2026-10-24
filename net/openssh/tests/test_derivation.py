import copy
from pathlib import Path
import sys
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT / 'scripts'))
import packages
import patching
import verify

PKG = packages.Package('openssh')
FIXTURE = yaml.safe_load((Path(__file__).parent / 'fixtures/openssh.yaml').read_text())
LOCK = dict(recipe='openssh.yaml', version='10.6_p1', epoch=0, revision=1,
            source_commit=PKG.hooks.source_commit(FIXTURE))


def configure(config):
    return next(s for s in config['pipeline'] if s.get('uses') == 'autoconf/configure')['with']['opts'].split()


class OpenSSH(unittest.TestCase):
    def derived(self, change=lambda c: None):
        upstream = copy.deepcopy(FIXTURE)
        change(upstream)
        return upstream, PKG.hooks.derive(PKG, copy.deepcopy(upstream), LOCK)

    def test_drops_pam_selinux_zlib_and_pkcs11_and_outranks_wolfi(self):
        upstream, result = self.derived()
        verify.check(PKG, upstream, result)
        options = configure(result)
        for option in ('--without-pam', '--without-selinux', '--without-zlib', '--disable-pkcs11'):
            self.assertIn(option, options)
        for option in ('--with-pam', '--with-pam-service=sshd', '--with-selinux', '--without-openssl'):
            self.assertNotIn(option, options)
        build = result['environment']['contents']['packages']
        for name in ('libselinux-dev', 'linux-pam-dev', 'zlib-dev'):
            self.assertNotIn(name, build)
        self.assertIn('openssl-hardened-${{vars.openssl-version}}-dev', build)
        self.assertEqual(1000, result['package']['epoch'])
        self.assertEqual('10.6_p1-r1000', PKG.hooks.full_version(LOCK))
        names = packages.output_names(result)
        self.assertNotIn('openssh-pam-config', names)
        self.assertLessEqual({'openssh', 'openssh-server', 'openssh-client', 'openssh-keygen',
                              'openssh-sftp-server', 'openssh-server-config'}, names)
        for section in [result['package']] + result['subpackages']:
            self.assertIn('upstream-origin:openssh:10.6_p1-r0', section['dependencies']['provides'])
        self.assertEqual(PKG.hooks.PROBED, result['test']['pipeline'][-1]['name'])

    def test_wolfis_patch_and_tests_are_retained_and_ours_follow(self):
        upstream, result = self.derived()
        self.assertEqual(upstream['pipeline'][1], result['pipeline'][1])
        self.assertEqual(patching.step(PKG.hooks.PATCHES), result['pipeline'][2])
        self.assertEqual(['0001-no-pty-reopen.patch', '0002-no-default-listen-in-session.patch'],
                         (PKG.dir / 'patches/series').read_text().split())
        self.assertEqual(upstream['test']['pipeline'], result['test']['pipeline'][:-1])

    def test_upstream_changes_stop_for_review(self):
        changes = (
            lambda c: c['pipeline'][3]['with'].update(opts=c['pipeline'][3]['with']['opts'] + ' --with-kerberos5'),
            lambda c: c['pipeline'].insert(4, {'uses': 'patch'}),
            lambda c: c['pipeline'][0]['with'].update(repository='https://example.com/openssh'),
            lambda c: c['subpackages'].append({'name': 'openssh-extra'}),
            lambda c: c['environment']['contents']['packages'].remove('zlib-dev'),
            lambda c: c['package'].update(name='openssh-10'),
        )
        for change in changes:
            with self.subTest(change=change), self.assertRaisesRegex(ValueError, 'Review'):
                self.derived(change)

    def test_relaxing_the_derived_build_is_rejected(self):
        upstream, result = self.derived()
        relaxations = (
            lambda r: r['pipeline'][4]['with'].update(opts=r['pipeline'][4]['with']['opts'] + ' --with-pam'),
            lambda r: r['pipeline'].pop(2),
            lambda r: r['environment']['contents']['packages'].append('linux-pam-dev'),
            lambda r: r['test']['pipeline'].pop(),
            lambda r: r['subpackages'].append(next(
                s for s in upstream['subpackages'] if s['name'] == 'openssh-pam-config')),
        )
        for relax in relaxations:
            relaxed = copy.deepcopy(result)
            relax(relaxed)
            with self.subTest(relax=relax), self.assertRaises(AssertionError):
                verify.check(PKG, upstream, relaxed)


if __name__ == '__main__':
    unittest.main()
