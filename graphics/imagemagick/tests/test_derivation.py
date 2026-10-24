"""Changed upstream build inputs must not silently weaken the reduced profile."""
from copy import deepcopy
from pathlib import Path
import sys
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT / 'wolfi/scripts'))
import packages
import verify


class UpstreamDrift(unittest.TestCase):
    def setUp(self):
        self.pkg = packages.Package('imagemagick')
        self.config = yaml.safe_load((Path(__file__).parent / 'fixtures/imagemagick-7.yaml').read_text())
        self.lock = {'recipe': 'imagemagick-7.yaml',
                     'source_commit': self.pkg.hooks.source_commit(self.config),
                     'version': str(self.config['package']['version']),
                     'epoch': self.config['package']['epoch'], 'revision': 1}

    def test_server_profile_keeps_document_and_image_consumers(self):
        result = self.pkg.hooks.derive(self.pkg, deepcopy(self.config), self.lock)
        verify.check(self.pkg, self.config, result)
        dependencies = result['environment']['contents']['packages']
        self.assertTrue(self.pkg.hooks.REMOVED.isdisjoint(dependencies))
        for dependency in ('ghostscript-dev', 'libheif-dev', 'libjpeg-turbo-dev', 'libpng-dev', 'libwebp-dev'):
            self.assertIn(dependency, dependencies)
        self.assertEqual(self.config['test']['pipeline'], result['test']['pipeline'][:-1])
        probe = result['test']['pipeline'][-1]
        self.assertEqual(['ghostscript', 'ttf-dejavu'], probe['needs']['packages'])
        self.assertEqual((self.pkg.dir / 'tests/probe.sh').read_text(), probe['runs'])
        self.assertEqual(1000, result['package']['epoch'])
        self.assertEqual('7.1.2.34-r1000', self.pkg.hooks.full_version(self.lock))
        self.assertEqual({'imagemagick-7', 'imagemagick-7-doc', 'imagemagick-7-static', 'imagemagick-7-dev'},
                         packages.output_names(result))

    def test_relaxing_the_server_profile_is_rejected(self):
        result = self.pkg.hooks.derive(self.pkg, deepcopy(self.config), self.lock)
        changes = [lambda c: c['pipeline'][1]['with'].update(opts='--with-x'),
                   lambda c: c['environment']['contents']['packages'].append('libx11-dev'),
                   lambda c: c['test']['pipeline'].pop()]
        for change in changes:
            with self.subTest(change=change):
                relaxed = deepcopy(result)
                change(relaxed)
                with self.assertRaises(AssertionError):
                    verify.check(self.pkg, self.config, relaxed)

    def test_new_upstream_configuration_requires_review(self):
        self.config['pipeline'][1]['with'] = {'opts': '--with-x'}
        with self.assertRaisesRegex(ValueError, 'configure/source'):
            self.pkg.hooks.derive(self.pkg, self.config, self.lock)

    def test_unrecognized_build_layout_requires_review(self):
        self.config['pipeline'].insert(2, {'runs': 'make additional-plugin'})
        with self.assertRaisesRegex(ValueError, 'pipeline'):
            self.pkg.hooks.derive(self.pkg, self.config, self.lock)

    def test_source_identity_cannot_change(self):
        self.lock['source_commit'] = '0' * 40
        with self.assertRaisesRegex(ValueError, 'configure/source'):
            self.pkg.hooks.derive(self.pkg, self.config, self.lock)
