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
import packages
import patching
import publish
import verify

FIXTURE = yaml.safe_load((ROOT.parent / 'lang/openjdk/tests/fixtures/openjdk-27.yaml').read_text())
COMMIT = next(s for s in FIXTURE['pipeline'] if s.get('uses') == 'git-checkout')['with']['expected-commit']
LOCK = {'recipe': 'openjdk-27.yaml', 'version': '27.0.0', 'epoch': 2, 'revision': 1, 'source_commit': COMMIT}


class Derivation(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.root = Path(self.directory.name)
        shutil.copytree(ROOT.parent / 'lang/openjdk', self.root / 'packages/openjdk',
                        ignore=shutil.ignore_patterns('__pycache__'))

    def tearDown(self):
        self.directory.cleanup()

    def derived(self, change=None, lock=None):
        """The package, Wolfi's recipe after change(recipe), and its derivation."""
        upstream = copy.deepcopy(FIXTURE)
        if change:
            change(upstream)
        inputs = self.root / 'build/openjdk/inputs'
        inputs.mkdir(parents=True, exist_ok=True)
        (inputs / 'upstream.yaml').write_text(yaml.safe_dump(upstream))
        (inputs / 'upstream.lock.json').write_text(json.dumps(dict(LOCK, **(lock or {}))))
        pkg = packages.Package('openjdk', root=self.root)
        return pkg, copy.deepcopy(upstream), pkg.hooks.derive(pkg, upstream, dict(LOCK, **(lock or {})))

    def test_wolfi_names_and_a_version_above_every_wolfi_rebuild(self):
        pkg, _, derived = self.derived()
        self.assertEqual('openjdk-27', derived['package']['name'])
        self.assertEqual(1002, derived['package']['epoch'])
        self.assertEqual('27.0.0-r1002', pkg.hooks.full_version(LOCK))
        self.assertEqual(['${{package.name}}-jre', '${{package.name}}-default-jvm', '${{package.name}}-default-jdk'],
                         [s['name'] for s in derived['subpackages']])
        self.assertEqual({'openjdk-27', 'openjdk-27-jre', 'openjdk-27-default-jvm', 'openjdk-27-default-jdk'},
                         publish.package_names(derived))
        for section in [derived['package']] + derived['subpackages']:
            self.assertIn('upstream-origin:openjdk-27:27.0.0-r2', section['dependencies']['provides'])

    def test_the_checkout_becomes_our_fetch_where_it_was_then_our_patches_follow_wolfis(self):
        pkg, _, derived = self.derived()
        steps = derived['pipeline']
        self.assertIn('runs', steps[0])  # Wolfi's version check stays first
        self.assertEqual('Fetch the locked source commit', steps[1]['name'])
        self.assertIn(COMMIT, steps[1]['runs'])
        self.assertIn('https://github.com/openjdk/jdk27u.git https://github.com/openjdk/jdk.git', steps[1]['runs'])
        self.assertEqual('patch', steps[2]['uses'])
        self.assertEqual(patching.step(pkg.hooks.PATCHES), steps[3])
        self.assertFalse([s for s in steps if s.get('uses') == 'git-checkout'])

    def test_the_probe_runs_in_the_package_test_and_wolfis_tests_stay(self):
        _, upstream, derived = self.derived()
        self.assertIn('sh "$work/probe.sh" "$work"', derived['test']['pipeline'][-1]['runs'])
        self.assertEqual(upstream['test']['pipeline'], derived['test']['pipeline'][:-1])
        self.assertEqual({'enabled': False}, derived['update'])

    def test_verify_accepts_the_derivation_of_wolfis_recipe(self):
        pkg, upstream, derived = self.derived()
        verify.check(pkg, upstream, derived)

    def test_upstream_fontconfig_dependency_is_kept_once_in_jdk_and_jre(self):
        pkg = packages.Package('openjdk', root=self.root)
        upstream = yaml.safe_load((Path(__file__).parent / 'fixtures/openjdk-27.0.1.yaml').read_text())
        lock = dict(LOCK, version=str(upstream['package']['version']),
                    source_commit=pkg.hooks.source_commit(upstream))
        derived = pkg.hooks.derive(pkg, copy.deepcopy(upstream), lock)
        verify.check(pkg, upstream, derived)
        for name in ('jdk', 'jre'):
            with self.subTest(package=name):
                relaxed = copy.deepcopy(derived)
                section = relaxed['package'] if name == 'jdk' else relaxed['subpackages'][0]
                self.assertEqual(1, section['dependencies']['runtime'].count('libfontconfig1'))
                section['dependencies']['runtime'].remove('libfontconfig1')
                with self.assertRaises(AssertionError):
                    verify.check(pkg, upstream, relaxed)

    def test_omitted_packages_do_not_build_jmods_or_fetch_a_container_entrypoint(self):
        _, _, derived = self.derived()
        self.assertIn('make jdk-image legacy-jre-image\n', [s.get('runs') for s in derived['pipeline']])
        self.assertNotIn('cacert-entrypoint-hash', derived['vars'])
        self.assertFalse(any(s.get('uses') == 'fetch' for s in derived['pipeline']))

    def test_demo_cleanup_preserves_jdk_and_jre_files(self):
        pkg, _, derived = self.derived()
        root = self.root / 'installed files'
        home = 'usr/lib/jvm/java-27-openjdk/'
        kept = [home + 'bin/java', home + 'bin/javac', home + 'lib/modules', home + 'include/jni.h']
        for name in kept + [home + 'demo/jfc/SwingSet2/SwingSet2.jar']:
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.touch()
        self.assertEqual(pkg.hooks.PRUNE, derived['pipeline'][-1]['name'])
        script = derived['pipeline'][-1]['runs'].replace('${{targets.destdir}}', str(root)).replace(
            '${{vars.java-version}}', '27')
        subprocess.run(['sh', '-eu', '-c', script], check=True)
        self.assertEqual(set(kept), {str(p.relative_to(root)) for p in root.rglob('*') if p.is_file()})

    def test_a_future_configure_option_survives(self):
        def add(recipe):
            step = next(s for s in recipe['pipeline'] if s.get('uses') == 'autoconf/configure')
            step['with']['opts'] += '--enable-future-protection\n'
        pkg, upstream, derived = self.derived(add)
        step = next(s for s in derived['pipeline'] if s.get('uses') == 'autoconf/configure')
        self.assertIn('--enable-future-protection', step['with']['opts'])
        verify.check(pkg, upstream, derived)

    def test_changed_recipes_stop_the_derivation_for_review(self):
        def checkout(recipe):
            return next(s for s in recipe['pipeline'] if s.get('uses') == 'git-checkout')

        def repository(recipe):
            checkout(recipe)['with']['repository'] = 'https://github.com/openjdk/jdk'

        def option(recipe):
            checkout(recipe)['with']['recurse-submodules'] = 'true'

        def second(recipe):
            recipe['pipeline'].append(copy.deepcopy(checkout(recipe)))

        def order(recipe):
            patch = next(s for s in recipe['pipeline'] if s.get('uses') == 'patch')
            recipe['pipeline'].remove(patch)
            recipe['pipeline'].insert(0, patch)

        def environment(recipe):
            recipe['test']['environment']['contents']['packages'] = ['busybox']

        def name(recipe):
            recipe['package']['name'] = 'openjdk-28'

        def subpackage(recipe):
            recipe['subpackages'].append({'name': '${{package.name}}-new'})

        def build(recipe):
            next(s for s in recipe['pipeline'] if s.get('runs', '').strip() ==
                 'make jdk-image legacy-jre-image jmods')['runs'] += 'make new-image\n'

        for change in [repository, option, second, order, environment, name, subpackage, build]:
            with self.subTest(change.__name__), self.assertRaisesRegex(ValueError, 'Review'):
                self.derived(change)

    def test_a_source_pin_other_than_the_lock_is_refused(self):
        with self.assertRaises(AssertionError):
            self.derived(lock={'source_commit': '0' * 40})

    def test_verify_refuses_a_relaxed_derivation(self):
        pkg, upstream, derived = self.derived()
        def configure(d):
            step = next(s for s in d['pipeline'] if s.get('uses') == 'autoconf/configure')
            step['with']['opts'] = step['with']['opts'].replace('--disable-precompiled-headers', '')
        for relax in [
            lambda d: d['pipeline'].remove(next(s for s in d['pipeline'] if s.get('uses') == 'patch')),
            lambda d: d['environment']['contents']['packages'].remove('build-base'),
            lambda d: d['test']['pipeline'].pop(0),
            lambda d: d['subpackages'].pop(),
            lambda d: d.update(update={'enabled': True}),
            lambda d: d['pipeline'].pop(),
            lambda d: d['pipeline'][-1].update(runs='true\n'),
            lambda d: d['subpackages'].append(copy.deepcopy(next(s for s in upstream['subpackages']
                if s['name'] == '${{package.name}}-demos'))),
            lambda d: next(s for s in d['pipeline'] if s.get('runs', '').strip() ==
                'make jdk-image legacy-jre-image').update(runs='make jdk-image legacy-jre-image jmods\n'),
            configure,
        ]:
            relaxed = copy.deepcopy(derived)
            relax(relaxed)
            with self.subTest(relax=relax), self.assertRaises(AssertionError):
                verify.check(pkg, upstream, relaxed)

    def test_sources_follow_the_release_then_mainline(self):
        pkg, _, _ = self.derived()
        self.assertEqual(('https://github.com/openjdk/jdk25u.git', 'https://github.com/openjdk/jdk.git'),
                         pkg.sources(dict(LOCK, version='25.0.4.1')))
        with self.assertRaises(ValueError):
            pkg.hooks.sources(dict(LOCK, version='x.0.0'))

    def test_epoch_moves_with_wolfi_and_with_the_policy_never_colliding(self):
        pkg, _, _ = self.derived()
        epoch = pkg.hooks.epoch
        self.assertEqual(50 * 19, len({epoch(e, r) for e in range(50) for r in range(1, 20)}))
        with self.assertRaises(ValueError):
            epoch(1000, 1)

    def test_the_probe_cannot_end_its_heredoc_early_or_use_melange_variables(self):
        pkg, _, _ = self.derived()
        for text in ['${{package.name}}', pkg.hooks.PROBE_JAVA, pkg.hooks.PROBE_SH]:
            (self.root / 'packages/openjdk/tests/probe.sh').write_text('echo ' + text + '\n')
            with self.subTest(text=text), self.assertRaises(ValueError):
                pkg.hooks.probe(pkg)

    def test_every_allowance_the_patches_read_is_one_werewolf_defines(self):
        read = set()
        for patch in (ROOT.parent / 'lang/openjdk/patches').glob('*.patch'):
            read.update(__import__('re').findall(r'WerewolfAllowance\.allowed\("([a-z-]+)"\)', patch.read_text()))
        self.assertEqual({'deserialize', 'native'}, read)  # unsafe + native-access merged into native


if __name__ == '__main__':
    unittest.main()
