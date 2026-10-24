import copy
import json
import os
from pathlib import Path
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
import refresh
import verify

FIXTURE = yaml.safe_load((Path(__file__).parent / 'fixtures/nodejs-26.yaml').read_text())
PACKAGE = packages.Package('node')
COMMIT = PACKAGE.hooks.source_commit(FIXTURE)
LOCK = {'recipe': 'nodejs-26.yaml', 'version': '26.12.1', 'epoch': 0,
        'revision': 1, 'source_commit': COMMIT}


def named(pipeline, name):
    return next(s for s in pipeline if s.get('name') == name)


class NodeDerivation(unittest.TestCase):
    def derived(self, change=lambda c: None):
        upstream = copy.deepcopy(FIXTURE)
        change(upstream)
        return upstream, PACKAGE.hooks.derive(PACKAGE, copy.deepcopy(upstream), LOCK)

    def test_follows_the_latest_wolfi_stream(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for stream in (24, 26, 28):
                config = copy.deepcopy(FIXTURE)
                config['package'].update(name='nodejs-' + str(stream), version=str(stream) + '.1.0')
                (root / ('nodejs-' + str(stream) + '.yaml')).write_text(yaml.safe_dump(config))
            self.assertEqual('nodejs-28.yaml', refresh.latest(PACKAGE, root)[0].name)
            config['package']['version'] = '28.1.0_rc1'
            (root / 'nodejs-28.yaml').write_text(yaml.safe_dump(config))
            self.assertEqual('nodejs-26.yaml', refresh.latest(PACKAGE, root)[0].name)

    def test_signed_feed_identity_and_minimum_runtime(self):
        upstream, result = self.derived()
        verify.check(PACKAGE, upstream, result)
        self.assertEqual({'nodejs-26'}, publish.package_names(result))
        self.assertEqual('26.12.1-r1000', PACKAGE.hooks.full_version(LOCK))
        self.assertIn('upstream-origin:nodejs-26:26.12.1-r0', result['package']['dependencies']['provides'])
        self.assertIn('nodejs-hardened=${{package.full-version}}', result['package']['dependencies']['provides'])
        self.assertEqual([], result['subpackages'])
        build = next(s['runs'] for s in result['pipeline'] if s.get('name') == PACKAGE.hooks.BUILD)
        for flag in ('--without-inspector', '--without-npm', '--without-node-options', '--without-brotli', '--without-zstd', '--without-ffi'):
            self.assertIn(flag, build)

    def test_build_and_test_guests_alone_get_capabilities(self):
        _, result = self.derived()
        allowed = named(result['pipeline'], PACKAGE.hooks.ALLOW)
        self.assertIn('/etc/runtime-hardening/build-root', allowed['runs'])
        for group in PACKAGE.hooks.GROUPS:
            self.assertIn(group, allowed['runs'])
        tested = PACKAGE.hooks.test_recipe(copy.deepcopy(result), 'unused')
        self.assertEqual(allowed, tested['test']['pipeline'][0])
        self.assertIn('effective root', result['test']['pipeline'][-1]['runs'])
        self.assertEqual({'packages': ['build-base', 'openssl']}, result['test']['pipeline'][-1]['needs'])

    def test_many_core_hosts_do_not_start_unbounded_compiler_jobs(self):
        _, result = self.derived()
        script = named(result['pipeline'], PACKAGE.hooks.BUILD)['runs']
        script = script.replace('${{build.arch}}', 'x86_64').replace(
            '${{vars.configure-flags}}', result['vars']['configure-flags']).replace(
            '${{vars.python-version}}', str(result['vars']['python-version']))
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tools = {
                'python' + str(result['vars']['python-version']): '#!/bin/sh\nexit 0\n',
                'icu-config': '#!/bin/sh\necho /fixture/icu\n',
                'make': '#!/bin/sh\nprintf "%s\\n" "$@" > "$NODE_BUILD_ARGS"\n',
                'nproc': '#!/bin/sh\ntouch "$NODE_NPROC_CALLED"\necho 128\n',
            }
            for name, code in tools.items():
                tool = root / name
                tool.write_text(code)
                tool.chmod(0o755)
            capture, nproc = root / 'args', root / 'nproc-called'
            env = {**os.environ, 'PATH': str(root) + os.pathsep + os.environ['PATH'],
                   'NODE_BUILD_ARGS': str(capture), 'NODE_NPROC_CALLED': str(nproc),
                   'CFLAGS': '-Os', 'CXXFLAGS': '-Os', 'CPPFLAGS': '-Os'}
            subprocess.run(['bash', '-eu', '-c', script], cwd=root, env=env, check=True)
            args = capture.read_text().splitlines()
            jobs = int(next(arg[2:] for arg in args if arg.startswith('-j')))
            self.assertGreater(jobs, 0)
            self.assertLessEqual(jobs, 4)
            self.assertIn('JOBS=' + str(jobs), args)
            self.assertFalse(nproc.exists())

    def test_reviewed_wolfi_patches_apply_before_ours(self):
        upstream, result = self.derived()
        uses = [s.get('uses') or s.get('name') for s in result['pipeline'][:4]]
        self.assertEqual([None, 'patch', 'Apply Werewolf policy patches', PACKAGE.hooks.ALLOW],
                         [None] + uses[1:])
        self.assertEqual(upstream['pipeline'][1], result['pipeline'][1])

    def test_changed_build_paths_stop_for_review(self):
        changes = [
            lambda c: c['pipeline'].append({'runs': 'new install'}),
            lambda c: c['pipeline'].insert(1, {'uses': 'patch'}),
            lambda c: c['pipeline'].insert(1, {'uses': 'patch', 'with': {'patches': 'unreviewed.patch'}}),
            lambda c: c['subpackages'].append({'name': '${{package.name}}-new'}),
            lambda c: c['subpackages'].pop(),
            lambda c: c['pipeline'][0]['with'].update(repository='https://example.com/node'),
            lambda c: named(c['pipeline'], PACKAGE.hooks.BUILD).update(name='Build changed'),
            lambda c: named(c['pipeline'], PACKAGE.hooks.BUILD).update(runs='configure differently'),
            lambda c: named(c['pipeline'], PACKAGE.hooks.BUILD).update(
                runs=named(c['pipeline'], PACKAGE.hooks.BUILD)['runs'].replace('-j$(nproc)', '-j128')),
        ]
        for change in changes:
            with self.subTest(change=change), self.assertRaisesRegex(ValueError, 'Review'):
                self.derived(change)

    def test_relaxing_the_derived_build_is_rejected(self):
        upstream, result = self.derived()
        for change in [
            lambda c: named(c['pipeline'], PACKAGE.hooks.BUILD).update(
                runs=named(c['pipeline'], PACKAGE.hooks.BUILD)['runs'].replace('--without-inspector', '')),
            lambda c: named(c['pipeline'], PACKAGE.hooks.ALLOW).update(runs='true'),
            lambda c: c['pipeline'].pop(1),
            lambda c: c['subpackages'].append(upstream['subpackages'][0]),
            lambda c: c['test']['pipeline'].pop(0),
            lambda c: c.update(update={'enabled': True}),
            lambda c: named(c['pipeline'], PACKAGE.hooks.BUILD).update(
                runs=named(c['pipeline'], PACKAGE.hooks.BUILD)['runs'].replace('JOBS=4 -j4', '-j$(nproc)')),
        ]:
            changed = copy.deepcopy(result)
            change(changed)
            with self.subTest(change=change), self.assertRaises(AssertionError):
                verify.check(PACKAGE, upstream, changed)


if __name__ == '__main__':
    unittest.main()
