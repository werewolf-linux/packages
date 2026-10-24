"""glibc: Wolfi's newest glibc, with fixed policy patches (README.md).

The hooks scripts/packages.py names. The derivation renames the packages
glibc-neutered-VERSION, which provide Wolfi's names, so an image can require
this policy whatever glibc version it holds."""
import copy
import datetime
import json
import os
import re
import shlex
import shutil
from derive import fetch
import packages
import patching

TITLE = 'glibc'
FAMILY = 'glibc-'
SOURCES = ('https://github.com/kraj/glibc.git', 'https://sourceware.org/git/glibc.git')
PATCHES = 'patches'  # in the build context
notes = packages.sibling(__file__, 'notes')
REMOVED = {'nscd', 'nss-db', 'nss-hesiod', 'sotruss', 'pcprofiledump', 'glibc-tracing'}
HARDENING = ['CFLAGS += -fstrict-flex-arrays=3', 'LDFLAGS.so += -Wl,-z,separate-code',
             'LDFLAGS-rtld += -Wl,-z,separate-code']
MIN_KERNEL = '6.18'
# Wolfi's main-package test requires /usr/lib/locale to hold C.utf8 alone, so no
# test environment holds every locale subpackage; each locale's own test has it.
TEST_UNPINNED = ('glibc-neutered-*-locale-*', 'glibc-neutered-*-locales')

def version(text):
    return tuple(int(x) for x in text.split('.'))
APK_TEST_ADAPTER = '''mv /usr/bin/apk /usr/bin/apk.real
for app in info add del version update; do ln -s apk.real "/usr/bin/apk_$app"; done
cat > /usr/bin/apk <<'NEUTERED_APK'
#!/bin/sh
case "$1" in
  info|add|del|version|update) app=$1; shift; exec "/usr/bin/apk_$app" "$@" ;;
  *) exec /usr/bin/apk.real "$@" ;;
esac
NEUTERED_APK
chmod 755 /usr/bin/apk
test "$(apk version -t 0-r0 1-r0)" = '<'
if test -n '${{vars.neutered-test-repository}}'; then
  printf '%s\\n' '${{vars.neutered-test-repository}}' 'https://packages.wolfi.dev/os' > /etc/apk/repositories
  apk update
fi
'''

