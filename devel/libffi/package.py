"""Wolfi's libffi, with its callback trampolines in read-only library code."""
from pathlib import Path
from derive import fetch

TITLE = 'libffi'
RECIPE = 'libffi'
SOURCES = ('https://github.com/libffi/libffi.git',)
UPSTREAM = 'https://github.com/libffi/libffi'
# The policy is two build options, not a patch: patches/series is empty.
PATCHES = None
# PR_SET_MDWE, which the probe sets before it makes a closure.
MIN_KERNEL = '6.3'
# Wolfi turns static trampolines off, so each closure needs fresh code: a
# writable and executable page, or a file mapped twice, writable and
# executable. MDWE, memfd_noexec and noexec mounts refuse each. Static
# trampolines run from the library's own read-only code; only their data
# is writable. PaX's emulated trampolines go with them.
CONFIGURE = '--disable-builddir --enable-portable-binary --enable-pax_emutramp --disable-exec-static-tramp'
HARDENED = '--disable-builddir --enable-portable-binary --disable-pax_emutramp --enable-exec-static-tramp'
BUILT_IN = 'Static trampolines are built in'
PROBED = 'A closure works with executable-memory gain refused'
PROBE = 'WEREWOLF_LIBFFI_PROBE'
SUBPACKAGES = ('libffi-dev', 'libffi-pic-dev', 'libffi-doc')
# Both provide pc:libffi: each test resolves the one it needs, ours, as it outranks Wolfi's.
TEST_UNPINNED = ('libffi-dev', 'libffi-pic-dev')


def source_commit(config):
    steps = [s for s in config['pipeline'] if s.get('uses') == 'git-checkout']
    return steps[0].get('with', {}).get('expected-commit') if len(steps) == 1 else None


def epoch(wolfi_epoch, revision):
    if not 0 <= wolfi_epoch < 1000:
        raise ValueError('Review Wolfi epoch: ' + str(wolfi_epoch))
    return 1000 * revision + wolfi_epoch


def full_version(lock):
    return lock['version'] + '-r' + str(epoch(lock['epoch'], lock['revision']))


def context(pkg, destination):
    """Nothing beside Wolfi's recipe files: the policy is in the recipe."""


def built_in():
    return {'name': BUILT_IN, 'runs': "grep -qx '#define FFI_EXEC_STATIC_TRAMP 1' fficonfig.h\n"}


def probe(pkg):
    source = (pkg.dir / 'tests/probe.c').read_text()
    if '${{' in source or PROBE in source:
        raise ValueError('libffi probe cannot be embedded in the recipe')
    return {'name': PROBED, 'runs':
            'work=$(mktemp -d)\n'
            "cat > \"$work/probe.c\" <<'" + PROBE + "'\n" + source + PROBE + '\n'
            'gcc -o "$work/probe" "$work/probe.c" -lffi\n'
            '"$work/probe"\n'}


def derive(pkg, config, lock):
    original = config['package']['name']
    if original != Path(lock['recipe']).stem or str(config['package']['version']) != lock['version']:
        raise ValueError('Review changed upstream package identity')
    pipeline = config['pipeline']
    if [i for i, s in enumerate(pipeline) if s.get('uses') == 'git-checkout'] != [0]:
        raise ValueError('Review changed upstream checkout order')
    assert source_commit(config) == lock['source_commit'], 'source pin differs from the lock'
    if pipeline[0]['with'] != {'repository': UPSTREAM + '.git', 'tag': 'v${{package.version}}',
                              'expected-commit': lock['source_commit']}:
        raise ValueError('Review changed upstream source checkout')
    configures = [s for s in pipeline if s.get('uses') == 'autoconf/configure']
    if len(configures) != 1 or configures[0].get('with', {}).get('opts', '').split() != CONFIGURE.split():
        raise ValueError('Review changed upstream configure options')
    makes = [i for i, s in enumerate(pipeline) if s.get('uses') == 'autoconf/make']
    if len(makes) != 1 or sorted(s['name'] for s in config['subpackages']) != sorted(SUBPACKAGES):
        raise ValueError('Review changed upstream build or subpackages')
    configures[0]['with']['opts'] = HARDENED
    pipeline.insert(makes[0] + 1, built_in())
    pipeline[0] = fetch(lock['source_commit'], SOURCES)
    wolfi = lock['version'] + '-r' + str(lock['epoch'])
    config['package']['epoch'] = epoch(lock['epoch'], lock['revision'])
    config['package']['description'] += ' (Werewolf static trampolines)'
    config['package'].setdefault('dependencies', {}).setdefault('provides', []).append(
        'wolfi-origin:' + original + ':' + wolfi)
    config['test']['pipeline'].append(probe(pkg))
    config['update'] = {'enabled': False}
    return config


def check(pkg, upstream, derived):
    assert derived['environment'] == upstream['environment'], 'upstream build environment changed'
    assert derived['subpackages'] == upstream['subpackages'], 'upstream subpackages changed'
    expected = []
    for step in upstream['pipeline'][1:]:
        if step.get('uses') == 'autoconf/configure':
            expected.append({**step, 'with': {**step['with'], 'opts': HARDENED}})
        else:
            expected.append(step)
        if step.get('uses') == 'autoconf/make':
            expected.append(built_in())
    assert derived['pipeline'][1:] == expected, 'an upstream build step changed'
    assert derived['test']['pipeline'][:-1] == upstream['test']['pipeline'], 'upstream tests changed'
    assert derived['test']['pipeline'][-1] == probe(pkg), 'the MDWE closure probe changed'
    assert derived['test']['environment'] == upstream['test']['environment'], 'test environment changed'
