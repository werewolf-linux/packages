import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import runtime


class RuntimePublication(unittest.TestCase):
    def parts(self, name):
        data = io.BytesIO()
        with tarfile.open(fileobj=data, mode='w') as archive:
            archive.addfile(tarfile.TarInfo(name))
        return [None, None, (b'', data.getvalue())]

    def test_no_package_can_ship_its_own_exception(self):
        for name in ('etc/runtime-hardening', './etc/runtime-hardening/build-root',
                     '/etc/runtime-hardening/build-root', 'usr/../etc/runtime-hardening/build-root',
                     'etc/runtime-hardening/allow/all/node/exec'):
            with self.subTest(name=name), self.assertRaises(ValueError):
                runtime.check_apk(None, 'runtime.apk', self.parts(name))
        runtime.check_apk(None, 'runtime.apk', self.parts('usr/bin/node'))


@unittest.skipUnless(shutil.which('cc'), 'C compiler required')
class RootGuard(unittest.TestCase):
    def test_each_shipped_guard_uses_effective_uid_and_refuses_non_regular_markers(self):
        patches = list(ROOT.parent.glob('*/*/patches/*-refuse-effective-root.patch'))
        self.assertEqual({'node', 'python', 'php', 'openjdk'}, {p.parents[1].name for p in patches})
        for patch in patches:
            with self.subTest(runtime=patch.parents[1].name), tempfile.TemporaryDirectory() as directory:
                work = Path(directory)
                # Compile the actual new header from each runtime's patch. Only
                # the effective identity and file owner are simulated here;
                # package probes exercise real setresuid mismatches on Linux.
                added_file = next(block for block in patch.read_text().split('diff --git ')
                                  if any(line.startswith('+++ b/') and line.endswith('/runtime_hardening_root.h')
                                         for line in block.splitlines()))
                header = '\n'.join(line[1:] for line in added_file.splitlines()
                                   if line.startswith('+') and not line.startswith('+++')) + '\n'
                (work / 'guard.h').write_text(header)
                (work / 'probe.c').write_text(r'''
#include <sys/stat.h>
#include <unistd.h>
#include <stdlib.h>
static uid_t effective;
static uid_t test_euid(void) { return effective; }
static int test_lstat(const char *path, struct stat *st) {
    int result = lstat(path, st);
    if (!result) st->st_uid = 0;
    return result;
}
#define geteuid test_euid
#define getuid forbidden_real_uid
#define lstat test_lstat
#include "guard.h"
int main(int argc, char **argv) {
    effective = (uid_t)strtoul(argv[1], NULL, 10);
    return runtime_hardening_root_refused();
}
''')
                marker = work / 'marker'
                subprocess.run(['cc', '-Wall', '-Werror', '-DRUNTIME_HARDENING_BUILD_ROOT="' + str(marker) + '"',
                                str(work / 'probe.c'), '-o', str(work / 'probe')], check=True)

                def run(uid):
                    return subprocess.run([str(work / 'probe'), str(uid)]).returncode

                self.assertEqual(1, run(0))
                self.assertEqual(0, run(65534))
                marker.touch(mode=0o600)
                self.assertEqual(0, run(0))
                marker.chmod(0o666)
                self.assertEqual(1, run(0))
                marker.unlink()
                target = work / 'target'
                target.touch()
                marker.symlink_to(target)
                self.assertEqual(1, run(0))
                marker.unlink()
                marker.mkdir()
                self.assertEqual(1, run(0))


