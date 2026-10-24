import copy
from pathlib import Path
import sys
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT / 'scripts'))
import packages
import verify

PKG = packages.Package('vips')
SOURCE = yaml.safe_load((Path(__file__).parent / 'fixtures/libvips.yaml').read_text())
LOCK = dict(recipe='libvips.yaml', version='8.18.7', epoch=1, revision=PKG.policy['revision'],
            source_commit=PKG.hooks.source_commit(SOURCE))


class Vips(unittest.TestCase):
    def test_retains_non_heif_upstream_tests_and_explicitly_selects_formats(self):
        result = PKG.hooks.derive(PKG, copy.deepcopy(SOURCE), LOCK)
        verify.check(PKG, SOURCE, result)
        options = result['pipeline'][1]['with']['opts']
        for option in ['-Dauto_features=disabled', '-Djpeg=enabled', '-Dwebp=enabled',
                       '-Dpng=enabled', '-Dnsgif=true', '-Dexif=enabled', '-Dlcms=enabled']:
            self.assertIn(option, options)
        self.assertNotIn('libheif-dev', result['environment']['contents']['packages'])
        self.assertNotIn('librsvg-dev', result['environment']['contents']['packages'])
        self.assertEqual(LOCK['revision'] * 1000 + LOCK['epoch'], result['package']['epoch'])

    def test_new_upstream_dependency_requires_review(self):
        source = copy.deepcopy(SOURCE)
        source['environment']['contents']['packages'].append('new-loader-dev')
        with self.assertRaises(ValueError):
            PKG.hooks.derive(PKG, source, LOCK)


if __name__ == '__main__':
    unittest.main()