def derive(pkg, config, lock):
    policy = pkg.policy
    assert source_commit(config) == lock['source_commit']
    # Retain the source pin in package metadata after replacing the checkout.
    config['vars']['glibc-expected-commit'] = lock['source_commit']
    original = config['package']['name']
    config['package']['name'] = original.replace('glibc-', 'glibc-neutered-', 1)
    wolfi = f"{config['package']['version']}-r{config['package']['epoch']}"
    config['package']['epoch'] += lock['revision']
    config['package']['description'] += ' (Werewolf fixed hardening policy)'
    deps = config['package']['dependencies']
    # glibc-neutered names whichever stream is newest, so a consumer can
    # require this policy without naming a glibc version.
    deps['provides'] += [original + '=${{package.full-version}}', 'glibc-neutered=${{package.full-version}}']
    deps['replaces'] = [original]
    deps['provider-priority'] = '${{vars.neutered-priority}}'
    # Above every Wolfi stream (2.44 -> 244), and a newer stream above an
    # older one (2.44 -> 10244), as Wolfi's own provider-priority orders them.
    config['var-transforms'].append({'from': '${{package.version}}', 'match': '([0-9])[.]([0-9]+)',
                                     'replace': '10$1$2', 'to': 'neutered-priority'})
    config['vars']['neutered-test-repository'] = ''
    old_loader = 'ld-linux-${{package.version}}'
    loader = 'ld-linux-neutered-${{package.version}}'
    deps['runtime'] = [x.replace(old_loader, loader) for x in deps['runtime']]
    deps['runtime'].append('${{package.name}}-nss-config=${{package.full-version}}')
    pipeline = config['pipeline']
    assert pipeline[0]['uses'] == 'git-checkout'
    assert pipeline[1]['uses'] == 'patch'
    checkout = pipeline[0]['with']
    if (set(checkout) != {'expected-commit', 'repository', 'branch'} or
            checkout['expected-commit'] not in {
                lock['source_commit'], '${{vars.glibc-expected-commit}}'} or
            checkout['repository'] not in {
                'https://sourceware.org/git/glibc.git',
                'https://gitlab.com/gnutools/glibc.git'} or
            checkout['branch'] != 'release/${{package.version}}/master'):
        raise ValueError('Review changed upstream source checkout before deriving')
    pipeline[0] = fetch(lock['source_commit'], SOURCES)
    pipeline.insert(2, patching.step(PATCHES))
    configure = next(x for x in pipeline if x.get('name') == 'Configure glibc')
    assert configure['runs'].count('--disable-werror') == 1
    continuation = ' ' + chr(92) + '\n        '
    configure['runs'] = configure['runs'].replace('--disable-werror', continuation.join([
        '--disable-build-nscd', '--disable-nscd', '--enable-hardcoded-path-in-tests', '--disable-werror']))
    # Test executables carry the build interpreter/RPATH. Build utilities run
    # with the build host's installed libc, never a permissive policy build.
    # Makeconfig assigns rtld-prefix after reading configparms. An override
    # also survives Wolfi's second pass rebuilding configparms from its base.
    configure['runs'] += '\n' + "echo 'override rtld-prefix =' >> configparms.base\n" + \
        "echo 'override rtld-prefix =' >> configparms\n"
    configure['runs'] += "echo 'include $(common-objpfx)../tests/harness.mk' >> configparms.base\n" + \
        "echo 'include $(common-objpfx)../tests/harness.mk' >> configparms\n"
    # Beyond Wolfi at no run-time cost (docs/design/compiler-hardening.md):
    # fortify bounds trailing arrays by their declared size, and no library or
    # loader maps its headers and read-only data executable, as x86 already does.
    for line in HARDENING:
        configure['runs'] += f"echo '{line}' >> configparms.base\necho '{line}' >> configparms\n"
    # Werewolf boots Linux 6.18 or later: glibc drops fallbacks for older kernels.
    kernel = re.findall(r'--enable-kernel=([0-9.]+)', configure['runs'])
    assert len(kernel) == 1, 'review changed upstream minimum kernel'
    if version(kernel[0]) < version(MIN_KERNEL):
        configure['runs'] = configure['runs'].replace('--enable-kernel='+kernel[0], '--enable-kernel='+MIN_KERNEL)
    test = next(x for x in pipeline if x.get('name') == 'Run the glibc testsuite')
    assert 'make -C build' in test['runs']
    # Retain Wolfi's full suite and its gates; hardcoded test paths avoid a
    # --library-path exception. Runtime programs never gain such an exception.
    index = pipeline.index(test)
    pipeline.insert(index, {'name': 'Install explicit locale test fixtures', 'runs':
        'python3 tests/prepare-locales.py --build build\n'})
    pipeline.insert(index, {'name': 'Configure built-in NSS providers for validation', 'runs':
        'cp profile/nsswitch.conf /etc/nsswitch.conf\n'})
    index = pipeline.index(test)
    pipeline.insert(index, {'name': 'Verify fixed hardening policy', 'runs':
        'case "${{build.arch}}" in\n'
        '  aarch64) baseline=/lib/ld-linux-aarch64.so.1 ;;\n'
        '  x86_64) baseline=/lib64/ld-linux-x86-64.so.2 ;;\n'
        '  *) exit 1 ;;\n'
        'esac\n'
        'python3 tests/run.py --build build --cc gcc --baseline-loader "$baseline" --report build/policy-report.json\n'})
    pipeline.insert(index, {'uses': 'test/compiler/hardening-check', 'with': {'cc': 'gcc'}})
    index = next(i for i, x in enumerate(pipeline) if x.get('uses') == 'strip')
    removal = {'name': 'Remove unsupported runtime facilities', 'runs':
        'rm -rf "${{targets.destdir}}/usr/lib/gconv"\n'
        'rm -f "${{targets.destdir}}/usr/bin/iconvconfig" '
        '"${{targets.destdir}}/usr/bin/sotruss" '
        '"${{targets.destdir}}/usr/bin/pcprofiledump"\n'
        'rm -f "${{targets.destdir}}"/usr/lib/libnss_db.so* "${{targets.destdir}}"/usr/lib/libnss_hesiod.so* '
        '"${{targets.destdir}}"/usr/lib/libpcprofile.so* "${{targets.destdir}}"/usr/lib/libmemusage.so* "${{targets.destdir}}"/usr/lib/libsotruss.so* "${{targets.destdir}}"/usr/lib/libc_malloc_debug.so*\n'}
    pipeline.insert(index, removal)
    kept = []
    for sub in config['subpackages']:
        name = sub['name'].replace('${{package.name}}', 'glibc')
        base = name.split('-${{package.version}}')[0]
        if base in REMOVED:
            # Its files would stay in the main package: xtrace alone needs bash.
            for step in sub['pipeline']:
                for line in step['runs'].splitlines():
                    words = shlex.split(line)
                    if not words or words[0] == 'mkdir':
                        continue
                    if words[0] != 'mv':
                        raise ValueError('Review changed upstream ' + base + ' packaging before deriving')
                    for source in words[1:-1]:
                        path = source.removeprefix('${{targets.destdir}}/')
                        if path == source or '..' in path.split('/'):
                            raise ValueError('Review changed upstream ' + base + ' packaging before deriving')
                        removal['runs'] += 'rm -rf "${{targets.destdir}}"/' + path + '\n'
            continue
        if '${{package.name}}' in sub['name']:
            original_name = sub['name'].replace('${{package.name}}', original)
            subdeps = sub.setdefault('dependencies', {})
            subdeps.setdefault('provides', []).append(original_name+'=${{package.full-version}}')
            subdeps.setdefault('replaces', []).append(original_name)
            subdeps['provider-priority'] = '${{vars.neutered-priority}}'
            subdeps['runtime'] = [
                '${{package.name}}=${{package.full-version}}' if x.startswith('glibc>=') else x
                for x in subdeps.get('runtime', [])]
        if sub['name'] == old_loader:
            sub['name'] = loader
            sub['dependencies']['provides'].append(old_loader + '=${{package.full-version}}')
            sub['dependencies']['replaces'] = [old_loader]
            sub['dependencies']['provider-priority'] = '${{vars.neutered-priority}}'
        if sub['name'] == '${{package.name}}-iconv':
            sub['description'] = 'Built-in iconv command; external converters disabled'
            sub['pipeline'] = [{'runs': 'mkdir -p "${{targets.subpkgdir}}/usr/bin"\n'
                'mv "${{targets.destdir}}/usr/bin/iconv" "${{targets.subpkgdir}}/usr/bin/"\n'}]
            # iconvconfig is intentionally absent; keep CLI and refusal checks.
            sub['test']['pipeline'] = [copy.deepcopy(x) for x in sub['test']['pipeline']]
            for x in sub['test']['pipeline']:
                if x.get('with', {}).get('bins'):
                    x['with']['bins'] = 'iconv\n'
        if sub['name'] == '${{package.name}}-testresults':
            sub['pipeline'].append({'runs': 'cp build/policy-report.json "${{targets.subpkgdir}}/usr/share/glibc-testresults/policy-report.json"\n'})
            gate = next(x for x in sub['test']['pipeline'] if x.get('name') == 'Verify PASS/FAIL ratio in test result files is within acceptable threshold')
            assert gate['runs'].count('MAX_FAIL_RATIO=0.02') == 1
            gate['runs'] = gate['runs'].replace('MAX_FAIL_RATIO=0.02',
                'MAX_FAIL_RATIO=' + format(policy['max_fail_per_mille'] / 1000, '.3f'))
        if sub['name'] == 'posix-libc-utils-${{package.version}}':
            sub['test']['environment'] = {'contents': {'packages': ['build-base', '${{package.name}}-dev']}}
            sub['test']['pipeline'].append({'name': 'ldd lists dependencies without executing target code', 'runs':
                "cat > ldd-probe.c <<'NEUTERED_LDD'\n"
                '#include <fcntl.h>\n#include <unistd.h>\n'
                'static void mark(void) { int fd = open("ldd-executed", O_WRONLY|O_CREAT, 0600); if (fd >= 0) close(fd); }\n'
                '__attribute__((constructor)) static void init(void) { mark(); }\n'
                'int main(void) { mark(); return 0; }\n'
                'NEUTERED_LDD\n'
                'gcc ldd-probe.c -o ldd-probe\n'
                './ldd-probe\n'
                'test -f ldd-executed\nrm ldd-executed\n'
                'ldd ./ldd-probe > ldd-list\n'
                'grep "libc.so.6" ldd-list\ntest ! -e ldd-executed\n'
                'for option in -d -r -u -v; do\n'
                '  if ldd "$option" ./ldd-probe; then exit 1; fi\n'
                '  test ! -e ldd-executed\ndone\n'})
        kept.append(sub)
    config['subpackages'] = kept
    config['subpackages'].append({
        'name': '${{package.name}}-nss-config',
        'description': 'Fixed files/DNS account and host configuration',
        'dependencies': {'replaces': ['wolfi-baselayout'], 'runtime': ['${{package.name}}=${{package.full-version}}']},
        'pipeline': [{'runs': 'install -Dm644 profile/nsswitch.conf "${{targets.subpkgdir}}/etc/nsswitch.conf"\n'}],
        'test': {'pipeline': [{'runs': "cmp /etc/nsswitch.conf - <<'NEUTERED_NSS'\n" +
            (pkg.dir/'profile/nsswitch.conf').read_text()+'NEUTERED_NSS\n'}]}})
    # Each package names the Wolfi source and version it rebuilds, unversioned
    # so that all may: Werewolf matches Wolfi's CVE fixes against it, not ours.
    for section in [config['package']] + config['subpackages']:
        section.setdefault('dependencies', {}).setdefault('provides', []).append(
            'wolfi-origin:' + original + ':' + wolfi)
    # Keep the original ratio and assertions; find must use explicit commands.
    for section in [config] + config['subpackages']:
        tests = section.get('test', {}).get('pipeline', [])
        if tests:
            tests.insert(0, {'name': 'Adapt apk test tooling to strict option ordering', 'runs': APK_TEST_ADAPTER})
        for step in tests:
            if 'runs' in step:
                step['runs'] = step['runs'].replace('-exec grep ', '-exec /usr/bin/grep ')
    # Preserve the solver test for every remaining virtual package.
    for x in config['test']['pipeline']:
        if x.get('name') == 'Verify version-streamed virtuals do not resolve to an older package':
            lines = x['runs'].splitlines()
            x['runs'] = '\n'.join(s for s in lines if s.strip() not in REMOVED) + '\n'
    config['update'] = {'enabled': False}
    return config


