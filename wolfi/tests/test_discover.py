import json
import os
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import discover
import fixture

LOCK = {'version': '1.2', 'epoch': 0, 'recipe': 'lang-1.yaml', 'wolfi_commit': 'a' * 40,
        'source_commit': 'b' * 40, 'build_id': 'c' * 64}


class Check(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.pkg = fixture.package(self.directory.name)
        self.pkg.inputs.mkdir(parents=True)
        (self.pkg.inputs / 'upstream.lock.json').write_text(json.dumps(LOCK))
        self.filed, self.resolved = [], []

    def tearDown(self):
        self.directory.cleanup()

    def check(self, outcomes, state='build', github=True):
        """discover.check with each step passing or failing as outcomes says, by kind."""
        ran = []
        def step(pkg, kind, command, wolfi):
            ran.append(kind)
            passed, said = outcomes.get(kind, (True, 'ok\n'))
            (pkg.build / (kind + '.log')).write_text(said)
            return passed, said
        with patch.object(discover, 'step', step), \
             patch.object(discover.drift, 'file', lambda pkg, kind, *a: self.filed.append(kind) or ('opened', 1)), \
             patch.object(discover.drift, 'resolve', lambda pkg, fuzz, said: self.resolved.append(fuzz) or []), \
             patch.object(discover.publish, 'status', return_value=state), \
             patch.dict(os.environ, {'GITHUB_REPOSITORY': 'example/repo'}):
            return discover.check(self.pkg, '/wolfi', github), ran

    def test_a_failing_step_files_its_kind_and_stops_that_package(self):
        state, ran = self.check({'derivation': (False, 'ValueError: Review changed\n')})
        self.assertEqual('failed', state)
        self.assertEqual(['refresh', 'derivation'], ran)
        self.assertEqual(['derivation'], self.filed)
        self.assertEqual([], self.resolved)

    def test_a_patch_that_will_not_apply_is_filed_as_patches(self):
        state, _ = self.check({'patches': (False, 'prepare: patches/0001-a.patch does not apply\n')})
        self.assertEqual(('failed', ['patches']), (state, self.filed))

    def test_a_passing_package_closes_its_issues_bundles_inputs_and_reports_its_release(self):
        state, ran = self.check({}, state='publish')
        self.assertEqual('publish', state)
        self.assertEqual(['refresh', 'derivation', 'derivation', 'patches'], ran)
        self.assertEqual([False, True], self.resolved)
        with tarfile.open(self.pkg.root / 'build/bundles/fixture.tar.gz') as bundle:
            self.assertIn('./upstream.lock.json', bundle.getnames())

    def test_a_fuzzy_patch_builds_and_files_an_advisory_issue(self):
        state, _ = self.check({'patches': (True, 'prepare: patches/0002-b.patch applied with fuzz: hunk 1\n')})
        self.assertEqual('build', state)
        self.assertEqual(['fuzz'], self.filed)
        self.assertEqual([False], self.resolved)

    def test_locally_nothing_is_filed_or_closed_and_a_passing_package_is_built(self):
        self.check({'patches': (False, 'prepare: x does not apply\n')}, github=False)
        self.assertEqual('build', self.check({}, state='complete', github=False)[0])
        self.assertEqual(([], []), (self.filed, self.resolved))


if __name__ == '__main__':
    unittest.main()
