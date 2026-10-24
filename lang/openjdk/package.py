"""OpenJDK: Wolfi's newest stable OpenJDK, with six policy patches (README.md).

The hooks scripts/packages.py names. Packages keep Wolfi's names; their epoch
sorts above every Wolfi rebuild of the same JDK release."""
from pathlib import Path
import re
import shutil
from derive import fetch
import patching
import runtime

check_apk = runtime.check_apk

TITLE = 'OpenJDK'
FAMILY = 'openjdk-'
PATCHES = 'werewolf/patches'  # in the build context
REPOSITORY = 'https://github.com/openjdk/jdk${{vars.java-version}}u'
UPSTREAM = 'https://github.com/openjdk/jdk'
PROBE_JAVA, PROBE_SH = 'WEREWOLF_PROBE_JAVA', 'WEREWOLF_PROBE_SH'
KEPT_SUBPACKAGES = tuple('${{package.name}}' + suffix for suffix in (
    '-jre', '-default-jvm', '-default-jdk'))
OMITTED_SUBPACKAGES = tuple('${{package.name}}' + suffix for suffix in (
    '-jmods', '-demos', '-oci-entrypoint-compat'))
BUILD = 'make jdk-image legacy-jre-image jmods'
PRUNE = 'Remove demos before packaging the JDK'
ALLOW_ROOT = 'Permit effective root only in the build guest'

def checkout(config):
    """Wolfi's one git-checkout step: its index and the step."""
    found = [(i, step) for i, step in enumerate(config['pipeline']) if step.get('uses') == 'git-checkout']
    if len(found) != 1:
        raise ValueError('Review changed upstream source checkout before deriving')
    return found[0]

def source_commit(config):
    try:
        return checkout(config)[1].get('with', {}).get('expected-commit')
    except ValueError:
        return None

def sources(lock):
    """The update repository of the locked release, then mainline, which holds each GA."""
    feature = lock['version'].split('.')[0]
    if not re.fullmatch('[0-9]+', feature):
        raise ValueError('Review OpenJDK version ' + lock['version'])
    return ('https://github.com/openjdk/jdk' + feature + 'u.git', UPSTREAM + '.git')

def epoch(wolfi_epoch, revision):
    """Our epoch: above every Wolfi rebuild of the same JDK release, so apk
    prefers ours, and moving when either Wolfi's epoch or our revision does."""
    if not 0 <= wolfi_epoch < 1000:
        raise ValueError('Review Wolfi epoch ' + str(wolfi_epoch) + ': ours would no longer sort above it')
    return 1000 * revision + wolfi_epoch

def full_version(lock):
    return lock['version'] + '-r' + str(epoch(lock['epoch'], lock['revision']))

def context(pkg, destination):
    # Beside Wolfi's files, under a name the JDK's source does not use at its top.
    shutil.copytree(pkg.dir / 'patches', destination / PATCHES)

def retained_subpackages(config):
    subpackages = config['subpackages']
    if sorted(s['name'] for s in subpackages) != sorted(KEPT_SUBPACKAGES + OMITTED_SUBPACKAGES):
        raise ValueError('Review changed upstream subpackages')
    return [s for s in subpackages if s['name'] in KEPT_SUBPACKAGES]

def omit_jmods(pipeline):
    builds = [s for s in pipeline if s.get('runs', '').strip() == BUILD]
    if len(builds) != 1:
        raise ValueError('Review changed upstream JDK image build step')
    builds[0]['runs'] = 'make jdk-image legacy-jre-image\n'

def prune():
    # The -demos step normally moves this tree out of the main package.
    return {'name': PRUNE, 'runs':
            'rm -R "${{targets.destdir}}/usr/lib/jvm/java-${{vars.java-version}}-openjdk/demo"\n'}

def probe(pkg):
    java = (pkg.dir / 'tests/Probe.java').read_text()
    sh = (pkg.dir / 'tests/probe.sh').read_text()
    for text in (java, sh):
        if '${{' in text or PROBE_JAVA in text or PROBE_SH in text:
            raise ValueError('tests/Probe.java and tests/probe.sh cannot be embedded in a Melange recipe')
    return (runtime.root_probe('java', ['java', '-version']) + 'work=$(mktemp -d)\n'
            "cat > \"$work/Probe.java\" <<'" + PROBE_JAVA + "'\n" + java + PROBE_JAVA + '\n' +
            "cat > \"$work/probe.sh\" <<'" + PROBE_SH + "'\n" + sh + PROBE_SH + '\n' +
            'sh "$work/probe.sh" "$work"\n')