# --- hooks beside the derivation (scripts/packages.py) ---------------------------

def stable(config):
    """Wolfi's primary glibc recipe: the one providing glibc itself."""
    provides = config.get('package', {}).get('dependencies', {}).get('provides', [])
    return 'glibc=${{package.full-version}}' in provides

def source_commit(config):
    pipeline = config.get('pipeline', [])
    if not pipeline or pipeline[0].get('uses') != 'git-checkout':
        return None
    pin = pipeline[0].get('with', {}).get('expected-commit')
    if pin == '${{vars.glibc-expected-commit}}':
        return config.get('vars', {}).get('glibc-expected-commit')
    return pin

def check_policy(policy):
    if type(policy['max_fail_per_mille']) is not int or not 0 <= policy['max_fail_per_mille'] <= 55:
        raise ValueError('Raw failure ceiling must not exceed 5.5%')

def full_version(lock):
    return lock['version'] + '-r' + str(lock['epoch'] + lock['revision'])

def context(pkg, destination):
    for path in ['patches', 'tests', 'profile']:
        shutil.copytree(pkg.dir / path, destination / path, dirs_exist_ok=True)

def prepared(pkg, destination):
    """The upstream test failures Wolfi allows, beside the prepared source."""
    shutil.copyfile(pkg.inputs / 'upstream/allowed-failures.txt', destination / 'allowed-failures.txt')

