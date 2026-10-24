"""The merged entry points select local tools and preserve source timestamps."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[1]


class BuildTools(unittest.TestCase):
    def test_root_make_uses_the_installed_pinned_builder(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            (root / 'Makefile').write_text((ROOT / 'Makefile').read_text() + '\nprint-tool:\n\t@echo $(MELANGE)\n')
            local = root / 'wolfi/build/tools/melange'
            local.parent.mkdir(parents=True)
            local.touch()
            env = os.environ.copy()
            env.pop('MELANGE', None)
            command = ['make', '--no-print-directory', 'print-tool']
            result = subprocess.check_output(command, cwd=root, env=env, text=True).strip()
            self.assertEqual(str(local), result)
            result = subprocess.check_output(command + ['MELANGE=/explicit/melange'], cwd=root, env=env, text=True).strip()
            self.assertEqual('/explicit/melange', result)

    def test_wolfi_tests_cannot_succeed_with_an_invalid_catalog(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            (root / 'wolfi').mkdir()
            (root / 'scripts').mkdir()
            shutil.copy2(ROOT / 'wolfi/Makefile', root / 'wolfi/Makefile')
            (root / 'scripts/catalog.py').write_text('raise SystemExit("invalid catalog")\n')
            result = subprocess.run(['make', 'PYTHON=' + sys.executable, 'test'], cwd=root / 'wolfi', capture_output=True, text=True)
            self.assertNotEqual(0, result.returncode)
            self.assertIn('invalid catalog', result.stderr)

    def test_self_hosted_checkout_cleans_both_build_engines(self):
        for name in ('build', 'check', 'publish', 'wolfi-build'):
            with self.subTest(workflow=name), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                workflow = yaml.safe_load((ROOT / '.github/workflows' / (name + '.yml')).read_text())
                steps = next(j['steps'] for j in workflow['jobs'].values() if 'steps' in j)
                cleanup = next(s for s in steps if s.get('name', '').startswith('Remove what'))
                self.assertLess(steps.index(cleanup), next(i for i, s in enumerate(steps) if s.get('uses', '').startswith('actions/checkout@')))
                for directory in ('build', 'wolfi/build'):
                    (root / directory).mkdir(parents=True)
                    (root / directory / 'leftover').touch()
                tools = root / 'tools'
                tools.mkdir()
                unshare = tools / 'unshare'
                unshare.write_text('#!/bin/sh\nwhile [ "$1" != rm ]; do shift; done\nexec "$@"\n')
                unshare.chmod(0o755)
                (root / 'source').touch()
                subprocess.run(['/bin/sh', '-eu', '-c', cleanup['run']], cwd=root,
                               env={**os.environ, 'PATH': str(tools) + os.pathsep + os.environ['PATH']}, check=True)
                self.assertFalse((root / 'build').exists())
                self.assertFalse((root / 'wolfi/build').exists())
                self.assertTrue((root / 'source').exists())

    def test_native_checkout_preserves_the_context_commit_timestamp(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            source, checkout = root / 'source', root / 'checkout'
            source.mkdir()
            def git(*args, env=None):
                return subprocess.check_output(['git', '-C', str(source), *args], env=env, text=True).strip()
            git('init', '--quiet')
            git('config', 'user.name', 'Package test')
            git('config', 'user.email', 'test@example.invalid')
            git('config', 'commit.gpgsign', 'false')
            context = source / 'www/example'
            context.mkdir(parents=True)
            (context / 'recipe.yaml').write_text('package: example\n')
            git('add', '.')
            stamp = '2001-01-01T00:00:00+0000'
            git('commit', '--quiet', '-m', 'package', env={**os.environ, 'GIT_AUTHOR_DATE': stamp, 'GIT_COMMITTER_DATE': stamp})
            expected = git('log', '-1', '--format=%ct', '--', 'www/example')
            (source / 'unrelated.txt').write_text('unrelated change\n')
            git('add', '.')
            stamp = '2002-01-01T00:00:00+0000'
            git('commit', '--quiet', '-m', 'unrelated', env={**os.environ, 'GIT_AUTHOR_DATE': stamp, 'GIT_COMMITTER_DATE': stamp})
            workflow = yaml.safe_load((ROOT / '.github/workflows/build.yml').read_text())
            step = next(s for s in workflow['jobs']['build']['steps'] if s.get('uses', '').startswith('actions/checkout@'))
            depth = step.get('with', {}).get('fetch-depth', 1)
            args = ['git', 'clone', '--quiet', '--no-local']
            if depth:
                args += ['--depth', str(depth)]
            subprocess.run(args + [str(source), str(checkout)], check=True)
            actual = subprocess.check_output(['git', '-C', str(checkout), 'log', '-1', '--format=%ct', '--', 'www/example'], text=True).strip()
            self.assertEqual(expected, actual)


if __name__ == '__main__':
    unittest.main()
