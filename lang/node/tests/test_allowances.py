from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent


@unittest.skipUnless(shutil.which('c++'), 'C++ compiler required')
class Allowances(unittest.TestCase):
    def test_regular_grants_and_mount_provenance_fail_closed(self):
        patch = next((HERE.parent / 'patches').glob('0002-*.patch')).read_text()
        header = '\n'.join(line[1:] for line in patch.splitlines()
                           if line.startswith('+') and not line.startswith('+++')) + '\n'
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'hardening.h').write_text(header)
            (root / 'probe.cc').write_text(r'''
#include <sys/statvfs.h>
#include <cstring>
static int mount_info(const char *path, struct statvfs *info) {
  if (!strcmp(path, "unknown")) return -1;
  info->f_flag = !strcmp(path, "readonly") ? ST_RDONLY : 0;
  return 0;
}
#define statvfs(path, info) mount_info(path, info)
#include "hardening.h"
int main(int argc, char **argv) {
  if (!strcmp(argv[1], "source")) return !node::hardening::SourceAllowed(argv[2]);
  return !node::hardening::Allowed(argv[1]);
}
''')
            subprocess.run(['c++', '-std=c++20', '-Wall', '-Werror',
                            '-DNODE_RUNTIME_HARDENING_DIR="' + str(root / 'allow') + '"',
                            str(root / 'probe.cc'), '-o', str(root / 'probe')], check=True)

            def allowed(*args):
                return subprocess.run([str(root / 'probe'), *args]).returncode == 0

            self.assertFalse(allowed('exec'))
            self.assertFalse(allowed('source', 'unknown'))
            self.assertFalse(allowed('source', 'writable'))
            self.assertTrue(allowed('source', 'readonly'))
            base = root / 'allow/all/node'
            base.mkdir(parents=True)
            (base / 'native').touch()
            self.assertTrue(allowed('native'))
            self.assertFalse(allowed('exec'))
            (base / 'exec').symlink_to(base / 'native')
            self.assertFalse(allowed('exec'))
            (base / 'exec').unlink()
            (base / 'exec').mkdir()
            self.assertFalse(allowed('exec'))
            (base / 'writable-source').touch()
            self.assertTrue(allowed('source', 'writable'))
            self.assertTrue(allowed('source', 'unknown'))


if __name__ == '__main__':
    unittest.main()
