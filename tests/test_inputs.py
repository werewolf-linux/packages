from pathlib import Path
import os
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scripts'))
import common


class Inputs(unittest.TestCase):
    def test_every_context_has_a_unique_apk_name_including_vulnerable_apps(self):
        names = set()
        for name in common.contexts():
            meta = common.metadata(name)
            self.assertFalse(names.intersection(meta['names']), name)
            names.update(meta['names'])
        for name in ['solr', 'n8n', 'langflow']:
            self.assertIn(name, names)
            self.assertIn(name + '-vuln-demo', names)

    def test_context_bytes_modes_and_symlinks_are_bound(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'example').mkdir()
            (root / 'scripts').mkdir()
            for name in ['example/test.yaml', 'example/test.patch', 'tools.lock.json', 'scripts/build.py', 'scripts/common.py']:
                (root / name).write_text('original')
            with patch.object(common, 'ROOT', root):
                original = common.input_digest('example')
                file = root / 'example/test.patch'
                old = file.stat()
                file.write_text('modified')
                os.utime(file, ns=(old.st_atime_ns, old.st_mtime_ns))
                changed = common.input_digest('example')
                self.assertNotEqual(original, changed)
                file.chmod(0o755)
                self.assertNotEqual(changed, common.input_digest('example'))
                file.unlink()
                file.symlink_to('/etc/passwd')
                with self.assertRaisesRegex(ValueError, 'symlinks'):
                    common.input_digest('example')

    def test_secret_material_is_not_passed_to_native_builds(self):
        source = (common.ROOT / 'scripts/build.py').read_text()
        self.assertNotIn('os.environ.copy()', source)
        self.assertNotIn('PACKAGES_APK_SIGNING_KEY', source)
        workflow = (common.ROOT / '.github/workflows/build.yml').read_text()
        self.assertNotIn('secrets.', workflow)
        self.assertNotIn('environment: release', workflow)


if __name__ == '__main__':
    unittest.main()
