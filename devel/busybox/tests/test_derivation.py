import copy
from pathlib import Path
import sys
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT / 'scripts'))
import packages
import verify

PKG = packages.Package('busybox')
FIXTURE = yaml.safe_load((Path(__file__).parent / 'fixtures/busybox.yaml').read_text())
LOCK = dict(recipe='busybox.yaml', version='1.38.0', epoch=3, revision=1,
            source_commit=PKG.hooks.source_commit(FIXTURE))


class Busybox(unittest.TestCase):
    def derived(self, change=lambda c: None):
        upstream = copy.deepcopy(FIXTURE)
        change(upstream)
        return upstream, PKG.hooks.derive(PKG, copy.deepcopy(upstream), LOCK)

    def test_every_variant_prefers_applets_and_outranks_wolfi(self):
        upstream, result = self.derived()
        verify.check(PKG, upstream, result)
        steps = PKG.hooks.configures(result)
        self.assertEqual(3, len(steps))
        for step in steps:
            self.assertIn("CONFIG_FEATURE_PREFER_APPLETS=y/' .config", step['runs'])
            self.assertTrue(step['runs'].endswith(PKG.hooks.PREFER))
        self.assertEqual('build', result['pipeline'][0]['working-directory'])
        self.assertEqual('1.38.0-r1003', PKG.hooks.full_version(LOCK))
        self.assertEqual({'busybox', 'busybox-full', 'busybox-static'}, packages.output_names(result))
        for section in [result['package']] + result['subpackages']:
            self.assertIn('upstream-origin:busybox:1.38.0-r3', section['dependencies']['provides'])
        for section in [result] + result['subpackages']:
            self.assertEqual(PKG.hooks.PROBED, section['test']['pipeline'][-1]['name'])

    def test_prepare_finds_wolfis_patches_beside_the_recipe(self):
        names = PKG.hooks.upstream_patches(FIXTURE)
        self.assertEqual(14, len(names))
        self.assertTrue(all(n.endswith('.patch') and '/' not in n for n in names))
        moved = copy.deepcopy(FIXTURE)
        moved['pipeline'][1]['with']['patches'] = 'sub/dir.patch'
        with self.assertRaisesRegex(ValueError, 'Review'):
            PKG.hooks.upstream_patches(moved)

    def test_upstream_changes_stop_for_review(self):
        changes = (
            lambda c: c['pipeline'][0]['with'].update(repository='https://example.com/busybox'),
            lambda c: c['subpackages'].append({'name': 'busybox-extra'}),
            lambda c: c['package'].update(name='busybox2'),
            # Wolfi enables it itself: this overlay may go.
            lambda c: PKG.hooks.configures(c)[0].update(
                runs='cp ../busyboxconfig .config\necho CONFIG_FEATURE_PREFER_APPLETS=y >> .config\n'),
            lambda c: c['subpackages'][0]['pipeline'].pop(0),
        )
        for change in changes:
            with self.subTest(change=change), self.assertRaisesRegex(ValueError, 'Review'):
                self.derived(change)

    def test_relaxing_the_derived_build_is_rejected(self):
        upstream, result = self.derived()
        relaxations = (
            lambda r: PKG.hooks.configures(r)[1].update(runs='cp ../busyboxconfig-full .config\n'),
            lambda r: r['test']['pipeline'].pop(),
            lambda r: r['subpackages'][1]['test']['pipeline'].pop(),
            lambda r: r['pipeline'][1]['with'].update(patches='../other.patch'),
        )
        for relax in relaxations:
            relaxed = copy.deepcopy(result)
            relax(relaxed)
            with self.subTest(relax=relax), self.assertRaises(AssertionError):
                verify.check(PKG, upstream, relaxed)


if __name__ == '__main__':
    unittest.main()
