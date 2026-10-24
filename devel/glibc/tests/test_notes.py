import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT/'scripts'))
import packages

# glibc's own notes, not the shared ones in scripts/notes.py.
notes = packages.Package('glibc').hooks.notes

X86_NOTES = ('GNU 0x00000040\tNT_GNU_PROPERTY_TYPE_0\t Properties: x86 feature: IBT, SHSTK, x86 ISA needed: '
             'x86-64-baseline\nGNU 0x00000010\tNT_GNU_ABI_TAG (ABI version tag)\t OS: Linux, ABI: 6.18.0')
# GNU readelf's spelling, as the probes record it.
ARM_NOTES = 'GNU 0x00000010\tNT_GNU_PROPERTY_TYPE_0\t Properties: AArch64 feature: BTI, PAC, GCS'

def report(arch_notes, first_load):
    headers = (f'  LOAD 0x000000 0x0000000000000000 0x0000000000000000 0x023808 0x023808 {first_load} 0x1000\n'
               '  GNU_RELRO 0x1efb98 0x00000000001f0b98 0x00000000001f0b98 0x003468 0x003468 R 0x1\n')
    elf = {'notes': arch_notes, 'program_headers': headers, 'dynamic_tags': ' 0x18 (BIND_NOW) 0x0\n'}
    return {'compiler': 'gcc (Wolfi 16.2.0-r1) 16.2.0\nCopyright', 'resolved_compiler_options': '',
            'elf': {'elf/ld.so': elf, 'libc.so': elf}}

def facts(previous=True):
    """A plausible release: Wolfi moved from 2.44-r8 to 2.44-r9, one test regressed on x86_64."""
    before = {'misc/tst-a': 'PASS', 'misc/tst-b': 'FAIL', 'math/test-avx512': 'PASS'}
    architectures = {
        'x86_64': {'tests': {'misc/tst-a': 'FAIL', 'misc/tst-b': 'PASS', 'math/test-avx512': 'UNSUPPORTED'},
                   'previous_tests': dict(before) if previous else None, 'report': report(X86_NOTES, 'R'),
                   'packages': [{'name': 'glibc-neutered-2.44', 'version': '2.44-r21', 'size': 4194304},
                                {'name': 'glibc-neutered-2.44-locale-extra', 'version': '2.44-r21', 'size': 272255385}]},
        'aarch64': {'tests': dict(before), 'previous_tests': dict(before) if previous else None,
                    'report': report(ARM_NOTES, 'R E'),
                    'packages': [{'name': 'glibc-neutered-2.44', 'version': '2.44-r21', 'size': 3145728}]}}
    secdb = {'packages': [{'pkg': {'name': 'glibc-2.44', 'secfixes': {
        '0': ['CVE-2010-4756'], '2.44-r4': ['CVE-2026-1'], '2.44-r8': ['CVE-2026-2'],
        '2.44-r9': ['CVE-2026-3', 'GHSA-aaaa-bbbb-cccc'], '2.44-r10': ['CVE-2026-4']}}}]}
    return {'version': '2.44-r21', 'date': '2026-10-16', 'build_id': 'b' * 64, 'commit': 'c' * 40,
            'run': 'https://github.com/werewolf-linux/glibc-build/actions/runs/1',
            'source': {'glibc_commit': 'd' * 40, 'wolfi_commit': 'e' * 40, 'wolfi_version': '2.44-r9',
                       'recipe': 'glibc-2.44.yaml', 'policy_revision': 12},
            'repository': {'stable': 'https://dist.example/apk/', 'snapshot': 'https://dist.example/apk/builds/b',
                           'key_sha256': 'f' * 64},
            'patches': ['0001-deny-internal-environment.patch'], 'wolfi_inputs': {'glibc-2.44.yaml': '1', 'new.patch': '2'},
            'previous': {'version': '2.44-r20', 'build_id': 'a' * 64, 'commit': '9' * 40, 'wolfi_version': '2.44-r8',
                         'wolfi_inputs': {'glibc-2.44.yaml': '0'}} if previous else None,
            'commits': [{'sha': '8' * 40, 'subject': 'release: escape <tags> & such'}] if previous else None,
            'glibc_commits': [{'sha': '7' * 40, 'subject': 'elf: Fix a loader bug (CVE-2026-3)'}] if previous else None,
            'ceiling': 55, 'architectures': architectures,
            'security': notes.security(secdb, 'glibc-2.44', '2.44-r8' if previous else None, '2.44-r9')}