def check(pkg, upstream, derived):
    """Raise AssertionError unless derived keeps the hardening Wolfi builds with."""
    configure = lambda c: next(x['runs'] for x in c['pipeline'] if x.get('name') == 'Configure glibc')
    flags = lambda s: set(re.findall(r'--[a-zA-Z0-9_-]+(?:=[^\s\\]+)?', s))
    kept = {f for f in flags(configure(upstream)) if not f.startswith('--enable-kernel=')}
    assert kept <= flags(configure(derived)), 'upstream configure flags removed'
    # The minimum kernel may only rise: it removes fallbacks, never protections.
    kernels = lambda s: [version(v) for v in re.findall(r'--enable-kernel=([0-9.]+)', s)]
    assert len(kernels(configure(derived))) == 1, 'one minimum kernel'
    assert kernels(configure(derived)) >= kernels(configure(upstream)), 'minimum kernel lowered'
    assert derived['environment'] == upstream['environment'], 'upstream compiler environment changed'
    environment = derived['environment']['contents']['packages']
    assert 'openssf-compiler-options' in environment and 'build-base' in environment
    for flag in ['--enable-bind-now', '--enable-fortify-source', '--enable-stackguard-randomization',
                 '--enable-stack-protector=strong', '--enable-cet']:
        assert flag in flags(configure(derived)), flag
    assert derived['environment']['environment']['CPPFLAGS'] == ''
    assert 'CFLAGS' not in derived['environment'].get('environment', {})
    assert 'LDFLAGS' not in derived['environment'].get('environment', {})
    original_patch = next(x for x in upstream['pipeline'] if x.get('uses') == 'patch')
    assert original_patch == next(x for x in derived['pipeline'] if x.get('uses') == 'patch')
    for name in ['Run the glibc testsuite', 'Generate public ABI report', 'Check libc public ABI against the previous report']:
        assert next(x for x in upstream['pipeline'] if x.get('name') == name) == next(x for x in derived['pipeline'] if x.get('name') == name), name
    policy = pkg.policy
    assert 0 <= policy['max_fail_per_mille'] <= 55
    gate_name = 'Verify PASS/FAIL ratio in test result files is within acceptable threshold'
    def gate(config):
        sub = next(x for x in config['subpackages'] if x['name'] == '${{package.name}}-testresults')
        return next(x['runs'] for x in sub['test']['pipeline'] if x.get('name') == gate_name)
    ceiling = format(policy['max_fail_per_mille'] / 1000, '.3f')
    assert gate(derived).replace('MAX_FAIL_RATIO='+ceiling, 'MAX_FAIL_RATIO=0.02').replace(
        '-exec /usr/bin/grep ', '-exec grep ') == gate(upstream), 'raw test counting/gate changed'

