from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import notes

SECDB = {'packages': [
    {'pkg': {'name': 'php-8.5', 'secfixes': {'0': ['CVE-2000-0001'], '8.5.10-r0': ['CVE-2026-1'],
                                              '8.5.11-r2': ['CVE-2026-3', 'CVE-2026-2'], '8.5.12-r0': ['CVE-2026-4']}}},
    {'pkg': {'name': 'python-3.15', 'secfixes': {'3.15.0_git20261001-r0': ['CVE-2026-8'],
                                                  '3.15.0_git20261010-r0': ['CVE-2026-9']}}}]}


def facts(previous=None, commits=(), stream='php-8.5', wolfi='8.5.11-r3'):
    return {'title': 'PHP', 'package': 'php', 'version': '8.5.11-r1003', 'build_id': 'f' * 64, 'date': '2026-10-15',
            'commit': 'c' * 40, 'run': 'https://github.com/werewolf-linux/hardened-packages/actions/runs/1',
            'source': {'source_commit': 'b' * 40, 'wolfi_commit': 'a' * 40, 'wolfi_version': wolfi,
                       'recipe': stream + '.yaml', 'policy_revision': 1, 'upstream': 'https://github.com/php/php-src'},
            'repository': {'stable': 'https://dist.werewolf-linux.org/apk/php/',
                           'snapshot': 'https://dist.werewolf-linux.org/apk/php/builds/' + 'f' * 64, 'key_sha256': '0' * 64},
            'patches': ['0001-a.patch', '0002-b.patch'], 'wolfi_inputs': {}, 'previous': previous, 'commits': list(commits),
            'security': notes.security(SECDB, stream, previous['wolfi_version'] if previous else None, wolfi),
            'architectures': {'x86_64': {'packages': [{'name': 'php-8.5', 'version': '8.5.11-r1003', 'size': 1234}]},
                              'aarch64': {'packages': [{'name': 'php-8.5', 'version': '8.5.11-r1003', 'size': 999}]}}}


class Notes(unittest.TestCase):
    def test_versions_order_as_apk_does_snapshots_included(self):
        key = notes.version_key
        self.assertLess(key('3.15.0-r0'), key('3.15.0_git20261001-r0'))
        self.assertLess(key('3.15.0_git20261001-r0'), key('3.15.0_git20261010-r0'))
        self.assertLess(key('3.15.0_git20261010-r9'), key('3.15.1-r0'))
        self.assertLess(key('8.5.11-r3'), key('8.5.11-r1003'))
        self.assertIsNone(key('0'))
        self.assertIsNone(key('3.15.0_rc1-r0'))

    def test_security_counts_wolfis_fixes_in_the_window_through_this_build(self):
        found = notes.security(SECDB, 'php-8.5', '8.5.10-r0', '8.5.11-r3')
        self.assertEqual([{'version': '8.5.11-r2', 'ids': ['CVE-2026-2', 'CVE-2026-3']}], found['fixed'])
        self.assertEqual((3, 1), (found['cumulative'], found['not_affected']))
        snapshot = notes.security(SECDB, 'python-3.15', '3.15.0_git20261001-r0', '3.15.0_git20261010-r0')
        self.assertEqual([{'version': '3.15.0_git20261010-r0', 'ids': ['CVE-2026-9']}], snapshot['fixed'])

    def test_the_first_release(self):
        text = notes.markdown(notes.build(facts()))
        self.assertTrue(text.startswith('# PHP 8.5.11-r1003\n'))
        for fact in ['The first release.', '8.5.11-r3', 'php-8.5.yaml', '`0002-b.patch`', '| php-8.5 | 1,234 | 999 |',
                     'https://github.com/php/php-src/commit/' + 'b' * 40, 'packages/php/patches']:
            self.assertIn(fact, text)

    def test_what_changed_since_the_previous_release(self):
        previous = {'version': '8.5.10-r1003', 'build_id': 'e' * 64, 'commit': 'd' * 40,
                    'wolfi_version': '8.5.10-r0', 'source_commit': '9' * 40}
        text = notes.markdown(notes.build(facts(previous, [{'sha': '1' * 40, 'subject': 'Tighten the floor'}])))
        for fact in ['Since 8.5.10-r1003:', "Wolfi's build: 8.5.10-r0 → 8.5.11-r3",
                     '/compare/' + '9' * 40 + '...' + 'b' * 40, 'Tighten the floor', 'CVE-2026-2, CVE-2026-3']:
            self.assertIn(fact, text)

    def test_an_unanswered_github_is_unavailable_never_unchanged(self):
        previous = {'version': '8.5.10-r1003', 'build_id': 'e' * 64, 'commit': 'd' * 40,
                    'wolfi_version': '8.5.10-r0', 'source_commit': 'b' * 40}
        made = notes.build(facts(previous))
        made['commits'] = None
        text = notes.markdown(made)
        self.assertIn('unavailable', text)
        self.assertNotIn('unchanged', text)


if __name__ == '__main__':
    unittest.main()
