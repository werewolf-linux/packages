import copy
from pathlib import Path
import sys
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT / 'scripts'))
import packages
import verify

PKG = packages.Package('ffmpeg')
SOURCE = yaml.safe_load((Path(__file__).parent / 'fixtures/ffmpeg-9.0.yaml').read_text())
LOCK = dict(recipe='ffmpeg-9.0.yaml', version='9.0.2', epoch=5, revision=1,
            source_commit=PKG.hooks.source_commit(SOURCE))


class FFmpeg(unittest.TestCase):
    def test_codecs_and_conversion_tests_remain(self):
        result = PKG.hooks.derive(PKG, copy.deepcopy(SOURCE), LOCK)
        verify.check(PKG, SOURCE, result)
        options = result['pipeline'][1]['runs']
        for flag in ['--enable-libx264', '--enable-libx265', '--enable-libdav1d',
                     '--enable-libvpx', '--enable-libmp3lame', '--enable-libopus',
                     '--disable-network', '--enable-protocol=file,pipe']:
            self.assertIn(flag, options)
        self.assertEqual(SOURCE['test']['pipeline'][-1], result['test']['pipeline'][-2])
        self.assertEqual(1005, result['package']['epoch'])

    def test_upstream_fate_tests_are_retained_and_required(self):
        result = PKG.hooks.derive(PKG, copy.deepcopy(SOURCE), LOCK)
        self.assertEqual({'uses': 'upstream-tests/ffmpeg-fate'}, result['pipeline'][3])
        source = copy.deepcopy(SOURCE)
        source['pipeline'].pop(3)
        with self.assertRaises(ValueError):
            PKG.hooks.derive(PKG, source, LOCK)
        result['pipeline'].pop(3)
        with self.assertRaises(AssertionError):
            verify.check(PKG, SOURCE, result)

    def test_missing_upstream_flag_requires_review(self):
        source = copy.deepcopy(SOURCE)
        source['pipeline'][1]['runs'] = source['pipeline'][1]['runs'].replace('--enable-libssh', '')
        with self.assertRaises(ValueError):
            PKG.hooks.derive(PKG, source, LOCK)


if __name__ == '__main__':
    unittest.main()
