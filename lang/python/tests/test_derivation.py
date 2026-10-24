import copy
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT / 'scripts'))
import derive
import packages
import publish
import verify

HERE = Path(__file__).resolve().parent
FIXTURE = yaml.safe_load((HERE / 'fixtures/python-3.15.yaml').read_text())
COMMIT = next(s for s in FIXTURE['pipeline'] if s.get('uses') == 'git-checkout')['with']['expected-commit']
LOCK = {'recipe': 'python-3.15.yaml', 'version': '3.15.0_git20261017', 'epoch': 3, 'revision': 1,
        'source_commit': COMMIT}


class Derivation(unittest.TestCase):
    def derived(self, change=None, lock=None):
        """The package and derive() over a copy of Wolfi's recipe, after change(recipe)."""
        upstream = copy.deepcopy(FIXTURE)
        if change:
            change(upstream)
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        root = Path(self.directory.name)
        shutil.copytree(ROOT.parent / 'lang/python', root / 'packages/python',
                        ignore=shutil.ignore_patterns('__pycache__'))
        pkg = packages.Package('python', root=root)
        pkg.inputs.mkdir(parents=True)
        (pkg.inputs / 'upstream.yaml').write_text(yaml.safe_dump(upstream))
        (pkg.inputs / 'upstream.lock.json').write_text(json.dumps(dict(LOCK, **(lock or {}))))
        return pkg, upstream, derive.derive(pkg)

    def names(self, derived):
        return [s.get('name') or s.get('uses') for s in derived['pipeline']]

    def runtime(self, recipe):
        return next(s for s in recipe['subpackages'] if s['name'] == '${{package.name}}${{range.key}}-runtime')

    def test_wolfi_names_and_a_version_above_every_wolfi_rebuild(self):
        pkg, _, derived = self.derived()
        self.assertEqual('python-3.15', derived['package']['name'])
        self.assertEqual(1003, derived['package']['epoch'])
        self.assertEqual('3.15.0_git20261017-r1003', pkg.hooks.full_version(LOCK))
        self.assertEqual(['${{package.name}}${{range.key}}-runtime', '${{package.name}}${{range.key}}-base',
                          '${{package.name}}${{range.key}}-dev', '${{package.name}}${{range.key}}-base-dev'],
                         [s['name'] for s in derived['subpackages']])
        self.assertEqual({'python-3.15', 'python-3.15-runtime', 'python-3.15-base', 'python-3.15-dev',
                          'python-3.15-base-dev'}, publish.package_names(derived))
        for section in [derived['package']] + derived['subpackages']:
            self.assertIn('upstream-origin:python-3.15:3.15.0_git20261017-r3', section['dependencies']['provides'])

    def test_the_locked_commit_is_fetched_in_place_of_wolfis_checkout(self):
        _, _, derived = self.derived()
        self.assertEqual('Fetch the locked source commit', derived['pipeline'][0]['name'])
        self.assertIn(COMMIT, derived['pipeline'][0]['runs'])
        self.assertFalse([s for s in derived['pipeline'] if s.get('uses') == 'git-checkout'])

    def test_our_patches_follow_wolfis_before_configuring_the_ordinary_interpreter(self):
        _, _, derived = self.derived()
        names = self.names(derived)
        ours = names.index('Apply Werewolf policy patches')
        self.assertEqual('patch', derived['pipeline'][ours - 1]['uses'])
        self.assertNotIn('Clone patched source for no-GIL and JIT builds', names)
        self.assertIn('Allow every runtime-hardening group in the build guest only', names)
        # The interpreter configures in -runtime, whose pipeline runs after the main one.
        self.assertNotIn('autoconf/configure', names)
        self.assertIn('autoconf/configure', [s.get('uses') for s in self.runtime(derived)['pipeline']])

    def test_loadable_sqlite_extensions_alone_leave_the_configure_options(self):
        _, upstream, derived = self.derived()
        before = upstream['vars']['config-opts-common'].splitlines()
        after = derived['vars']['config-opts-common'].splitlines()
        self.assertEqual([o for o in before if 'loadable-sqlite' not in o], after)
        self.assertEqual(len(before) - 1, len(after))

    def test_the_probe_runs_in_the_package_test(self):
        _, _, derived = self.derived()
        self.assertIn('sh /tmp/probe/probe.sh /tmp/probe/probe.py', derived['test']['pipeline'][-1]['runs'])
        self.assertEqual({'enabled': False}, derived['update'])

    def test_test_recipe_grants_every_group_in_each_test_environment(self):
        pkg, _, derived = self.derived()
        tested = pkg.hooks.test_recipe(copy.deepcopy(derived), 'http://127.0.0.1:0')
        for section in [tested] + tested['subpackages']:
            test = section.get('test')
            if test and test.get('pipeline'):
                self.assertEqual('Grant every group for the suite (the probe clears them)',
                                 test['pipeline'][0]['name'])
        self.assertIn('/etc/runtime-hardening/allow/all/python', tested['test']['pipeline'][0]['runs'])

    def test_the_build_guest_grants_every_group_in_the_portable_tree(self):
        _, _, derived = self.derived()
        allow = next(s['runs'] for s in derived['pipeline']
                     if s.get('name') == 'Allow every runtime-hardening group in the build guest only')
        self.assertIn('/etc/runtime-hardening/allow/all/python', allow)
        for group in ['native', 'deserialize', 'inline', 'writable-source', 'environment']:
            self.assertIn(group, allow)

    def test_verify_accepts_the_derivation_of_wolfis_recipe(self):
        pkg, upstream, derived = self.derived()
        verify.check(pkg, upstream, derived)

    def test_verify_refuses_restoring_any_omitted_subpackage(self):
        pkg, upstream, derived = self.derived()
        for sub in upstream['subpackages']:
            if sub['name'] in [s['name'] for s in derived['subpackages']]:
                continue
            restored = copy.deepcopy(derived)
            restored['subpackages'].append(copy.deepcopy(sub))
            with self.subTest(sub=sub['name']), self.assertRaisesRegex(AssertionError, 'subpackage'):
                verify.check(pkg, upstream, restored)

    def test_cleanup_discards_gui_and_manpages_but_preserves_runtime_and_headers(self):
        pkg, _, derived = self.derived()
        # -runtime's last step runs before -base, -dev and -base-dev take their files.
        root = Path(self.directory.name) / 'installed files'
        kept = ['usr/lib/python3.15/os.py',
                'usr/lib/python3.15/lib-dynload/_sqlite3.cpython-315.so',
                'usr/include/python3.15/Python.h', 'usr/bin/python3.15']
        removed = ['usr/lib/python3.15/tkinter/__init__.py',
                   'usr/lib/python3.15/tkinter/__pycache__/__init__.cpython-315.pyc',
                   'usr/lib/python3.15/lib-dynload/_tkinter.cpython-315-aarch64-linux-gnu.so',
                   'usr/share/man/man1/python3.15.1']
        for name in kept + removed:
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.touch()
        last = self.runtime(derived)['pipeline'][-1]
        self.assertEqual(pkg.hooks.PRUNE, last['name'])
        script = last['runs'].replace('${{targets.contextdir}}', str(root)).replace(
            '${{vars.python}}', 'python3.15')
        subprocess.run(['sh', '-eu', '-c', script], check=True)
        self.assertEqual(set(kept), {str(p.relative_to(root)) for p in root.rglob('*') if p.is_file()})

    def test_the_c_extension_test_names_its_directory(self):
        # Ours leaves the working directory off sys.path, which Wolfi's test relies on.
        pkg, upstream, derived = self.derived()
        for sub in derived['subpackages']:
            steps = sub.get('test', {}).get('pipeline', [])
            self.assertNotIn(pkg.hooks.C_EXT, [s.get('uses') for s in steps])
        dev = {s['name']: s for s in derived['subpackages']}
        for name in ('${{package.name}}${{range.key}}-dev', '${{package.name}}${{range.key}}-base-dev'):
            runs = [s.get('runs', '') for s in dev[name]['test']['pipeline']]
            self.assertTrue(any('sys.path.insert(0, sys.argv[1])' in r for r in runs), name)

    def test_changed_recipes_stop_the_derivation_for_review(self):
        def repository(recipe):
            recipe['pipeline'][0]['with']['repository'] = 'https://example.com/cpython'

        def option(recipe):
            recipe['vars']['config-opts-common'] = recipe['vars']['config-opts-common'].replace(
                '--enable-loadable-sqlite-extensions \\\n', '')

        def clone(recipe):
            recipe['pipeline'] = [s for s in recipe['pipeline']
                                  if s.get('name') != 'Clone patched source for no-GIL and JIT builds']

        def late_patch(recipe):
            patch = next(s for s in recipe['pipeline'] if s.get('uses') == 'patch')
            recipe['pipeline'].append(copy.deepcopy(patch))

        def name(recipe):
            recipe['package']['name'] = 'python-3.16'

        def second_checkout(recipe):
            recipe['pipeline'].insert(1, copy.deepcopy(recipe['pipeline'][0]))

        def privileged_subpackage(recipe):
            next(s for s in recipe['subpackages']
                 if s['name'] == '${{package.name}}-privileged-netbindservice')['name'] += '-changed'

        def unknown_subpackage(recipe):
            recipe['subpackages'].append({'name': '${{package.name}}-new'})

        def missing_subpackage(recipe):
            recipe['subpackages'].pop()

        def variant(recipe):
            recipe['data'][0]['items']['t'] = '${{vars.config-opts-freethreading}}'

        for change in [repository, option, clone, late_patch, name, second_checkout,
                       privileged_subpackage, unknown_subpackage, missing_subpackage, variant]:
            with self.subTest(change.__name__), self.assertRaisesRegex(ValueError, 'Review'):
                self.derived(change)

    def test_a_source_pin_other_than_the_lock_is_refused(self):
        with self.assertRaises(AssertionError):
            self.derived(lock={'source_commit': '0' * 40})

    def test_verify_refuses_a_relaxed_derivation(self):
        pkg, upstream, derived = self.derived()
        for relax in [
            lambda d: d['pipeline'].remove(next(s for s in d['pipeline'] if s.get('uses') == 'patch')),
            lambda d: d['environment']['contents']['packages'].remove('build-base'),
            lambda d: d['test']['pipeline'].pop(0),
            lambda d: d['subpackages'][0]['pipeline'].pop(),
            lambda d: self.runtime(d)['pipeline'].pop(),
            lambda d: self.runtime(d)['pipeline'][-1].update(runs='true\n'),
            lambda d: d['data'][0]['items'].update(t='${{vars.config-opts-freethreading}}'),
            lambda d: d['pipeline'].insert(0, copy.deepcopy(next(s for s in upstream['pipeline']
                if s.get('name') == 'Clone patched source for no-GIL and JIT builds'))),
            lambda d: d['vars'].update({'config-opts-common': d['vars']['config-opts-common'] + '--enable-loadable-sqlite-extensions \\\n'}),
            lambda d: d.update(update={'enabled': True}),
        ]:
            relaxed = copy.deepcopy(derived)
            relax(relaxed)
            with self.subTest(relax=relax), self.assertRaises(AssertionError):
                verify.check(pkg, upstream, relaxed)

    def test_epoch_moves_with_wolfi_and_with_the_policy_never_colliding(self):
        pkg, _, _ = self.derived()
        seen = {pkg.hooks.epoch(e, r) for e in range(0, 50) for r in range(1, 20)}
        self.assertEqual(50 * 19, len(seen))
        with self.assertRaises(ValueError):
            pkg.hooks.epoch(1000, 1)

    def test_the_probe_cannot_end_its_heredoc_early_or_use_melange_variables(self):
        pkg, _, _ = self.derived()
        for text in ['${{package.name}}', pkg.hooks.PROBE_PY, pkg.hooks.PROBE_SH]:
            (pkg.dir / 'tests/probe.sh').write_text('echo ' + text + '\n')
            with self.subTest(text=text), self.assertRaises(ValueError):
                pkg.hooks.probe(pkg)


if __name__ == '__main__':
    unittest.main()
