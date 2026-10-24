from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import patching
from fixture import change


class Patching(unittest.TestCase):
    def source(self, directory, lines):
        root = Path(directory) / 'source'
        root.mkdir()
        (root / 'file.c').write_text('\n'.join(lines) + '\n')
        return root

    def test_an_exact_patch_applies_and_reports_no_fuzz(self):
        with tempfile.TemporaryDirectory() as directory:
            root = self.source(directory, ['one', 'two', 'old', 'three'])
            patch = Path(directory) / 'exact.patch'
            patch.write_text(change('file.c', 'old', 'new'))
            self.assertEqual([], patching.apply(root, patch))
            self.assertEqual('one\ntwo\nnew\nthree\n', (root / 'file.c').read_text())

    def test_a_patch_whose_context_moved_applies_with_fuzz_and_says_so(self):
        with tempfile.TemporaryDirectory() as directory:
            # Upstream reworded the outermost context line since the patch was made.
            root = self.source(directory, ['a1, renamed', 'a2', 'a3', 'old', 'b1', 'b2', 'b3'])
            patch = Path(directory) / 'fuzzy.patch'
            patch.write_text('--- a/file.c\n+++ b/file.c\n@@ -1,7 +1,7 @@\n a1\n a2\n a3\n-old\n+new\n b1\n b2\n b3\n')
            fuzzed = patching.apply(root, patch)
            self.assertEqual(1, len(fuzzed))
            self.assertGreater(fuzzed[0][2], 0)
            self.assertEqual('a1, renamed\na2\na3\nnew\nb1\nb2\nb3\n', (root / 'file.c').read_text())

    def test_a_patch_that_will_not_apply_changes_nothing(self):
        with tempfile.TemporaryDirectory() as directory:
            root = self.source(directory, ['alpha', 'beta', 'gamma', 'delta'])
            patch = Path(directory) / 'stale.patch'
            patch.write_text(change('file.c', 'old', 'new'))
            with self.assertRaises(patching.PatchError) as failed:
                patching.apply(root, patch)
            self.assertIn('file.c', str(failed.exception))
            self.assertEqual('alpha\nbeta\ngamma\ndelta\n', (root / 'file.c').read_text())
            self.assertEqual(['file.c'], [p.name for p in root.iterdir()])

    def test_the_build_applies_every_patch_the_same_way_and_stops_on_one_that_fails(self):
        step = patching.step('werewolf/patches')
        self.assertEqual(['patch'], step['needs']['packages'])
        self.assertIn('patch ' + ' '.join(patching.OPTIONS) + ' -i "werewolf/patches/$name" || exit 1', step['runs'])
        self.assertTrue(step['runs'].endswith('done < werewolf/patches/series\n'))
        self.assertIn('--fuzz=2', patching.OPTIONS)


if __name__ == '__main__':
    unittest.main()
