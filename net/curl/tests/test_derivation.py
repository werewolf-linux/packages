import copy
from pathlib import Path
import sys
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT / 'scripts'))
import packages
import verify

PKG = packages.Package('curl')
FIXTURE = yaml.safe_load((Path(__file__).parent / 'fixtures/curl.yaml').read_text())
LOCK = dict(recipe='curl.yaml', version='8.23.0', epoch=2, revision=1,
            source_commit=PKG.hooks.source_commit(FIXTURE))


class Curl(unittest.TestCase):
    def test_only_cli_headers_and_shared_library_are_published(self):
        result = PKG.hooks.derive(PKG, copy.deepcopy(FIXTURE), LOCK)
        verify.check(PKG, FIXTURE, result)
        self.assertEqual({'curl', 'curl-dev', 'libcurl-openssl4'}, packages.output_names(result))
        self.assertEqual(1002, result['package']['epoch'])
        self.assertIn('curl-hardened=${{package.full-version}}', result['package']['dependencies']['provides'])
        self.assertIn('--without-brotli', result['pipeline'][1]['with']['opts'])
        self.assertNotIn('brotli-dev', result['environment']['contents']['packages'])
        self.assertTrue(any(s.get('uses') == 'test/git-https-auth' for s in result['test']['pipeline']))

    def test_upstream_build_tests_are_retained_and_required(self):
        result = PKG.hooks.derive(PKG, copy.deepcopy(FIXTURE), LOCK)
        expected = copy.deepcopy(FIXTURE['pipeline'][3])
        expected['runs'] = expected['runs'].replace(' !2095"', ' !2095 !netrc"')
        self.assertEqual(expected, result['pipeline'][3])
        source = copy.deepcopy(FIXTURE)
        source['pipeline'].pop(3)
        with self.assertRaises(ValueError):
            PKG.hooks.derive(PKG, source, LOCK)
        result['pipeline'].pop(3)
        with self.assertRaises(AssertionError):
            verify.check(PKG, FIXTURE, result)

    def test_test_selection_cannot_hide_failures_for_supported_features(self):
        for replacement in (' !2095"', ' !2095 !netrc !HTTP"'):
            with self.subTest(replacement=replacement):
                result = PKG.hooks.derive(PKG, copy.deepcopy(FIXTURE), LOCK)
                result['pipeline'][3]['runs'] = result['pipeline'][3]['runs'].replace(
                    ' !2095 !netrc"', replacement)
                with self.assertRaises(AssertionError):
                    verify.check(PKG, FIXTURE, result)

    def test_upstream_changes_cannot_silently_add_a_feature(self):
        for mutate in (
            lambda c: c['pipeline'][1]['with'].update(opts=c['pipeline'][1]['with']['opts'] + ' --enable-new-protocol'),
            lambda c: c['pipeline'].insert(1, {'uses': 'patch'}),
            lambda c: c['subpackages'].append({'name': 'curl-extra'}),
            lambda c: c['pipeline'][0]['with'].update(branch='main'),
            lambda c: c['test']['pipeline'].pop(3),
            lambda c: c['pipeline'][3].update(runs=c['pipeline'][3]['runs'].replace('!2095', '!2096')),
        ):
            with self.subTest(mutate=mutate):
                source = copy.deepcopy(FIXTURE)
                mutate(source)
                with self.assertRaises(ValueError):
                    PKG.hooks.derive(PKG, source, LOCK)

    def test_verifier_rejects_missing_tls_authentication_test(self):
        result = PKG.hooks.derive(PKG, copy.deepcopy(FIXTURE), LOCK)
        result['test']['pipeline'] = [s for s in result['test']['pipeline'] if s.get('uses') != 'test/git-https-auth']
        with self.assertRaises(AssertionError):
            verify.check(PKG, FIXTURE, result)


if __name__ == '__main__':
    unittest.main()
