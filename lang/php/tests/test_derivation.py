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
import patching
import publish
import verify

FIXTURE = yaml.safe_load((ROOT.parent / 'lang/php/tests/fixtures/php-8.5.yaml').read_text())
COMMIT = FIXTURE['pipeline'][0]['with']['expected-commit']
PHP = packages.Package('php')


class Derivation(unittest.TestCase):
    def derived(self, change=None, lock=None):
        """The derivation of a copy of Wolfi's recipe, after change(recipe)."""
        upstream = copy.deepcopy(FIXTURE)
        if change:
            change(upstream)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copytree(ROOT.parent / 'lang/php', root / 'packages/php', ignore=shutil.ignore_patterns('__pycache__'))
            pkg = packages.Package('php', root=root)
            pkg.inputs.mkdir(parents=True)
            (pkg.inputs / 'upstream.yaml').write_text(yaml.safe_dump(upstream))
            base = {'recipe': 'php-8.5.yaml', 'version': '8.5.11', 'epoch': 3, 'revision': 1, 'source_commit': COMMIT}
            (pkg.inputs / 'upstream.lock.json').write_text(json.dumps(dict(base, **(lock or {}))))
            return upstream, derive.derive(pkg)

    def test_wolfi_names_and_a_version_above_every_wolfi_rebuild(self):
        _, derived = self.derived()
        self.assertEqual('php-8.5', derived['package']['name'])
        self.assertEqual(1003, derived['package']['epoch'])
        self.assertEqual([s['name'] for s in FIXTURE['subpackages'] if s['name'] not in PHP.hooks.OMITTED_SUBPACKAGES],
                         [s['name'] for s in derived['subpackages']])
        for section in [derived['package']] + derived['subpackages']:
            self.assertIn('upstream-origin:php-8.5:8.5.11-r3', section['dependencies']['provides'])
            self.assertNotIn('replaces', section['dependencies'])

    def test_epoch_moves_with_wolfi_and_with_the_policy_never_colliding(self):
        seen = {PHP.hooks.epoch(e, r) for e in range(0, 50) for r in range(1, 20)}
        self.assertEqual(50 * 19, len(seen))
        self.assertGreater(PHP.hooks.epoch(0, 1), 999)
        with self.assertRaises(ValueError):
            PHP.hooks.epoch(1000, 1)
        self.assertEqual('8.5.11-r2004', PHP.hooks.full_version({'version': '8.5.11', 'epoch': 4, 'revision': 2}))

    def test_the_locked_source_then_wolfis_patches_then_ours(self):
        steps = self.derived()[1]['pipeline']
        self.assertEqual('Fetch the locked source commit', steps[0]['name'])
        self.assertIn('for source in https://github.com/php/php-src.git;', steps[0]['runs'])
        self.assertIn(COMMIT, steps[0]['runs'])
        self.assertEqual(['patch', 'patch'], [s.get('uses') for s in steps[1:3]])
        self.assertEqual(patching.step('werewolf/patches'), steps[3])

    def test_removed_facilities_are_neither_built_nor_packaged(self):
        derived = self.derived()[1]
        configure = next(s['runs'] for s in derived['pipeline'] if s.get('name') == 'Configure')
        for name, flag in PHP.hooks.REMOVED.items():
            self.assertNotIn(flag, configure)
            self.assertNotIn(name, [s['name'] for s in derived['subpackages']])
        for flag in list(PHP.hooks.OMITTED_EXTENSIONS.values()) + list(PHP.hooks.OMITTED_LIBRARIES):
            self.assertNotIn(flag, configure)
        self.assertIn('--disable-cgi \\\n', configure)
        self.assertIn('--disable-phar \\\n', configure)
        self.assertIn('--enable-cli \\\n', configure)
        self.assertIn('--enable-fpm \\\n', configure)
        self.assertFalse(any(s.get('name') in PHP.hooks.APXS_STEPS for s in derived['pipeline']))

    def test_only_the_omitted_tidy_test_and_dependency_are_removed(self):
        upstream, derived = self.derived()
        expected = [s for s in upstream['test']['pipeline'] if s.get('uses') != 'test/php-tidy']
        self.assertEqual(expected, derived['test']['pipeline'][:-1])
        packages = derived['test']['environment']['contents']['packages']
        self.assertNotIn('${{package.name}}-tidy', packages)
        self.assertNotIn('${{package.name}}-phar', packages)

    def test_manpage_cleanup_preserves_runtime_modules_and_headers(self):
        derived = self.derived()[1]
        self.assertEqual(PHP.hooks.PRUNE, derived['pipeline'][-1]['name'])
        with tempfile.TemporaryDirectory(prefix='php installed ') as directory:
            root = Path(directory)
            kept = ['usr/bin/php', 'usr/bin/php-fpm', 'usr/include/php/main/php.h',
                    'usr/lib/php/modules/pdo.so', 'usr/lib/php/modules/ldap.so']
            for name in kept + ['usr/share/man/man1/php.1', 'usr/share/man/man8/php-fpm.8']:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.touch()
            script = derived['pipeline'][-1]['runs'].replace('${{targets.destdir}}', str(root))
            subprocess.run(['sh', '-eu', '-c', script], check=True)
            self.assertEqual(set(kept), {str(p.relative_to(root)) for p in root.rglob('*') if p.is_file()})

    def test_test_recipe_grants_every_group_in_each_test_environment(self):
        import copy as _copy
        derived = self.derived()[1]
        tested = PHP.hooks.test_recipe(_copy.deepcopy(derived), 'http://127.0.0.1:0')
        self.assertEqual('Grant every group for the suite (the probe clears them)',
                         tested['test']['pipeline'][0]['name'])
        self.assertIn('/etc/runtime-hardening/allow/all/php', tested['test']['pipeline'][0]['runs'])
        for sub in tested['subpackages']:
            test = sub.get('test')
            if test and test.get('pipeline'):
                self.assertEqual('Grant every group for the suite (the probe clears them)',
                                 test['pipeline'][0]['name'])

    def test_the_probe_runs_in_the_package_test_with_what_it_loads(self):
        derived = self.derived()[1]
        self.assertIn('sh /tmp/probe.sh /tmp/probe.php', derived['test']['pipeline'][-1]['runs'])
        for name in PHP.hooks.PROBE_PACKAGES:
            self.assertIn(name, derived['test']['environment']['contents']['packages'])
        self.assertEqual({'enabled': False}, derived['update'])

    def test_verify_accepts_the_derivation(self):
        upstream, derived = self.derived()
        verify.check(PHP, upstream, derived)

    def test_a_future_upstream_flag_survives(self):
        def add(recipe):
            step = next(s for s in recipe['pipeline'] if s.get('name') == 'Configure')
            step['runs'] = step['runs'].replace('--enable-cli \\\n', '--enable-cli \\\n  --enable-future-protection \\\n')
        upstream, derived = self.derived(add)
        self.assertIn('--enable-future-protection', next(s['runs'] for s in derived['pipeline'] if s.get('name') == 'Configure'))
        verify.check(PHP, upstream, derived)

    def test_changed_recipes_stop_the_derivation_for_review(self):
        def checkout(recipe):
            recipe['pipeline'][0]['with']['recurse-submodules'] = 'true'
        def flag(recipe):
            step = next(s for s in recipe['pipeline'] if s.get('name') == 'Configure')
            step['runs'] = step['runs'].replace('--enable-phpdbg=shared', '--enable-phpdbg')
        def subpackage(recipe):
            recipe['subpackages'] = [s for s in recipe['subpackages'] if s['name'] != '${{package.name}}-dbg']
        def depends(recipe):
            fpm = next(s for s in recipe['subpackages'] if s['name'] == '${{package.name}}-fpm')
            fpm['dependencies']['runtime'].append('${{package.name}}-pear')
        def patches(recipe):
            recipe['pipeline'].insert(3, recipe['pipeline'].pop(1))
        def name(recipe):
            recipe['package']['name'] = 'php-8.6'
        def new_extension(recipe):
            recipe['data'][0]['items']['new'] = 'Needs a consumer'
        def new_subpackage(recipe):
            recipe['subpackages'].append({'name': '${{package.name}}-new'})
        def cgi(recipe):
            next(s for s in recipe['pipeline'] if s.get('name') == 'Configure')['runs'] += '--enable-cgi\n'
        def apache_step(recipe):
            recipe['pipeline'] = [s for s in recipe['pipeline'] if s.get('name') != PHP.hooks.APXS_STEPS[0]]
        def tidy_test(recipe):
            recipe['test']['pipeline'] = [s for s in recipe['test']['pipeline'] if s.get('uses') != 'test/php-tidy']
        for change in [checkout, flag, subpackage, depends, patches, name,
                       new_extension, new_subpackage, cgi, apache_step, tidy_test]:
            with self.subTest(change.__name__), self.assertRaisesRegex(ValueError, 'Review'):
                self.derived(change)

    def test_a_source_pin_other_than_the_lock_is_refused(self):
        with self.assertRaises(AssertionError):
            self.derived(lock={'source_commit': '0' * 40})

    def test_verify_refuses_a_relaxed_derivation(self):
        upstream, derived = self.derived()
        for relax in [
            lambda d: d['pipeline'].remove(next(s for s in d['pipeline'] if s.get('uses') == 'patch')),
            lambda d: d['environment']['contents']['packages'].remove('build-base'),
            lambda d: d['test']['pipeline'].pop(0),
            lambda d: d.update(update={'enabled': True}),
            lambda d: d['pipeline'].remove(patching.step('werewolf/patches')),
            lambda d: d['pipeline'].pop(),
            lambda d: d['pipeline'][-1].update(runs='true\n'),
            lambda d: d['data'][0]['items'].update({'phar': 'PHP Archive'}),
            lambda d: d['subpackages'].append(copy.deepcopy(next(s for s in upstream['subpackages']
                if s['name'] == '${{package.name}}-cgi'))),
            lambda d: next(s for s in d['pipeline'] if s.get('name') == 'Configure').update(
                runs=next(s for s in d['pipeline'] if s.get('name') == 'Configure')['runs'].replace('--disable-phar', '')),
        ]:
            relaxed = copy.deepcopy(derived)
            relax(relaxed)
            with self.subTest(relax=relax), self.assertRaises(AssertionError):
                verify.check(PHP, upstream, relaxed)

    def test_the_probe_cannot_end_its_heredoc_early_or_use_melange_variables(self):
        for text in ['${{package.name}}', PHP.hooks.PROBE_PHP, PHP.hooks.PROBE_SH,
                     PHP.hooks.PROBE_CAPABILITIES]:
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                shutil.copytree(ROOT.parent / 'lang/php', root / 'packages/php', ignore=shutil.ignore_patterns('__pycache__'))
                pkg = packages.Package('php', root=root)
                (pkg.dir / 'tests/probe.sh').write_text('echo ' + text + '\n')
                with self.assertRaises(ValueError):
                    pkg.hooks.probe(pkg)

    def test_every_range_and_named_subpackage_is_published(self):
        names = publish.package_names(self.derived()[1])
        for name in ['php-8.5', 'php-8.5-fpm', 'php-8.5-pcntl', 'php-8.5-pcntl-config',
                     'php-8.5-dev', 'php-8.5-sysvsem', 'php-8.5-soap', 'php-8.5-ldap']:
            self.assertIn(name, names)
        omitted = {'dbg', 'pear', 'apache', 'cgi', 'doc', 'bz2', 'gettext', 'pdo_dblib', 'pdo_odbc',
                   'odbc', 'sockets', 'tidy', 'xsl', 'ftp', 'sysvshm', 'phar'}
        for suffix in omitted:
            self.assertNotIn('php-8.5-' + suffix, names)
            self.assertNotIn('php-8.5-' + suffix + '-config', names)
        self.assertEqual(67, len(names))


if __name__ == '__main__':
    unittest.main()
