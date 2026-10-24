"""Wolfi's newest stable Node.js, with Werewolf's compiled capability floor."""
from pathlib import Path
import shutil
from derive import fetch
import patching
import runtime

TITLE = 'Node.js'
FAMILY = 'nodejs-'
SOURCES = ('https://github.com/nodejs/node.git',)
UPSTREAM = 'https://github.com/nodejs/node'
PATCHES = 'werewolf/patches'
check_apk = runtime.check_apk
GROUPS = ('exec', 'native', 'inline', 'writable-source', 'environment')
ALLOW = 'Permit capabilities only in the build guest'
BUILD = 'Configure and build'
PRUNE = 'Omit package managers and documentation'
FLAGS = (' --without-inspector --without-npm --without-node-options'
         ' --disable-single-executable-application --without-lief')
OMITTED = ('${{package.name}}-minimal', '${{package.name}}-minimal-compat',
           'libnode${{vars.soversion}}', '${{package.name}}-oci-entrypoint-compat',
           '${{package.name}}-doc', '${{package.name}}-minimal-doc')
NPM_CLEANUP = '''rm -rf "${{targets.destdir}}"/usr/lib/node_modules
rm "${{targets.destdir}}"/usr/bin/npm
rm "${{targets.destdir}}"/usr/bin/npx'''
PROBE_JS, PROBE_SH = 'WEREWOLF_NODE_PROBE_JS', 'WEREWOLF_NODE_PROBE_SH'
# Wolfi's own patches, after its checkout, each reviewed; any other stops the
# derivation. system-brotli-block-switch drops a constant the system brotli
# lacks, so Node builds against it (26.12.1-r0). ncrypto-pbkdf2-pkcs5 passes
# pkcs5=1 to PBKDF2 under OpenSSL 4 as Node always did under OpenSSL 3: the
# SP 800-132 lower bounds, which only the FIPS provider applies by default,
# stay off (26.12.1-r2).
WOLFI_PATCHES = ('system-brotli-block-switch.patch', 'ncrypto-pbkdf2-pkcs5.patch')


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
    shutil.copytree(pkg.dir / 'patches', destination / PATCHES)


def grant_all():
    return (runtime.build_root() + 'mkdir -p /etc/runtime-hardening/allow/all/node\n'
            'for group in ' + ' '.join(GROUPS) + '; do\n'
            '  : > /etc/runtime-hardening/allow/all/node/$group\n'
            'done\n')


def configure(script):
    before = 'configure.py ${{vars.configure-flags}}'
    if script.count(before) != 1 or '--without-' in script.replace(
            '# NOTE: --without-corepack is no longer supported in Node.js 25+', ''):
        raise ValueError('Review changed upstream Node configure step')
    return script.replace(before, before + FLAGS)


def prune():
    return {'name': PRUNE, 'runs': '''# --without-npm must actually omit npm, npx and their modules.
test ! -e "${{targets.destdir}}/usr/bin/npm"
test ! -e "${{targets.destdir}}/usr/bin/npx"
test ! -e "${{targets.destdir}}/usr/lib/node_modules/npm"
rm -R "${{targets.destdir}}/usr/share/man"
'''}


def probe(pkg):
    js = (pkg.dir / 'tests/probe.cjs').read_text()
    sh = (pkg.dir / 'tests/probe.sh').read_text()
    for text in (js, sh):
        if '${{' in text or PROBE_JS in text or PROBE_SH in text:
            raise ValueError('Node probe cannot be embedded in the recipe')
    return (runtime.root_probe('node', ['node', '--version']) +
            'work=$(mktemp -d)\n' +
            'cat > "$work/probe.cjs" <<\'' + PROBE_JS + "'\n" + js + PROBE_JS + '\n' +
            'cat > "$work/probe.sh" <<\'' + PROBE_SH + "'\n" + sh + PROBE_SH + '\n' +
            'sh "$work/probe.sh" "$work"\n')


def wolfi_patches(pipeline):
    """How many of Wolfi's patch steps follow its checkout, each one reviewed."""
    n = 0
    while n + 1 < len(pipeline) and pipeline[n + 1].get('uses') == 'patch':
        names = pipeline[n + 1].get('with', {}).get('patches', '').split()
        if not names or any(name not in WOLFI_PATCHES for name in names):
            raise ValueError('Review new upstream patch: ' + ' '.join(names))
        n += 1
    return n


