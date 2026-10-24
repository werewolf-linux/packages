"""The Plex vendor package keeps signed provenance and exact build inputs."""
import importlib.util
import json
from pathlib import Path
import shutil
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1] / 'plex'
spec = importlib.util.spec_from_file_location('plex_provenance', ROOT / 'verify.py')
provenance = importlib.util.module_from_spec(spec)
spec.loader.exec_module(provenance)


class PlexProvenance(unittest.TestCase):
    def copied(self, directory):
        target = Path(directory) / 'plex'
        shutil.copytree(ROOT, target)
        return target

    def test_signed_inputs_and_codec_pins(self):
        source = provenance.verify()
        self.assertEqual(set(source['architectures']), {'aarch64', 'x86_64'})
        for arch in source['architectures'].values():
            self.assertGreaterEqual(len(arch['codecs']), 6)

    def test_changed_package_index_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            copy = self.copied(directory)
            with (copy / 'Packages.arm64').open('a') as out:
                out.write('Package: attacker\n')
            with self.assertRaisesRegex(ValueError, 'altered package index'):
                provenance.verify(copy)

    def test_changed_codec_recipe_hash_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            copy = self.copied(directory)
            source = json.loads((copy / 'sources.json').read_text())
            digest = source['architectures']['aarch64']['codecs'][0]['fileSha256']
            recipe = copy / 'plex.yaml'
            recipe.write_text(recipe.read_text().replace(digest, '0' * 64))
            with self.assertRaisesRegex(ValueError, 'Recipe checksum differs'):
                provenance.verify(copy)

    def test_changed_vendor_archive_pin_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            copy = self.copied(directory)
            path = copy / 'sources.json'
            source = json.loads(path.read_text())
            source['architectures']['x86_64']['deb']['sha256'] = '0' * 64
            path.write_text(json.dumps(source))
            with self.assertRaisesRegex(ValueError, 'Vendor package identity'):
                provenance.verify(copy)


if __name__ == '__main__':
    unittest.main()