def derive(pkg, config, lock):
    original = config['package']['name']
    if original != Path(lock['recipe']).stem or str(config['package']['version']) != lock['version']:
        raise ValueError('Review changed upstream package identity before deriving')
    pipeline = config['pipeline']
    index, source = checkout(config)
    assert source_commit(config) == lock['source_commit'], 'source pin differs from the lock'
    if set(source.get('with', {})) != {'repository', 'tag', 'expected-commit'} or \
            source['with']['repository'] != REPOSITORY:
        raise ValueError('Review changed upstream source checkout before deriving')
    pipeline[index] = fetch(lock['source_commit'], sources(lock))
    # Wolfi's patches first, as Wolfi applies them; then ours.
    patched = [i for i, step in enumerate(pipeline) if step.get('uses') == 'patch']
    if not patched or patched[0] < index:
        raise ValueError('Review changed upstream patch steps before deriving')
    pipeline.insert(patched[-1] + 1, patching.step(PATCHES))
    pipeline.insert(patched[-1] + 2, {'name': ALLOW_ROOT, 'runs': runtime.build_root()})
    omit_jmods(pipeline)
    pipeline.append(prune())
    config['subpackages'] = retained_subpackages(config)
    # Only the omitted entrypoint's fetch used this variable.
    if 'cacert-entrypoint-hash' not in config.get('vars', {}):
        raise ValueError('Review changed upstream OCI entrypoint hash')
    del config['vars']['cacert-entrypoint-hash']
    wolfi = str(config['package']['version']) + '-r' + str(config['package']['epoch'])
    config['package']['epoch'] = epoch(config['package']['epoch'], lock['revision'])
    config['package']['description'] += ' (Werewolf fixed hardening policy)'
    # Each package names the Wolfi build it rebuilds: werewolf matches Wolfi's
    # CVE fixes against that (lib/prebuilt.zig), not against our epoch.
    for section in [config['package']] + config['subpackages']:
        section.setdefault('dependencies', {}).setdefault('provides', []).append(
            'wolfi-origin:' + original + ':' + wolfi)
    test = config['test']
    if 'openjdk-${{vars.java-version}}-default-jdk' not in test.get('environment', {}).get('contents', {}).get('packages', []):
        raise ValueError('Review changed upstream package test environment: the probe needs the JDK')
    test['pipeline'].append({'name': 'Werewolf protections hold, and allowances take each back',
                             'needs': {'packages': ['build-base']},
                             'runs': probe(pkg)})
    config['update'] = {'enabled': False}
    return config

def test_recipe(config, repository):
    for section in [config] + config.get('subpackages', []):
        test = section.get('test')
        if test and test.get('pipeline'):
            test['pipeline'].insert(0, {'name': ALLOW_ROOT, 'runs': runtime.build_root()})
    return config

def check(pkg, upstream, derived):
    """Keep Wolfi's JDK/JRE build and tests, with documented package omissions."""
    configure = lambda c: [x for x in c['pipeline'] if x.get('uses') == 'autoconf/configure']
    assert configure(upstream) == configure(derived), 'upstream configure changed'
    assert derived['environment'] == upstream['environment'], 'upstream build environment changed'
    patches = lambda c: [x for x in c['pipeline'] if x.get('uses') == 'patch']
    assert patches(upstream) == patches(derived), "Wolfi's patches changed"
    ours = next(i for i, x in enumerate(derived['pipeline']) if x.get('name') == 'Apply Werewolf policy patches')
    assert derived['pipeline'][ours - 1] == patches(upstream)[-1], "our patches must follow Wolfi's"
    assert derived['pipeline'][-1] == prune(), 'demos must be removed before packaging'
    assert sum(s.get('name') == PRUNE for s in derived['pipeline']) == 1, 'one demo cleanup step'
    expected = {'pipeline': [dict(step) for step in upstream['pipeline']]}
    omit_jmods(expected['pipeline'])
    steps = lambda c: [x for x in c['pipeline']
                       if x.get('uses') != 'git-checkout'
                       and x.get('name') not in ('Apply Werewolf policy patches', 'Fetch the locked source commit', PRUNE, ALLOW_ROOT)]
    assert steps(expected) == steps(derived), 'an upstream build step changed'
    assert [s for s in derived['pipeline'] if s.get('name') == ALLOW_ROOT] == [
        {'name': ALLOW_ROOT, 'runs': runtime.build_root()}], 'build-root marker missing or changed'
    assert derived['vars'] == {k: v for k, v in upstream['vars'].items() if k != 'cacert-entrypoint-hash'}, 'upstream variables changed'
    strip = lambda subs: [{**s, 'dependencies': {**s.get('dependencies', {}), 'provides': [
        p for p in s.get('dependencies', {}).get('provides', []) if not p.startswith('wolfi-origin:')]}}
        for s in subs]
    assert strip(retained_subpackages(upstream)) == strip(derived['subpackages']), 'an upstream subpackage changed'
    for test in upstream['test']['pipeline']:
        assert test in derived['test']['pipeline'], "Wolfi's package test removed"
    assert upstream['test']['environment'] == derived['test']['environment'], 'upstream test environment changed'