def derive(pkg, config, lock):
    original = config['package']['name']
    if original != Path(lock['recipe']).stem or str(config['package']['version']) != lock['version']:
        raise ValueError('Review changed upstream package identity')
    pipeline = config['pipeline']
    checkouts = [i for i, s in enumerate(pipeline) if s.get('uses') == 'git-checkout']
    if checkouts != [0]:
        raise ValueError('Review changed upstream checkout order')
    assert source_commit(config) == lock['source_commit'], 'source pin differs from the lock'
    if pipeline[0]['with'] != {'repository': UPSTREAM + '.git', 'tag': 'v${{package.version}}',
                              'expected-commit': lock['source_commit']}:
        raise ValueError('Review changed upstream source checkout')
    if sorted(s['name'] for s in config['subpackages']) != sorted(OMITTED):
        raise ValueError('Review changed upstream subpackages')
    patched = wolfi_patches(pipeline)
    builds = [s for s in pipeline if s.get('name') == BUILD]
    cleanup = [s for s in pipeline if s.get('runs', '').strip() == NPM_CLEANUP]
    if len(builds) != 1 or len(cleanup) != 1 or pipeline.index(builds[0]) != 1 + patched:
        raise ValueError('Review changed upstream build or npm cleanup')
    if pipeline[-1] != cleanup[0]:
        raise ValueError('Review changed upstream packaging order')
    builds[0]['runs'] = configure(builds[0]['runs'])
    pipeline[0] = fetch(lock['source_commit'], SOURCES)
    # Ours apply after Wolfi's, as prepare.py applies them.
    pipeline[1 + patched:1 + patched] = [patching.step(PATCHES), {'name': ALLOW, 'runs': grant_all()}]
    pipeline[-1] = prune()
    config['subpackages'] = []
    for var in ('docker-entrypoint-hash', 'soversion'):
        if var not in config.get('vars', {}):
            raise ValueError('Review changed upstream variable ' + var)
        del config['vars'][var]
    wolfi = lock['version'] + '-r' + str(lock['epoch'])
    config['package']['epoch'] = epoch(lock['epoch'], lock['revision'])
    config['package']['description'] += ' (Werewolf fixed hardening policy)'
    config['package'].setdefault('dependencies', {}).setdefault('provides', []).extend([
        'wolfi-origin:' + original + ':' + wolfi,
        'nodejs-hardened=${{package.full-version}}'])
    config['test']['pipeline'].append({
        'name': 'Werewolf protections hold, and allowances take each back',
        'needs': {'packages': ['build-base']}, 'runs': probe(pkg)})
    config['update'] = {'enabled': False}
    return config


def test_recipe(config, repository):
    config['test']['pipeline'].insert(0, {'name': ALLOW, 'runs': grant_all()})
    return config


def check(pkg, upstream, derived):
    assert not derived['subpackages'], 'unreviewed subpackage restored'
    assert derived['environment'] == upstream['environment'], 'upstream build environment changed'
    assert derived['vars'] == {k: v for k, v in upstream['vars'].items()
                               if k not in ('docker-entrypoint-hash', 'soversion')}, 'upstream variables changed'
    patched = wolfi_patches(upstream['pipeline'])
    ours = 1 + patched
    assert derived['pipeline'][1:ours] == upstream['pipeline'][1:ours], "Wolfi's patches changed"
    expected = []
    for step in upstream['pipeline'][ours:]:
        if step.get('runs', '').strip() == NPM_CLEANUP:
            expected.append(prune())
        elif step.get('name') == BUILD:
            expected.append({**step, 'runs': configure(step['runs'])})
        else:
            expected.append(step)
    assert derived['pipeline'][ours + 1] == {'name': ALLOW, 'runs': grant_all()}, 'build guest allowances changed'
    assert derived['pipeline'][ours + 2:] == expected, 'an upstream build step changed'
    assert derived['test']['pipeline'][:-1] == upstream['test']['pipeline'], 'upstream tests changed'
    assert derived['test'].get('environment') == upstream['test'].get('environment'), 'test environment changed'
