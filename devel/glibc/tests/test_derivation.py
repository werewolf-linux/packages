import copy
import json
from pathlib import Path
import re
import shutil
import sys
import tempfile
import unittest
import yaml

ROOT = Path(__file__).resolve().parents[3] / 'wolfi'
sys.path.insert(0, str(ROOT / 'scripts'))
import derive
import packages
import patching
import verify

FIXTURE = yaml.safe_load((ROOT.parent / 'devel/glibc/tests/fixtures/glibc-2.44.yaml').read_text())
COMMIT = FIXTURE['pipeline'][0]['with']['expected-commit']


class Derivation(unittest.TestCase):
    def derived(self, change=None, lock=None):
        """The derivation of a copy of Wolfi's recipe, after change(recipe)."""
        upstream = copy.deepcopy(FIXTURE)
        if change:
            change(upstream)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copytree(ROOT.parent / 'devel/glibc', root / 'packages/glibc',
                            ignore=shutil.ignore_patterns('docs', 'tools', '__pycache__'))
            pkg = packages.Package('glibc', root=root)
            pkg.inputs.mkdir(parents=True)
            (pkg.inputs / 'upstream.yaml').write_text(yaml.safe_dump(upstream))
            base = {'recipe': 'glibc-2.44.yaml', 'version': '2.44', 'epoch': FIXTURE['package']['epoch'],
                    'revision': pkg.policy['revision'], 'source_commit': COMMIT}
            (pkg.inputs / 'upstream.lock.json').write_text(json.dumps(dict(base, **(lock or {}))))
            return pkg, upstream, derive.derive(pkg)

    def configure(self, result):
        return next(x['runs'] for x in result['pipeline'] if x.get('name') == 'Configure glibc')

    def test_the_build_fetches_the_locked_commit_from_glibcs_mirrors(self):
        fetch = self.derived()[2]['pipeline'][0]
        self.assertEqual('Fetch the locked source commit', fetch['name'])
        self.assertIn('git', fetch['needs']['packages'])
        self.assertIn('for source in https://github.com/kraj/glibc.git https://sourceware.org/git/glibc.git;', fetch['runs'])
        self.assertIn('test "$(git rev-parse FETCH_HEAD)" = ' + COMMIT, fetch['runs'])

    def test_patches_apply_after_wolfis_by_the_shared_step(self):
        steps = self.derived()[2]['pipeline']
        self.assertEqual('patch', steps[1]['uses'])
        self.assertEqual(patching.step('patches'), steps[2])

    def test_verify_accepts_the_derivation(self):
        _, upstream, result = self.derived()
        verify.check(packages.Package('glibc'), upstream, result)

    def test_future_upstream_flag_survives(self):
        def add(upstream):
            step = next(x for x in upstream['pipeline'] if x.get('name') == 'Configure glibc')
            step['runs'] += '\n# Added by a future Wolfi update: --enable-future-protection\n'
        self.assertIn('--enable-future-protection', self.configure(self.derived(add)[2]))

    def test_source_pin_mismatch_is_rejected(self):
        with self.assertRaises(AssertionError):
            self.derived(lock={'source_commit': '0' * 40})

    def test_literal_checkout_pin_is_retained_in_package_metadata(self):
        result = self.derived()[2]
        self.assertEqual(COMMIT, result['vars']['glibc-expected-commit'])

    def test_legacy_variable_pin_and_sourceware_checkout_remain_supported(self):
        def change(upstream):
            upstream['vars']['glibc-expected-commit'] = COMMIT
            checkout = upstream['pipeline'][0]['with']
            checkout['expected-commit'] = '${{vars.glibc-expected-commit}}'
            checkout['repository'] = 'https://sourceware.org/git/glibc.git'
        _, upstream, result = self.derived(change)
        verify.check(packages.Package('glibc'), upstream, result)
        self.assertEqual(COMMIT, result['vars']['glibc-expected-commit'])

    def test_unreviewed_checkout_repository_is_rejected(self):
        def change(upstream):
            upstream['pipeline'][0]['with']['repository'] = 'https://example.com/glibc.git'
        with self.assertRaisesRegex(ValueError, 'Review changed'):
            self.derived(change)

    def test_changed_checkout_branch_is_rejected(self):
        def change(upstream):
            upstream['pipeline'][0]['with']['branch'] = 'master'
        with self.assertRaisesRegex(ValueError, 'Review changed'):
            self.derived(change)

    def test_unresolved_pin_cannot_use_a_stale_metadata_variable(self):
        def change(upstream):
            upstream['vars']['glibc-expected-commit'] = COMMIT
            upstream['pipeline'][0]['with']['expected-commit'] = '${{vars.unknown}}'
        with self.assertRaises(AssertionError):
            self.derived(change)

    def test_changed_upstream_source_checkout_requires_review(self):
        def change(upstream):
            upstream['pipeline'][0]['with']['recurse-submodules'] = 'true'
        with self.assertRaisesRegex(ValueError, 'Review changed'):
            self.derived(change)

    def test_removed_facilities_cannot_resolve_back_to_wolfi(self):
        result = self.derived()[2]
        names = [x['name'] for x in result['subpackages']]
        for forbidden in ['nscd-', 'nss-db-', 'nss-hesiod-', 'pcprofiledump-', 'sotruss-']:
            self.assertFalse(any(x.startswith(forbidden) for x in names), forbidden)
        solver = next(x['runs'] for x in result['test']['pipeline']
                      if x.get('name') == 'Verify version-streamed virtuals do not resolve to an older package')
        for forbidden in packages.Package('glibc').hooks.REMOVED:
            self.assertNotIn(forbidden, [line.strip() for line in solver.splitlines()])

    def test_development_and_loader_packages_require_matching_policy_libc(self):
        result = self.derived()[2]
        dev = next(x for x in result['subpackages'] if x['name'] == '${{package.name}}-dev')
        deps = dev['dependencies']
        self.assertIn('${{package.name}}=${{package.full-version}}', deps['runtime'])
        self.assertIn('glibc-' + str(result['package']['version']) + '-dev=${{package.full-version}}', deps['provides'])
        self.assertIn('glibc-' + str(result['package']['version']) + '-dev', deps['replaces'])
        loader = next(x for x in result['subpackages'] if x['name'].startswith('ld-linux-neutered-'))
        self.assertIn('${{package.name}}=${{package.full-version}}', loader['dependencies']['runtime'])
        self.assertIn('ld-linux-neutered-${{package.version}}=${{package.full-version}}',
                      result['package']['dependencies']['runtime'])

    def test_builtin_nss_configuration_is_a_required_owned_package(self):
        result = self.derived()[2]
        self.assertIn('${{package.name}}-nss-config=${{package.full-version}}', result['package']['dependencies']['runtime'])
        profile = next(x for x in result['subpackages'] if x['name'] == '${{package.name}}-nss-config')
        self.assertEqual(['wolfi-baselayout'], profile['dependencies']['replaces'])
        self.assertIn('${{package.name}}=${{package.full-version}}', profile['dependencies']['runtime'])
        for line in (ROOT.parent / 'devel/glibc/profile/nsswitch.conf').read_text().splitlines():
            self.assertLessEqual(set(line.split(':', 1)[1].split()), {'files', 'dns'})

    def test_removed_subpackage_files_leave_the_main_package(self):
        removal = next(x['runs'] for x in self.derived()[2]['pipeline']
                       if x.get('name') == 'Remove unsupported runtime facilities')
        # xtrace is a bash script: left behind, every image would need bash.
        for path in ['usr/bin/xtrace', 'usr/bin/mtrace', 'usr/bin/sprof', 'usr/bin/makedb', 'usr/lib/audit/*']:
            self.assertIn('rm -rf "${{targets.destdir}}"/' + path + '\n', removal)

    def test_changed_removed_subpackage_packaging_requires_review(self):
        def change(upstream):
            tracing = next(x for x in upstream['subpackages'] if x['name'] == '${{package.name}}-tracing')
            tracing['pipeline'][0]['runs'] += 'cp "${{targets.destdir}}"/usr/bin/new "${{targets.subpkgdir}}"/usr/bin\n'
        with self.assertRaisesRegex(ValueError, 'Review changed upstream glibc-tracing'):
            self.derived(change)

    def test_hardening_beyond_wolfi_survives_configparms_rebuilds(self):
        configure = self.configure(self.derived()[2])
        for line in packages.Package('glibc').hooks.HARDENING:
            for target in ['configparms.base', 'configparms']:
                self.assertIn(f"echo '{line}' >> {target}\n", configure)
        self.assertEqual(['6.18'], re.findall(r'--enable-kernel=([0-9.]+)', configure))

    def test_a_newer_upstream_minimum_kernel_is_kept(self):
        def change(upstream):
            step = next(x for x in upstream['pipeline'] if x.get('name') == 'Configure glibc')
            step['runs'] = re.sub(r'--enable-kernel=[0-9.]+', '--enable-kernel=7.1', step['runs'])
        self.assertEqual(['7.1'], re.findall(r'--enable-kernel=([0-9.]+)', self.configure(self.derived(change)[2])))

    def test_every_package_names_the_wolfi_source_it_rebuilds(self):
        upstream = FIXTURE['package']
        result = self.derived()[2]
        source = f"wolfi-origin:{upstream['name']}:{upstream['version']}-r{upstream['epoch']}"
        for section in [result['package']] + result['subpackages']:
            # Unversioned: apk installs one provider of each name=version.
            self.assertIn(source, section['dependencies']['provides'])

    def test_unversioned_virtual_prefers_policy_libc_and_newer_streams(self):
        result = self.derived()[2]
        self.assertIn('glibc-neutered=${{package.full-version}}', result['package']['dependencies']['provides'])
        self.assertNotIn('neutered-priority', result['vars'])
        transform = next(x for x in result['var-transforms'] if x['to'] == 'neutered-priority')
        wolfi = next(x for x in result['var-transforms'] if x['to'] == 'provider-priority')
        self.assertEqual((wolfi['from'], wolfi['match']), (transform['from'], transform['match']))
        self.assertEqual('10$1$2', transform['replace'])

    def test_the_published_version_adds_the_policy_revision_to_wolfis_epoch(self):
        hooks = packages.Package('glibc').hooks
        self.assertEqual('2.44-r22', hooks.full_version({'version': '2.44', 'epoch': 8, 'revision': 14}))


if __name__ == '__main__':
    unittest.main()