def test_recipe(config, repository):
    """The package test recipe: fresh-root apk checks install from repository."""
    config['vars']['neutered-test-repository'] = repository
    return config

def raw_gate(files, max_fail_per_mille):
    results = [data.splitlines()[0] for name, data in files.items()
               if name.endswith('.test-result') and data.splitlines()]
    passed = sum(line.startswith(b'PASS:') for line in results)
    failed = sum(line.startswith(b'FAIL:') for line in results)
    if passed + failed == 0 or failed * 1000 > (passed + failed) * max_fail_per_mille:
        raise ValueError(f'Raw failure gate failed: PASS={passed}, FAIL={failed}')
    if not any(name.endswith('/policy-report.json') for name in files):
        raise ValueError('Focused hardening evidence is missing')

def check_apk(pkg, name, parts):
    """The test results package holds results within the raw failure ceiling."""
    import publish
    if name.endswith('-testresults'):
        raw_gate(publish.contents(parts[2][1]), pkg.policy['max_fail_per_mille'])

def check_apks(pkg, names):
    if sum(name.endswith('-testresults') for name in names) != 1:
        raise ValueError('Missing or duplicate upstream test results')

# --- release notes -------------------------------------------------------------------

def stream(version):
    # glibc-neutered's package stem for a release version: 2.44-r19 is glibc-neutered-2.44.
    return 'glibc-neutered-' + version.split('-r')[0]

def glibc_commit(data):
    # The glibc commit a test results package was built from, in its recipe.
    import publish, yaml
    recipe = publish.contents(publish.members(data)[1][1])['.melange.yaml']
    return yaml.safe_load(recipe)['vars']['glibc-expected-commit']

def results(data):
    import publish
    files = publish.apk_files(data)
    report = next(v for k, v in files.items() if k.endswith('/policy-report.json'))
    summary = next(v for k, v in files.items() if k.endswith('glibc-testresults/tests.sum'))
    return notes.statuses(summary.decode(errors='replace')), json.loads(report)

