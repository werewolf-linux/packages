import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch
import yaml

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import inputs
import refresh
import fixture


def recipe(checkout, stream, version=None, commit='1' * 40, name=None):
    config = {'package': {'name': name or 'lang-' + stream, 'version': version or stream + '.0', 'epoch': 3},
              'vars': {'commit': commit}, 'pipeline': []}
    (checkout / ('lang-' + stream + '.yaml')).write_text(yaml.safe_dump(config))


class Latest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        self.checkout = self.root / 'os'
        self.checkout.mkdir()

    def tearDown(self):
        self.directory.cleanup()

    def latest(self, hooks='', policy=None):
        return refresh.latest(fixture.package(self.root, hooks, policy), self.checkout)[0].name

    def test_the_newest_stream_wins_numerically_not_wolfis_default(self):
        for stream in ['3.9', '3.10', '3.13', '3.14']:
            recipe(self.checkout, stream)
        self.assertEqual('lang-3.14.yaml', self.latest())

    def test_single_number_streams_order_numerically(self):
        for stream in ['21', '25', '26', '27', '9']:
            recipe(self.checkout, stream)
        self.assertEqual('lang-27.yaml', self.latest())

    def test_a_snapshot_of_a_released_branch_counts_as_stable(self):
        recipe(self.checkout, '3.14', '3.14.8_git20261014')
        recipe(self.checkout, '3.15', '3.15.0_git20261010')
        self.assertEqual('lang-3.15.yaml', self.latest())

    def test_pre_releases_are_never_chosen(self):
        recipe(self.checkout, '3.14')
        for stream, version in [('3.15', '3.15.0_rc1'), ('3.16', '3.16.0_alpha2'), ('3.17', '3.17.0_beta1')]:
            recipe(self.checkout, stream, version)
        self.assertEqual('lang-3.14.yaml', self.latest())

    def test_a_recipe_must_be_its_own_streams(self):
        recipe(self.checkout, '2.9')
        recipe(self.checkout, '2.10', version='2.9.0')       # a version from another stream
        recipe(self.checkout, '2.11', name='lang-other')     # another package's name
        (self.checkout / 'lang-crossver-check.yaml').write_text('package: {name: lang-crossver-check}\n')
        self.assertEqual('lang-2.9.yaml', self.latest())

    def test_a_package_may_narrow_what_counts_as_stable(self):
        recipe(self.checkout, '2.9')
        recipe(self.checkout, '2.10')
        hooks = "\ndef stable(config):\n    return config['package']['name'] != 'lang-2.10'\n"
        self.assertEqual('lang-2.9.yaml', self.latest(hooks))

    def test_policy_may_pin_an_older_stream(self):
        recipe(self.checkout, '25')
        recipe(self.checkout, '27')
        self.assertEqual('lang-25.yaml', self.latest(policy={'revision': 1, 'stream': '25'}))

    def test_an_unpinned_newest_recipe_never_falls_back_to_an_older_one(self):
        recipe(self.checkout, '2.9')
        recipe(self.checkout, '2.10', commit='main')
        with self.assertRaisesRegex(ValueError, 'no exact source pin'):
            self.latest()

    def test_no_stable_recipe_is_an_error(self):
        recipe(self.checkout, '3.15', '3.15.0_rc1')
        with self.assertRaisesRegex(ValueError, 'No stable Wolfi recipe'):
            self.latest()


class Capture(unittest.TestCase):
    def test_capture_locks_the_newest_recipe_and_its_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            checkout = root / 'os'
            checkout.mkdir()
            recipe(checkout, '8.4')
            recipe(checkout, '8.5', version='8.5.11')
            (checkout / 'lang-8.5').mkdir()
            (checkout / 'lang-8.5/fix.patch').write_text('patch\n')
            (checkout / 'pipelines').mkdir()
            (checkout / 'pipelines/strip.yaml').write_text('name: strip\n')
            (checkout / 'wolfi-signing.rsa.pub').write_text('key\n')
            pkg = fixture.package(root, policy={'revision': 2})
            git = {('rev-parse', 'HEAD'): 'c' * 40 + '\n', ('status', '--porcelain'): '',
                   ('log', '-1', '--format=%ct'): '1792000000\n'}
            with patch.object(refresh, 'policy_hashes', lambda pkg, root: {'policy.json': '0' * 64}), \
                 patch.object(refresh.subprocess, 'check_output', lambda args, text: git[tuple(args[3:])]):
                lock = refresh.capture(pkg, checkout)
            self.assertEqual(('fixture', 'lang-8.5.yaml', '8.5.11', 3, 2, 'c' * 40, 1792000000, '1' * 40),
                             (lock['package'], lock['recipe'], lock['version'], lock['epoch'], lock['revision'],
                              lock['wolfi_commit'], lock['source_date_epoch'], lock['source_commit']))
            self.assertEqual({'upstream.yaml', 'upstream/fix.patch', 'pipelines/strip.yaml', 'wolfi-signing.rsa.pub'},
                             set(lock['sha256']))
            self.assertEqual(lock['build_id'], inputs.identity(lock))
            self.assertEqual(lock, json.loads((pkg.inputs / 'upstream.lock.json').read_text()))

    def test_an_unrelated_wolfi_commit_does_not_rebuild_but_source_and_policy_do(self):
        lock = {'recipe': 'lang-8.5.yaml', 'source_commit': '1' * 40, 'revision': 1,
                'sha256': {'upstream.yaml': '2' * 64}, 'mode': {'upstream.yaml': 0o644},
                'policy_sha256': {'packages/lang/patches/series': '3' * 64}, 'wolfi_commit': '4' * 40}
        first = inputs.identity(lock)
        self.assertEqual(first, inputs.identity(dict(lock, wolfi_commit='5' * 40, source_date_epoch=1792000600)))
        for changed in [{'source_commit': '6' * 40}, {'revision': 2},
                        {'policy_sha256': {'packages/lang/patches/series': '7' * 64}}]:
            self.assertNotEqual(first, inputs.identity(dict(lock, **changed)))


class Identity(unittest.TestCase):
    def test_a_packages_build_hashes_its_own_files_and_shared_build_inputs_only(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            pkg = fixture.package(root, patches={'0001-a.patch': 'x\n'})
            for name in inputs.SHARED + ['scripts/publish.py', 'scripts/drift.py', 'README.md']:
                (root / name).parent.mkdir(parents=True, exist_ok=True)
                (root / name).write_text(name)
            (pkg.dir / 'README.md').write_text('docs\n')
            hashed = set(inputs.policy_hashes(pkg, root))
            self.assertIn('scripts/derive.py', hashed)
            self.assertIn('packages/fixture/patches/0001-a.patch', hashed)
            for left_out in ['scripts/publish.py', 'scripts/drift.py', 'README.md', 'packages/fixture/README.md']:
                self.assertNotIn(left_out, hashed)


if __name__ == '__main__':
    unittest.main()
