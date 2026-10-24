import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import drift
import fixture

LOCK = {'version': '8.5.12', 'epoch': 0, 'recipe': 'lang-8.5.yaml', 'wolfi_commit': 'a' * 40, 'source_commit': 'b' * 40}
PREPARE = ('prepare: fixture 8.5.12-r0 from Wolfi aaaa, source bbbb\n'
           "prepare: applied Wolfi's install-pear.patch\n"
           'prepare: applied patches/0001-deny-environment-configuration.patch\n'
           'prepare: patches/0002-werewolf-allowances.patch does not apply\n'
           'Hunk #1 FAILED at 769.\n'
           '1 out of 1 hunk FAILED -- saving rejects to file main/main.c.rej\n')
FUZZED = ('prepare: applied patches/0001-a.patch\n'
          'prepare: patches/0002-b.patch applied with fuzz: hunk 1 at line 2393 (fuzz 2)\n')
RUN = 'https://github.com/werewolf-linux/hardened-packages/actions/runs/7'


class Report(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.pkg = fixture.package(self.directory.name)

    def tearDown(self):
        self.directory.cleanup()

    def test_a_patch_failure_names_the_package_version_patch_and_output(self):
        title, body, _ = drift.report(self.pkg, 'patches', PREPARE, LOCK, RUN)
        self.assertEqual('Fixture 8.5.12-r0: patches/0002-werewolf-allowances.patch no longer applies', title)
        for fact in ['8.5.12-r0', 'patches/0002-werewolf-allowances.patch does not apply', 'a' * 40,
                     'b' * 40, 'lang-8.5.yaml', RUN, 'Hunk #1 FAILED at 769.', 'publishes no Fixture build']:
            self.assertIn(fact, body)

    def test_a_capture_failure_has_no_lock(self):
        title, body, _ = drift.report(self.pkg, 'refresh', 'ValueError: No stable Wolfi recipe\n', None, None)
        self.assertEqual('Fixture (latest Wolfi): capturing the recipe stopped', title)
        self.assertIn('`lang-*.yaml`, not captured', body)

    def test_a_derivation_failure(self):
        title, _, _ = drift.report(self.pkg, 'derivation', 'ValueError: Review changed\n', LOCK, None)
        self.assertEqual('Fixture 8.5.12-r0: the derivation stopped', title)

    def test_fuzz_names_the_patch_and_does_not_block(self):
        title, body, marker = drift.report(self.pkg, 'fuzz', FUZZED, LOCK, None)
        self.assertEqual('Fixture 8.5.12-r0: patches/0002-b.patch applies only with fuzz', title)
        self.assertIn('builds, but patches/0002-b.patch applied only with fuzz', body)
        self.assertNotIn('publishes no', body)
        self.assertTrue(marker.startswith('fuzz:'))
        self.assertEqual([drift.LABEL, 'package/fixture', drift.FUZZ], drift.labels(self.pkg, 'fuzz'))
        self.assertEqual([drift.LABEL, 'package/fixture'], drift.labels(self.pkg, 'patches'))

    def test_the_marker_ignores_the_run_but_not_the_failure(self):
        one = drift.report(self.pkg, 'patches', PREPARE, LOCK, RUN)[2]
        self.assertEqual(one, drift.report(self.pkg, 'patches', PREPARE, LOCK, RUN + '9')[2])
        self.assertNotEqual(one, drift.report(self.pkg, 'patches', PREPARE + 'more\n', LOCK, RUN)[2])

    def test_output_keeps_the_end_strips_colour_and_cannot_close_its_fence(self):
        text = '\n'.join(str(n) for n in range(1000)) + '\n\x1b[31mred\x1b[0m ``` end'
        kept = drift.output(text)
        self.assertEqual(drift.TAIL, len(kept.splitlines()))
        self.assertTrue(kept.endswith("red ''' end"))
        self.assertLessEqual(len(drift.output('x' * 10 ** 6)), drift.LIMIT)


class Filing(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.pkg = fixture.package(self.directory.name)

    def tearDown(self):
        self.directory.cleanup()

    def runner(self, issues=(), seen=None):
        calls = []
        def run(*args):
            calls.append(args)
            if args[:2] == ('issue', 'list'):
                return json.dumps(list(issues))
            if args[:2] == ('issue', 'view'):
                return json.dumps(seen or {'body': '', 'comments': []})
            if args[:2] == ('issue', 'create'):
                return 'https://github.com/werewolf-linux/hardened-packages/issues/9\n'
            return ''
        return run, calls

    def test_opens_an_issue_labelled_for_the_package_and_kind(self):
        run, calls = self.runner()
        self.assertEqual(('opened', 'https://github.com/werewolf-linux/hardened-packages/issues/9'),
                         drift.file(self.pkg, 'fuzz', 'Fixture 1: x', 'body', 'm', run))
        listing = next(c for c in calls if c[:2] == ('issue', 'list'))
        self.assertIn('package/fixture', listing)
        create = next(c for c in calls if c[:2] == ('issue', 'create'))
        for label in drift.labels(self.pkg, 'fuzz'):
            self.assertIn(label, create)

    def test_the_same_failure_again_adds_nothing(self):
        run, calls = self.runner([{'number': 4, 'title': 'Fixture 1: x', 'labels': []}],
                                 {'body': 'b\n<!-- drift m -->\n', 'comments': []})
        self.assertEqual(('unchanged', 4), drift.file(self.pkg, 'patches', 'Fixture 1: x', 'body', 'm', run))
        self.assertFalse(any(c[:2] in (('issue', 'create'), ('issue', 'comment')) for c in calls))

    def test_a_different_failure_under_the_same_title_is_a_comment(self):
        run, calls = self.runner([{'number': 4, 'title': 'Fixture 1: x', 'labels': []}],
                                 {'body': '<!-- drift old -->', 'comments': [{'body': '<!-- drift older -->'}]})
        self.assertEqual(('commented', 4), drift.file(self.pkg, 'patches', 'Fixture 1: x', 'body', 'new', run))
        self.assertTrue(any(c[:3] == ('issue', 'comment', '4') for c in calls))

    def test_a_passing_run_closes_blocking_issues_and_an_exact_one_closes_fuzz_issues(self):
        issues = [{'number': 1, 'title': 'Fixture 1: patches/0002 no longer applies', 'labels': [{'name': 'drift'}]},
                  {'number': 2, 'title': 'Fixture 1: patches/0003 applies only with fuzz',
                   'labels': [{'name': 'drift'}, {'name': 'fuzz'}]}]
        run, calls = self.runner(issues)
        self.assertEqual([1], drift.resolve(self.pkg, False, 'fixed', run))
        self.assertEqual([2], drift.resolve(self.pkg, True, 'exact again', run))
        self.assertEqual([('issue', 'close', '1', '--comment', 'fixed'), ('issue', 'close', '2', '--comment', 'exact again')],
                         [c for c in calls if c[:2] == ('issue', 'close')])


if __name__ == '__main__':
    unittest.main()