def gather(pkg, artifacts, lock, records):
    # Facts for notes.build: this build's evidence, the previous release's and
    # Wolfi's advisories. Run once per release; stage() keeps what it wrote.
    import publish
    wolfi = lock['version'] + '-r' + str(lock['epoch'])
    version = records[publish.ARCHES[0]]['version']
    previous = None
    pointer = publish.download(pkg.public + 'current.json')
    if pointer is not None:
        pointer = json.loads(pointer)
        if pointer['build_id'] == lock['build_id']:
            raise ValueError('current.json already names this build')
        old = publish.download(pkg.public + 'builds/' + pointer['build_id'] + '/release-notes.json')
        old = json.loads(old) if old is not None else None
        if old is not None:
            origin = old['source']['wolfi_version']
        else:
            # Releases before notes name their Wolfi build in a provides.
            entry = next(s for s in publish.stanzas(publish.download(pkg.public + publish.ARCHES[1] + '/APKINDEX.tar.gz'))
                         if s['P'] == stream(pointer['version']) and s['V'] == pointer['version'])
            origin = next((p.rsplit(':', 1)[1] for p in entry.get('p', '').split()
                           if p.startswith('wolfi-origin:')), None)
        previous = {'version': pointer['version'], 'build_id': pointer['build_id'],
                    'commit': pointer['source_sha'], 'wolfi_version': origin,
                    'wolfi_commit': old['source']['wolfi_commit'] if old is not None else None,
                    'wolfi_inputs': old['wolfi_inputs'] if old is not None else None}
    commits = glibc_commits = None
    architectures = {}
    for arch in publish.ARCHES:
        directory = artifacts / (pkg.name + '-' + arch)
        tests, report = results(next((directory / arch).glob('*-testresults-*.apk')).read_bytes())
        before = None
        if previous is not None:
            old = publish.download(pkg.public + arch + '/' + stream(previous['version']) + '-testresults-' +
                                   previous['version'] + '.apk')
            if old is not None:
                before = results(old)[0]
                previous.setdefault('glibc_commit', glibc_commit(old))
        found = [{'name': s['P'], 'version': s['V'], 'size': int(s['S'])}
                 for s in publish.stanzas((directory / arch / 'APKINDEX.tar.gz').read_bytes())]
        architectures[arch] = {'tests': tests, 'previous_tests': before, 'report': report,
                               'packages': sorted(found, key=lambda p: p['name'])}
    if previous is not None:
        found = publish.lookup('repos/' + os.environ['GITHUB_REPOSITORY'] + '/compare/' + previous['commit'] +
                               '...' + os.environ['GITHUB_SHA'])
        commits = publish.subjects(found['commits']) if found else None
        # Upstream glibc: the release-branch commits Wolfi's new pin brings, from
        # the mirror the build fetches first.
        if previous.get('glibc_commit') == lock['source_commit']:
            glibc_commits = []
        elif previous.get('glibc_commit'):
            found = publish.lookup('repos/kraj/glibc/compare/' + previous['glibc_commit'] + '...' + lock['source_commit'])
            glibc_commits = publish.subjects(found['commits']) if found else None
    secdb = json.loads(publish.download(notes.SECDB))
    return {'version': version, 'build_id': lock['build_id'],
            'date': datetime.datetime.now(datetime.timezone.utc).date().isoformat(),
            'commit': os.environ['GITHUB_SHA'],
            'run': os.environ.get('GITHUB_SERVER_URL', 'https://github.com') + '/' +
                   os.environ['GITHUB_REPOSITORY'] + '/actions/runs/' + os.environ['GITHUB_RUN_ID'],
            'source': {'glibc_commit': lock['source_commit'], 'wolfi_commit': lock['wolfi_commit'],
                       'wolfi_version': wolfi, 'recipe': lock['recipe'], 'policy_revision': lock['revision']},
            'repository': {'stable': pkg.public, 'snapshot': pkg.public + 'builds/' + lock['build_id'],
                           'key_sha256': publish.sha256(pkg.root / 'release/packages.pub')},
            'patches': (pkg.dir / 'patches/series').read_text().split(), 'wolfi_inputs': lock['sha256'],
            'previous': previous, 'commits': commits, 'glibc_commits': glibc_commits,
            'ceiling': pkg.policy['max_fail_per_mille'],
            'security': notes.security(secdb, 'glibc-' + lock['version'],
                                       previous['wolfi_version'] if previous else None, wolfi),
            'architectures': architectures}