class DenialLogging(unittest.TestCase):
    def test_native_loggers_match_the_shared_implementation(self):
        expected = (ROOT / 'scripts/runtime_hardening_log.h').read_text()
        patches = list(ROOT.parent.glob('*/*/patches/*-log-denials-to-stderr.patch'))
        self.assertEqual({'node', 'python', 'php', 'openjdk'}, {p.parents[1].name for p in patches})
        for patch in patches:
            block = next(b for b in patch.read_text().split('diff --git ')
                         if any(line.startswith('+++ b/') and line.endswith('/runtime_hardening_log.h')
                                for line in b.splitlines()))
            actual = '\n'.join(line[1:] for line in block.splitlines()
                               if line.startswith('+') and not line.startswith('+++')) + '\n'
            self.assertEqual(expected, actual, patch.name)

    @unittest.skipUnless(shutil.which('cc'), 'C compiler required')
    def test_native_records_escape_bound_and_preserve_failure_state(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'probe.c').write_text(r'''
#include "runtime_hardening_log.h"
int main(int argc, char **argv) {
    if (argc > 2) close(STDERR_FILENO);
    errno = EACCES;
    runtime_hardening_denied("node", "exec", "spawn", argv[1]);
    return errno != EACCES;
}
''')
            subprocess.run(['cc', '-Wall', '-Werror', '-I', str(ROOT / 'scripts'),
                            str(work / 'probe.c'), '-o', str(work / 'probe')], check=True)
            detail = 'command "quoted"\n\x1b\\argument'
            result = subprocess.run([str(work / 'probe'), detail], capture_output=True, check=True)
            self.assertEqual(b'', result.stdout)
            self.assertEqual(1, len(result.stderr.splitlines()))
            record = json.loads(result.stderr)
            self.assertEqual('runtime-denial', record['event'])
            self.assertEqual(('node', 'exec', 'spawn', detail),
                             tuple(record[k] for k in ('runtime', 'group', 'operation', 'detail')))
            self.assertEqual(os.geteuid(), record['euid'])
            self.assertFalse(record['truncated'])
            self.assertGreater(record['pid'], 0)
            result = subprocess.run([str(work / 'probe'), '\x01' * 8000], capture_output=True, check=True)
            record = json.loads(result.stderr)
            self.assertTrue(record['truncated'])
            self.assertEqual(192, len(record['detail']))
            self.assertLessEqual(len(result.stderr), 4096)
            # Closed stderr must not change the refusal or errno.
            result = subprocess.run([str(work / 'probe'), 'failure', 'close-stderr'],
                                    capture_output=True, check=True)
            self.assertEqual(b'', result.stderr)

    @unittest.skipUnless(shutil.which('javac') and shutil.which('java') and shutil.which('patch'),
                         'Java compiler and patch required')
    def test_java_uses_original_stderr_and_emits_the_same_fields(self):
        version = subprocess.run(['javac', '-version'], capture_output=True)
        if version.returncode:
            self.skipTest('Java compiler is unavailable')
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            name = 'src/java.base/share/classes/jdk/internal/misc/WerewolfAllowance.java'
            path = work / name
            path.parent.mkdir(parents=True)
            patches = ROOT.parent / 'lang/openjdk/patches'
            original = (patches / '0003-werewolf-allowances.patch').read_text()
            path.write_text('\n'.join(line[1:] for line in original.splitlines()
                                      if line.startswith('+') and not line.startswith('+++')) + '\n')
            patch = (patches / '0008-log-denials-to-stderr.patch').read_text()
            block = next(block for block in patch.split('diff --git ') if block.startswith('a/' + name + ' '))
            subprocess.run(['patch', '-p1', '--batch', '--fuzz=0'], input='diff --git ' + block,
                           text=True, cwd=work, capture_output=True, check=True)
            classes = work / 'classes'
            subprocess.run(['javac', '--patch-module', 'java.base=' + str(path.parent),
                            '-d', str(classes), str(path)], capture_output=True, check=True)
            (work / 'Probe.java').write_text('''
public class Probe {
    public static void main(String[] args) {
        System.setErr(new java.io.PrintStream(java.io.OutputStream.nullOutputStream()));
        jdk.internal.misc.WerewolfAllowance.denied("native", "Unsafe.freeMemory", args[0]);
        jdk.internal.misc.WerewolfAllowance.denied("deserialize", "readObject", "java.lang.Integer");
        System.out.println("continued");
    }
}
''')
            options = ['--patch-module', 'java.base=' + str(classes),
                       '--add-exports', 'java.base/jdk.internal.misc=ALL-UNNAMED']
            subprocess.run(['javac', *options, '-d', str(classes), str(work / 'Probe.java')],
                           capture_output=True, check=True)
            detail = 'argument "quoted"\n\x1b\\path'
            result = subprocess.run(['java', *options, '-cp', str(classes), 'Probe', detail],
                                    capture_output=True, check=True)
            self.assertEqual(b'continued\n', result.stdout)
            records = [json.loads(line) for line in result.stderr.splitlines()]
            self.assertEqual(2, len(records))
            self.assertEqual('java', records[0]['runtime'])
            self.assertEqual(detail, records[0]['detail'])
            self.assertEqual('runtime-denial', records[1]['event'])
            self.assertEqual('deserialize', records[1]['group'])
            self.assertEqual('java.lang.Integer', records[1]['detail'])
            self.assertLess(len(result.stderr), 4096)


if __name__ == '__main__':
    unittest.main()
