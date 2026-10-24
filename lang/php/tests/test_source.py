from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent


@unittest.skipUnless(shutil.which('cc'), 'C compiler required')
class Source(unittest.TestCase):
    def test_only_a_proven_regular_file_on_a_readonly_mount_is_code(self):
        patch = (HERE.parent / 'patches/0012-verify-all-compiled-files.patch').read_text()
        patch = patch.split('+++ b/main/runtime_hardening_source.h\n', 1)[1]
        header = '\n'.join(line[1:] for line in patch.splitlines()
                           if line.startswith('+')) + '\n'
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'source.h').write_text(header)
            (root / 'probe.c').write_text(r'''
#include <sys/stat.h>
#include <sys/statvfs.h>
static int file_info(int fd, struct stat *st) {
    if (fd == 1) return -1;
    st->st_mode = fd == 2 ? S_IFIFO : S_IFREG;
    return 0;
}
static int mount_info(int fd, struct statvfs *vfs) {
    if (fd == 3) return -1;
    vfs->f_flag = fd == 4 ? 0 : ST_RDONLY;
    return 0;
}
#define fstat(fd, st) file_info(fd, st)
#define fstatvfs(fd, vfs) mount_info(fd, vfs)
#include "source.h"
int main(void) {
    for (int fd = -1; fd <= 5; fd++) {
        if (!!runtime_hardening_source_fd(fd) != (fd == 0 || fd == 5)) return 1;
    }
    return 0;
}
''')
            subprocess.run(['cc', '-Wall', '-Wextra', '-Werror', str(root / 'probe.c'),
                            '-o', str(root / 'probe')], check=True)
            subprocess.run([str(root / 'probe')], check=True)


if __name__ == '__main__':
    unittest.main()
