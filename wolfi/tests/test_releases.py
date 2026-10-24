"""A newer upstream release keeps the captured recipe and every hardening gate."""
from copy import deepcopy
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
import yaml

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import derive
import fixture
import inputs
import packages
import refresh
import releases
import verify

ROOT = Path(__file__).resolve().parents[2]


class Upstream(unittest.TestCase):
    def test_all_production_overlays_have_independent_unrestricted_sources(self):
        for name in packages.names():
            pkg = packages.Package(name)
            policy = json.loads((pkg.dir / 'metadata.json').read_text())['update']['upstream']
            self.assertNotIn('series', policy)
            self.assertNotIn('stream', pkg.policy)

    def test_new_source_derives_and_verifies_against_unchanged_python_and_jdk_baselines(self):
        for name, file, version in [('python', 'python-3.15.yaml', '3.15.1'),
                                     ('openjdk', 'openjdk-27.0.1.yaml', '27.0.1.0.3')]:
            with self.subTest(package=name), tempfile.TemporaryDirectory() as temporary:
                pkg = packages.Package(name)
                baseline = yaml.safe_load((pkg.dir / 'tests/fixtures' / file).read_text())
                before = deepcopy(baseline)
                lock = dict(recipe=baseline['package']['name'] + '.yaml', version=version,
                            source_commit='b' * 40, epoch=0, revision=pkg.policy['revision'],
                            release={'version':version, 'repository':'python/cpython' if name=='python' else 'openjdk/jdk27u'})
                config = releases.effective(pkg, baseline, lock)
                derived = pkg.hooks.derive(pkg, deepcopy(config), lock)
                verify.check(pkg, config, derived)
                self.assertEqual(before, baseline)
                self.assertEqual(version, derived['package']['version'])
                fetch = next(s for s in derived['pipeline'] if s.get('name') == 'Fetch the locked source commit')
                self.assertIn('b' * 40, fetch['runs'])
                for section in [derived['package']] + derived['subpackages']:
                    self.assertTrue(any(p.startswith('upstream-origin:') and version in p
                                        for p in section['dependencies']['provides']))
                    self.assertFalse(any(p.startswith('wolfi-origin:') for p in section['dependencies']['provides']))

    def test_future_major_selects_available_baseline_but_not_an_unreleased_stream(self):
        with tempfile.TemporaryDirectory() as temporary:
            pkg = fixture.package(temporary)
            checkout = Path(temporary) / 'os'
            checkout.mkdir()
            for stream, version in [('27', '27.0.1'), ('28', '28.0.0_git20261020')]:
                (checkout / ('lang-' + stream + '.yaml')).write_text(yaml.safe_dump(
                    {'package': {'name':'lang-' + stream, 'version':version}, 'vars':{'commit':'a'*40}}))
            path, _ = refresh.latest(pkg, checkout, {'version':'27.0.2'})
            self.assertEqual('lang-27.yaml', path.name)
            path, _ = refresh.latest(pkg, checkout, {'version':'29.0.0'})
            self.assertEqual('lang-28.yaml', path.name)

    def test_upstream_target_is_hashed_and_not_just_the_recipe_baseline(self):
        lock=dict(recipe='lang-27.yaml',source_commit='a'*40,revision=1,sha256={},mode={},policy_sha256={},
                  version='27.0.1',epoch=0,release={'version':'27.0.1','commit':'a'*40})
        before=inputs.identity(lock)
        self.assertNotEqual(before,inputs.identity(dict(lock,version='27.0.2',release={'version':'27.0.2','commit':'a'*40})))

    def test_new_major_can_use_an_older_recipe_without_retaining_its_package_name(self):
        pkg=packages.Package('openjdk')
        baseline=yaml.safe_load((pkg.dir/'tests/fixtures/openjdk-27.0.1.yaml').read_text())
        lock=dict(recipe='openjdk-27.yaml',effective_recipe='openjdk-28.yaml',version='28.0.0',
                  source_commit='b'*40,epoch=0,revision=pkg.policy['revision'],
                  release={'version':'28.0.0','repository':'openjdk/jdk28u'})
        effective=releases.effective(pkg,baseline,lock)
        derived=pkg.hooks.derive(pkg,deepcopy(effective),lock)
        verify.check(pkg,effective,derived)
        self.assertEqual('openjdk-28',derived['package']['name'])
        self.assertIn('upstream-origin:openjdk-28:28.0.0-r0',derived['package']['dependencies']['provides'])
        self.assertEqual('openjdk-27',baseline['package']['name'])

    def test_failed_release_discovery_preserves_previous_inputs(self):
        with tempfile.TemporaryDirectory() as temporary:
            pkg=fixture.package(temporary)
            pkg.inputs.mkdir(parents=True)
            marker=pkg.inputs/'upstream.lock.json'
            marker.write_text('last usable capture')
            with patch.object(refresh.releases,'discover',side_effect=ValueError('upstream unavailable')):
                with self.assertRaisesRegex(ValueError,'upstream unavailable'):
                    refresh.capture(pkg,Path(temporary)/'os')
            self.assertEqual('last usable capture',marker.read_text())


if __name__ == '__main__':
    unittest.main()
