import copy
from pathlib import Path
import sys
import tempfile
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT / 'scripts'))
import packages
import publish
import refresh
import verify

FIXTURE = yaml.safe_load((Path(__file__).parent / 'fixtures/libffi.yaml').read_text())
PACKAGE = packages.Package('libffi')
COMMIT = PACKAGE.hooks.source_commit(FIXTURE)
LOCK = {'recipe': 'libffi.yaml', 'version': '3.8.0', 'epoch': 2, 'revision': 1, 'source_commit': COMMIT}


class LibffiDerivation(unittest.TestCase):
    def derived(self, change=lambda c: None):
        upstream = copy.deepcopy(FIXTURE)
        change(upstream)
        return upstream, PACKAGE.hooks.derive(PACKAGE, copy.deepcopy(upstream), LOCK)

    def test_follows_wolfis_one_recipe(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'libffi.yaml').write_text(yaml.safe_dump(FIXTURE))
            # A recipe named like a stream is another package, never a candidate.
            other = copy.deepcopy(FIXTURE)
            other['package'].update(name='libffi-4', version='4.0.0')
            (root / 'libffi-4.yaml').write_text(yaml.safe_dump(other))
            self.assertEqual('libffi.yaml', refresh.latest(PACKAGE, root)[0].name)
            pre = copy.deepcopy(FIXTURE)
            pre['package']['version'] = '3.9.0_rc1'
            (root / 'libffi.yaml').write_text(yaml.safe_dump(pre))
            with self.assertRaisesRegex(ValueError, 'No stable Wolfi recipe'):
                refresh.latest(PACKAGE, root)

    def test_static_trampolines_replace_wolfis_and_outrank_it(self):
        upstream, result = self.derived()
        verify.check(PACKAGE, upstream, result)
        configure = next(s for s in result['pipeline'] if s.get('uses') == 'autoconf/configure')
        self.assertIn('--enable-exec-static-tramp', configure['with']['opts'].split())
        self.assertIn('--disable-pax_emutramp', configure['with']['opts'].split())
        self.assertNotIn('--disable-exec-static-tramp', configure['with']['opts'].split())
        self.assertIn(PACKAGE.hooks.built_in(), result['pipeline'])
        self.assertEqual('3.8.0-r1002', PACKAGE.hooks.full_version(LOCK))
        self.assertIn('upstream-origin:libffi:3.8.0-r2', result['package']['dependencies']['provides'])
        self.assertEqual({'libffi', 'libffi-dev', 'libffi-pic-dev', 'libffi-doc'}, publish.package_names(result))
        self.assertEqual(PACKAGE.hooks.PROBED, result['test']['pipeline'][-1]['name'])
        self.assertNotIn('Apply Werewolf policy patches', [s.get('name') for s in result['pipeline']])

    def test_changed_build_paths_stop_for_review(self):
        changes = [
            lambda c: c['pipeline'][0]['with'].update(repository='https://example.com/libffi.git'),
            lambda c: c['pipeline'][1]['with'].update(opts='--disable-builddir'),
            lambda c: c['pipeline'].insert(1, {'uses': 'git-checkout', 'with': {}}),
            lambda c: c['subpackages'].pop(),
            lambda c: c['package'].update(name='libffi3'),
        ]
        for change in changes:
            with self.subTest(change=change), self.assertRaisesRegex(ValueError, 'Review'):
                self.derived(change)

    def test_relaxing_the_derived_build_is_rejected(self):
        upstream, result = self.derived()
        relaxed = copy.deepcopy(result)
        next(s for s in relaxed['pipeline'] if s.get('uses') == 'autoconf/configure')['with']['opts'] = \
            PACKAGE.hooks.CONFIGURE
        with self.assertRaises(AssertionError):
            verify.check(PACKAGE, upstream, relaxed)
        unprobed = copy.deepcopy(result)
        unprobed['test']['pipeline'].pop()
        with self.assertRaises(AssertionError):
            verify.check(PACKAGE, upstream, unprobed)
        unchecked = copy.deepcopy(result)
        unchecked['pipeline'].remove(PACKAGE.hooks.built_in())
        with self.assertRaises(AssertionError):
            verify.check(PACKAGE, upstream, unchecked)


if __name__ == '__main__':
    unittest.main()
