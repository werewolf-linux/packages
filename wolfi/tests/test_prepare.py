import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import yaml

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import prepare
import fixture

LINES = ['a1', 'a2', 'a3', 'base', 'b1', 'b2', 'b3']


def diff(before, after, first='a1'):
    return ('--- a/policy.txt\n+++ b/policy.txt\n@@ -1,7 +1,7 @@\n {}\n a2\n a3\n-{}\n+{}\n b1\n b2\n b3\n'
            .format(first, before, after))


class SourcePreparation(unittest.TestCase):
    def prepared(self, ours, upstream_lines=LINES, hooks=''):
        """prepare() over a local upstream holding upstream_lines; return (output, source)."""
        root = Path(tempfile.mkdtemp())
        self.addCleanup(subprocess.run, ['rm', '-rf', str(root)])
        upstream = root / 'upstream'
        upstream.mkdir()
        def git(*args):
            return subprocess.run(['git', '-C', str(upstream), *args], check=True,
                                  capture_output=True, text=True).stdout.strip()
        git('init')
        (upstream / 'policy.txt').write_text('\n'.join(upstream_lines) + '\n')
        git('add', 'policy.txt')
        git('-c', 'user.name=fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-m', 'fixture')
        pkg = fixture.package(root, hooks, patches={'0001-policy.patch': ours})
        (pkg.inputs / 'upstream').mkdir(parents=True)
        (pkg.inputs / 'upstream/wolfi.patch').write_text(diff('base', 'wolfi'))
        (pkg.inputs / 'upstream.yaml').write_text(yaml.safe_dump(
            {'pipeline': [{'uses': 'git-checkout'}, {'uses': 'patch', 'with': {'patches': 'wolfi.patch'}}]}))
        (pkg.inputs / 'upstream.lock.json').write_text(json.dumps(
            {'version': '1.0', 'epoch': 0, 'wolfi_commit': 'f' * 40, 'source_commit': git('rev-parse', 'HEAD')}))
        said = io.StringIO()
        with contextlib.redirect_stdout(said):
            prepare.prepare(pkg, root / 'source', (str(upstream),))
        return said.getvalue(), root / 'source'

    def test_wolfis_patches_then_ours_apply_and_report_nothing(self):
        said, source = self.prepared(diff('wolfi', 'downstream'))
        self.assertEqual('downstream', (source / 'policy.txt').read_text().splitlines()[3])
        self.assertIn("prepare: applied Wolfi's wolfi.patch", said)
        self.assertIn('prepare: applied patches/0001-policy.patch', said)
        self.assertNotIn('fuzz', said)

    def test_our_patch_needing_fuzz_applies_and_is_named_for_the_drift_issue(self):
        moved = ['a1, renamed'] + LINES[1:]
        said, source = self.prepared(diff('wolfi', 'downstream'), moved)
        self.assertEqual('downstream', (source / 'policy.txt').read_text().splitlines()[3])
        self.assertRegex(said, r'prepare: patches/0001-policy.patch applied with fuzz: hunk 1 at line 1 \(fuzz \d\)')

    def test_wolfis_patch_needing_fuzz_is_not_ours_to_refresh(self):
        moved = ['a1, renamed'] + LINES[1:]
        said, _ = self.prepared(diff('wolfi', 'downstream', first='a1, renamed'), moved)
        self.assertIn("prepare: applied Wolfi's wolfi.patch, as Wolfi does, with fuzz", said)
        self.assertNotRegex(said, r"prepare: Wolfi's wolfi.patch applied with fuzz")

    def test_a_patch_that_will_not_apply_names_itself_and_says_why(self):
        with self.assertRaises(SystemExit) as stopped:
            self.prepared(diff('nothing like it', 'downstream'))
        message = str(stopped.exception.code)
        self.assertTrue(message.startswith('prepare: patches/0001-policy.patch does not apply\n'), message)
        self.assertIn('policy.txt', message)

    def test_the_packages_own_last_step_runs(self):
        hooks = '\ndef prepared(pkg, destination):\n    (destination / "prepared").write_text("yes")\n'
        _, source = self.prepared(diff('wolfi', 'downstream'), hooks=hooks)
        self.assertEqual('yes', (source / 'prepared').read_text())

    def test_wolfis_patches_in_recipe_order(self):
        config = {'pipeline': [{'uses': 'git-checkout'}, {'uses': 'patch', 'with': {'patches': 'a.patch b.patch'}},
                               {'name': 'Configure'}, {'uses': 'patch', 'with': {'patches': 'c.patch'}}]}
        self.assertEqual(['a.patch', 'b.patch', 'c.patch'], prepare.upstream_patches(config))

    def test_an_existing_destination_is_never_reused(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(ValueError, 'new destination'):
                prepare.prepare(fixture.package(directory), Path(directory))


if __name__ == '__main__':
    unittest.main()
