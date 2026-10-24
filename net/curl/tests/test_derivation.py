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
LOCK = dict(recipe='curl.yaml', version='8.23.0', epoch=1, revision=1,
            source_commit=PKG.hooks.source_commit(FIXTURE))


class Curl(unittest.TestCase):
    def test_upstream_suite_stays_in_the_build(self):
        source = copy.deepcopy(FIXTURE)
        suite = {'name': 'Run upstream test suite', 'runs': 'make test\n'}
        source['pipeline'].insert(3, suite)
        result = PKG.hooks.derive(PKG, copy.deepcopy(source), LOCK)
        verify.check(PKG, source, result)
        self.assertEqual(suite, result['pipeline'][3])

    def test_only_cli_headers_and_shared_library_are_published(self):
        result = PKG.hooks.derive(PKG, copy.deepcopy(FIXTURE), LOCK)
        verify.check(PKG, FIXTURE, result)
        self.assertEqual({'curl', 'curl-dev', 'libcurl-openssl4'}, packages.output_names(result))
        self.assertEqual(1001, result['package']['epoch'])
        self.assertIn('curl-hardened=${{package.full-version}}', result['package']['dependencies']['provides'])
        self.assertIn('--without-brotli', result['pipeline'][1]['with']['opts'])
        self.assertNotIn('brotli-dev', result['environment']['contents']['packages'])
        self.assertTrue(any(s.get('uses') == 'test/git-https-auth' for s in result['test']['pipeline']))

    def test_upstream_changes_cannot_silently_add_a_feature(self):
        for mutate in (
            lambda c: c['pipeline'][1]['with'].update(opts=c['pipeline'][1]['with']['opts'] + ' --enable-new-protocol'),
            lambda c: c['pipeline'].insert(1, {'uses': 'patch'}),
            lambda c: c['subpackages'].append({'name': 'curl-extra'}),
            lambda c: c['pipeline'][0]['with'].update(branch='main'),
            lambda c: c['test']['pipeline'].pop(3),
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