class Notes(unittest.TestCase):
    def test_versions_order_as_apk_does(self):
        self.assertGreater(notes.version_key('2.44-r10'), notes.version_key('2.44-r9'))
        self.assertGreater(notes.version_key('2.45-r1'), notes.version_key('2.44-r30'))
        self.assertIsNone(notes.version_key('0'))

    def test_security_lists_the_window_and_counts_everything_through_current(self):
        s = facts()['security']
        self.assertEqual([{'version': '2.44-r9', 'ids': ['CVE-2026-3', 'GHSA-aaaa-bbbb-cccc']}], s['fixed'])
        self.assertEqual(4, s['cumulative'])
        self.assertEqual(['2.44-r4', '2.44-r8', '2.44-r9'], [x['version'] for x in s['through']])
        self.assertEqual(1, s['not_affected'])
        self.assertEqual([], facts(previous=False)['security']['fixed'])

    def test_tests_compare_against_the_previous_release(self):
        x86 = notes.build(facts())['architectures']['x86_64']['tests']
        self.assertEqual(['misc/tst-a'], x86['new_failures'])
        self.assertEqual(['misc/tst-b'], x86['fixed'])
        self.assertEqual({'PASS → UNSUPPORTED': 1}, x86['other_changes'])
        self.assertEqual(500.0, x86['fail_per_mille'])
        self.assertFalse(notes.build(facts(previous=False))['architectures']['x86_64']['tests']['compared'])

    def test_hardening_reads_readelf_output(self):
        made = notes.build(facts())['architectures']
        libc = made['x86_64']['hardening']['objects']['libc.so']
        self.assertEqual(('IBT, SHSTK', False, True, True, '6.18.0'),
                         (libc['control_flow'], libc['headers_executable'], libc['relro'], libc['bind_now'], libc['minimum_kernel']))
        arm = made['aarch64']['hardening']['objects']['ld.so']
        self.assertEqual(('BTI, PAC, GCS', True), (arm['control_flow'], arm['headers_executable']))
        self.assertEqual('gcc (Wolfi 16.2.0-r1) 16.2.0', made['aarch64']['hardening']['compiler'])

    def test_renderings_say_what_changed_and_escape_html(self):
        made = notes.build(facts())
        self.assertNotIn('wolfi_inputs', made['previous'])
        self.assertEqual([{'path': 'glibc-2.44.yaml', 'kind': 'changed'}, {'path': 'new.patch', 'kind': 'added'}],
                         made['wolfi_changes'])
        text = notes.markdown(made)
        for expected in ['## Changes since 2.44-r20', 'Wolfi glibc: 2.44-r8 → 2.44-r9.',
                         'Security: Wolfi fixed CVE-2026-3, GHSA-aaaa-bbbb-cccc.', 'x86_64: 1 new failure',
                         '| x86_64 | 1 | 1 | 0 | 0 | 1 |', '`misc/tst-a`', '| `glibc-neutered-2.44` | 2.44-r21 | 3.0 MiB | 4.0 MiB |',
                         'Wolfi recipe: 2 files changed.', 'glibc upstream: 1 commit on its release branch.',
                         'https://github.com/wolfi-dev/os/commits/' + 'e' * 40 + '/glibc-2.44.yaml', '## glibc upstream commits',
                         'sourceware.org/git/?p=glibc.git;a=commit;h=' + '7' * 40, '<summary>Every advisory fixed through 2.44-r9',
                         '| `glibc-neutered-2.44-locale-extra` | 2.44-r21 | — | 259.6 MiB |']:
            self.assertIn(expected, text)
        page = notes.page(made)
        self.assertIn('escape &lt;tags&gt; &amp; such', page)
        self.assertNotIn('<tags>', page)
        self.assertEqual(notes.encode(made), notes.encode(json.loads(notes.encode(made))))

    def test_a_first_release_says_so(self):
        made = notes.build(facts(previous=False))
        self.assertIsNone(made['wolfi_changes'])
        self.assertIn('First release with notes', notes.markdown(made))

    def test_releases_index_is_newest_first_and_idempotent(self):
        made = notes.build(facts())
        older = dict(notes.entry(made), version='2.44-r9', build_id='0' * 64)
        entries = notes.releases(notes.releases([older], made), made)
        self.assertEqual(['2.44-r21', '2.44-r9'], [e['version'] for e in entries])
        self.assertEqual(2, entries[0]['advisories_fixed'])
        self.assertEqual(1, entries[0]['new_test_failures'])
        self.assertIn('href="builds/' + 'b' * 64 + '/release-notes.html"', notes.index(entries))

if __name__ == '__main__':
    unittest.main()
