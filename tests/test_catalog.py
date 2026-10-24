"""Maintenance assertions fail before untracked or mislabeled packages can ship."""
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import catalog
import common


class Catalog(unittest.TestCase):
    def fixture(self, root, category='www', name='example'):
        directory = root / category / name
        directory.mkdir(parents=True)
        (directory / 'recipe.yaml').write_text('package: {name: example, version: 1, epoch: 0}\n')
        meta = {
            'update': {'source': 'github', 'repo': 'example/server'},
            'hardening': {'minimized': False, 'pledge': False,
                          'minimized_reason': 'No minimization claim.', 'pledge_reason': 'No package policy.',
                          'evidence': ['recipe.yaml']},
            'wolfi': {'relationship': 'independent', 'recipes': [],
                      'changes': [{'what': 'Package the server.', 'why': 'Provide the application.', 'evidence': ['recipe.yaml']}],
                      'removal_condition': 'Equivalent Wolfi packaging becomes available.'}}
        (directory / 'metadata.json').write_text(json.dumps(meta))
        return directory, meta

    def test_repository_covers_both_engines_and_keeps_npm_manifests_separate(self):
        packages = catalog.load()
        self.assertEqual({'upstream', 'wolfi'}, {m['engine'] for m in packages.values()})
        for name in ('n8n', 'n8n-vuln-demo'):
            directory = catalog.ROOT / packages[name]['path']
            self.assertIn('dependencies', json.loads((directory / 'package.json').read_text()))
        self.assertEqual('net/curl', packages['curl']['path'])

    def test_missing_source_bad_flags_and_unexplained_divergence_fail(self):
        changes = [lambda m: m.pop('update'), lambda m: m['update'].clear(),
                   lambda m: m['update'].update(source='pinned'),
                   lambda m: m['update'].update(source='github', hold='Do not update'),
                   lambda m: m['hardening'].update(minimized='false'),
                   lambda m: m['hardening'].pop('pledge'),
                   lambda m: m['hardening'].update(pledge_reason=''),
                   lambda m: m['wolfi'].update(changes=[]),
                   lambda m: m['wolfi']['changes'][0].update(why=''),
                   lambda m: m['wolfi']['changes'][0].update(evidence=['../secret']),
                   lambda m: m['wolfi'].update(relationship='replacement')]
        for change in changes:
            with self.subTest(change=change), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                directory, meta = self.fixture(root)
                change(meta)
                (directory / 'metadata.json').write_text(json.dumps(meta))
                with self.assertRaises(ValueError):
                    catalog.load(root)

    def test_a_recipe_without_metadata_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            directory, _ = self.fixture(root)
            (directory / 'metadata.json').unlink()
            with self.assertRaisesRegex(ValueError, 'metadata.json'):
                catalog.load(root)

    def test_category_moves_preserve_the_context_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            directory, _ = self.fixture(root)
            self.assertEqual(['example'], list(catalog.load(root)))
            (root / 'net').mkdir()
            directory.rename(root / 'net/example')
            self.assertEqual('net/example', catalog.load(root)['example']['path'])
            self.fixture(root, 'www')
            with self.assertRaisesRegex(ValueError, 'duplicate'):
                catalog.load(root)

    def test_descriptive_metadata_does_not_change_the_build_digest(self):
        from unittest.mock import patch
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            directory, meta = self.fixture(root)
            (root / 'wolfi').mkdir()
            (root / 'scripts').mkdir()
            for name in ('tools.lock.json', 'scripts/build.py', 'scripts/common.py', 'scripts/catalog.py'):
                (root / name).write_text('fixture')
            with patch.object(common, 'ROOT', root):
                before = common.input_digest('example')
                meta['wolfi']['removal_condition'] = 'More specific review condition.'
                (directory / 'metadata.json').write_text(json.dumps(meta))
                self.assertEqual(before, common.input_digest('example'))
                (directory / 'recipe.yaml').write_text('changed build input')
                self.assertNotEqual(before, common.input_digest('example'))


if __name__ == '__main__':
    unittest.main()
