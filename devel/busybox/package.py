"""Wolfi's busybox, running the applets its tools exec by name from itself."""
from pathlib import Path
from derive import fetch

TITLE = 'busybox'
RECIPE = 'busybox'
SOURCES = ('https://github.com/vda-linux/busybox_mirror.git', 'https://git.busybox.net/busybox.git')
UPSTREAM = 'https://github.com/vda-linux/busybox_mirror'
# The policy is a build option, not a patch: patches/series is empty.
PATCHES = None
VARIANTS = ('busybox', 'busybox-full', 'busybox-static')
# Each variant holds /usr/bin/busybox, so each test resolves its own alone.
TEST_UNPINNED = VARIANTS
# Werewolf's glibc refuses a bare-name execvp rather than search PATH
# (devel/glibc). Without this, env, timeout, nohup, nice, xargs, setsid, time,
# flock and chroot could run no command by name; with it, a name that is an
# applet runs through BUSYBOX_EXEC_PATH, an absolute path, and any other name
# still needs one.
PREFER = ("# Werewolf: an applet run by name runs from /usr/bin/busybox, with no PATH\n"
          "# search, which Werewolf's glibc refuses (devel/busybox/README.md).\n"
          "sed -i 's/^# CONFIG_FEATURE_PREFER_APPLETS is not set$/CONFIG_FEATURE_PREFER_APPLETS=y/' .config\n"
          "grep -qx 'CONFIG_FEATURE_PREFER_APPLETS=y' .config\n"
          "grep -qx 'CONFIG_BUSYBOX_EXEC_PATH=\"/usr/bin/busybox\"' .config\n")
PROBED = 'Tools run applets by name without searching PATH'
PROBE = 'WEREWOLF_BUSYBOX_PROBE'


def source_commit(config):
    steps = [s for s in config['pipeline'] if s.get('uses') == 'git-checkout']
    return steps[0].get('with', {}).get('expected-commit') if len(steps) == 1 else None


def epoch(wolfi_epoch, revision):
    if not 0 <= wolfi_epoch < 1000:
        raise ValueError('Review Wolfi epoch: ' + str(wolfi_epoch))
    return 1000 * revision + wolfi_epoch


def full_version(lock):
    return lock['version'] + '-r' + str(epoch(lock['epoch'], lock['revision']))


def upstream_patches(config):
    """Wolfi's patches, in order, for prepare: its patch steps run in build/,
    the source, so ../NAME is the recipe directory's NAME."""
    names = []
    for step in config['pipeline']:
        if step.get('uses') != 'patch':
            continue
        if step.get('working-directory') != 'build':
            raise ValueError('Review changed upstream busybox patch step')
        for name in step['with']['patches'].split():
            if not name.startswith('../') or '/' in name[3:]:
                raise ValueError('Review changed upstream busybox patch path ' + name)
            names.append(name[3:])
    return names


def context(pkg, destination):
    """Nothing beside Wolfi's recipe files: the policy is in the recipe."""


def probe(pkg):
    script = (pkg.dir / 'tests/probe.sh').read_text()
    if '${{' in script or PROBE in script:
        raise ValueError('busybox probe cannot be embedded in the recipe')
    return {'name': PROBED, 'runs': "cat > /tmp/busybox-probe.sh <<'" + PROBE + "'\n" +
            script + PROBE + '\n/usr/bin/busybox sh /tmp/busybox-probe.sh\n'}


def configures(config):
    """Each variant's Configure step: the main pipeline's, then each subpackage's."""
    sections = [config['pipeline']] + [s.get('pipeline', []) for s in config['subpackages']]
    return [step for steps in sections for step in steps if step.get('name') == 'Configure']


def derive(pkg, config, lock):
    if (config['package']['name'] != Path(lock.get('effective_recipe', lock['recipe'])).stem or
            str(config['package']['version']) != lock['version']):
        raise ValueError('Review changed upstream busybox identity')
    pipeline = config['pipeline']
    if [i for i, s in enumerate(pipeline) if s.get('uses') == 'git-checkout'] != [0]:
        raise ValueError('Review changed upstream busybox checkout')
    if pipeline[0] != {'uses': 'git-checkout', 'working-directory': 'build', 'with': {
            'expected-commit': lock['source_commit'], 'repository': UPSTREAM,
            'tag': '${{vars.mangled-package-version}}'}}:
        raise ValueError('Review changed upstream busybox source')
    if sorted(s['name'] for s in config['subpackages']) != sorted(VARIANTS[1:]):
        raise ValueError('Review changed upstream busybox subpackages')
    steps = configures(config)
    if len(steps) != len(VARIANTS) or any('CONFIG_FEATURE_PREFER_APPLETS' in s['runs'] for s in steps):
        raise ValueError('Review changed upstream busybox configuration')
    for step in steps:
        step['runs'] += PREFER
    pipeline[0] = {**fetch(lock['source_commit'], SOURCES), 'working-directory': 'build'}
    wolfi = lock['version'] + '-r' + str(lock['epoch'])
    config['package']['epoch'] = epoch(lock['epoch'], lock['revision'])
    config['package']['description'] += ' (Werewolf: applets exec applets by path)'
    for section in [config['package']] + config['subpackages']:
        section.setdefault('dependencies', {}).setdefault('provides', []).append(
            'upstream-origin:busybox:' + wolfi)
    for section in [config] + config['subpackages']:
        section.setdefault('test', {}).setdefault('pipeline', []).append(probe(pkg))
    config['update'] = {'enabled': False}
    return config


def check(pkg, upstream, derived):
    assert derived['environment'] == upstream['environment'], 'upstream build environment changed'
    assert derived.get('vars') == upstream.get('vars') and \
        derived['var-transforms'] == upstream['var-transforms'], 'upstream variables changed'
    assert derived['pipeline'][0]['working-directory'] == 'build', 'the source must be fetched into build'
    expected = []
    for step in upstream['pipeline'][1:]:
        expected.append({**step, 'runs': step['runs'] + PREFER} if step.get('name') == 'Configure' else step)
    assert derived['pipeline'][1:] == expected, 'an upstream build step changed'
    assert [s['name'] for s in derived['subpackages']] == [s['name'] for s in upstream['subpackages']]
    for sub, original in zip(derived['subpackages'], upstream['subpackages']):
        steps = [{**s, 'runs': s['runs'] + PREFER} if s.get('name') == 'Configure' else s
                 for s in original.get('pipeline', [])]
        assert sub.get('pipeline', []) == steps, 'an upstream ' + sub['name'] + ' step changed'
        assert sub['test']['pipeline'][:-1] == original.get('test', {}).get('pipeline', []), \
            'upstream ' + sub['name'] + ' tests changed'
        assert sub['test']['pipeline'][-1] == probe(pkg), sub['name'] + ' lost the applet probe'
    assert derived['test']['pipeline'][:-1] == upstream['test']['pipeline'], 'upstream tests changed'
    assert derived['test']['pipeline'][-1] == probe(pkg), 'busybox lost the applet probe'
    assert len([s for s in configures(derived) if s['runs'].endswith(PREFER)]) == len(VARIANTS)
