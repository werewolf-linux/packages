"""FFmpeg for local media conversion; no network, display, or font libraries."""
from copy import deepcopy
from pathlib import Path
from derive import fetch
import runtime

TITLE = 'FFmpeg'
FAMILY = 'ffmpeg-'
SOURCES = ('https://github.com/FFmpeg/FFmpeg.git',)
PATCHES = None
check_apk = runtime.check_apk
REMOVED = ('ffplay', 'openssl', 'libsrt', 'libssh', 'libzmq', 'libxml2',
           'libfontconfig', 'libfreetype', 'libharfbuzz')
REMOVED_BUILD = ('libsdl2-dev', 'libsrt', 'libssh-dev', 'zeromq-dev',
                 'openssl-${{vars.openssl-version}}-dev', 'libxml2-dev',
                 'fontconfig-dev', 'freetype-dev', 'harfbuzz-dev', 'expat-dev', 'libsodium-dev')
FLAGS = '''--disable-autodetect --disable-network --disable-protocols
    --enable-protocol=file,pipe --disable-indevs --enable-indev=lavfi --disable-outdevs
    --enable-zlib --enable-bzlib'''


def source_commit(config):
    step = config['pipeline'][0]
    return step.get('with', {}).get('expected-commit') if step.get('uses') == 'git-checkout' else None


def epoch(value, revision):
    if not 0 <= value < 1000:
        raise ValueError('Review Wolfi epoch')
    return revision * 1000 + value


def full_version(lock):
    return lock['version'] + '-r' + str(epoch(lock['epoch'], lock['revision']))


def context(pkg, destination):
    pass


def configure(script):
    for name in REMOVED:
        flag = '--enable-' + name
        if script.split().count(flag) != 1:
            raise ValueError('Review changed configure option ' + flag)
        script = script.replace(flag, '--disable-' + name)
    if not script.rstrip().endswith('--disable-libzmq'):
        raise ValueError('Review changed configure command')
    return script.rstrip() + ' ' + ' '.join(FLAGS.split()) + '\n'


def retained_tests(config):
    test = deepcopy(config['test'])
    checks = [s for s in test['pipeline'] if s.get('uses') == 'test/tw/help-check']
    if len(checks) != 1 or checks[0]['with']['bins'].split() != ['ffmpeg', 'ffprobe', 'ffplay']:
        raise ValueError('Review changed CLI tests')
    checks[0]['with']['bins'] = 'ffmpeg\nffprobe\n'
    return test


def probe(pkg):
    return {'name': 'Local media conversions work without network or display support',
            'runs': (pkg.dir / 'tests/probe.sh').read_text()}


def derive(pkg, config, lock):
    if config['package']['name'] != Path(lock['recipe']).stem or str(config['package']['version']) != lock['version']:
        raise ValueError('Review changed FFmpeg identity')
    steps = config['pipeline']
    kinds = [s.get('uses') for s in steps]
    prefix = ['git-checkout', None, 'autoconf/make']
    suffix = ['autoconf/make-install', None, 'strip']
    if kinds not in (prefix + suffix, prefix + ['upstream-tests/ffmpeg-fate'] + suffix):
        raise ValueError('Review changed FFmpeg build pipeline')
    if steps[0]['with'] != {'repository': 'https://github.com/FFmpeg/FFmpeg', 'expected-commit': lock['source_commit'], 'tag': 'n${{package.version}}'}:
        raise ValueError('Review changed source')
    steps[1]['runs'] = configure(steps[1]['runs'])
    steps[0] = fetch(lock['source_commit'], SOURCES)
    dependencies = config['environment']['contents']['packages']
    for name in REMOVED_BUILD:
        if dependencies.count(name) != 1:
            raise ValueError('Review changed build dependency ' + name)
        dependencies.remove(name)
    dependencies.extend(['zlib-dev', 'bzip2-dev', 'xz-dev'])
    config['test'] = retained_tests(config)
    config['test']['pipeline'].append(probe(pkg))
    config['package']['epoch'] = epoch(lock['epoch'], lock['revision'])
    origin = 'wolfi-origin:' + config['package']['name'] + ':' + lock['version'] + '-r' + str(lock['epoch'])
    for section in [config['package']] + config['subpackages']:
        section.setdefault('dependencies', {}).setdefault('provides', []).append(origin)
    config['package']['dependencies']['provides'].append('ffmpeg-hardened=${{package.full-version}}')
    config['update'] = {'enabled': False}
    return config


def check(pkg, upstream, derived):
    assert derived['pipeline'][2:] == upstream['pipeline'][2:]
    assert derived['pipeline'][1]['runs'] == configure(upstream['pipeline'][1]['runs'])
    environment = deepcopy(upstream['environment'])
    environment['contents']['packages'] = [d for d in environment['contents']['packages'] if d not in REMOVED_BUILD] + ['zlib-dev', 'bzip2-dev', 'xz-dev']
    assert derived['environment'] == environment
    assert derived['vars'] == upstream['vars'] and derived['data'] == upstream['data']
    subs = deepcopy(derived['subpackages'])
    for sub in subs:
        sub['dependencies']['provides'] = [p for p in sub['dependencies']['provides'] if not p.startswith('wolfi-origin:')]
    assert subs == upstream['subpackages']
    assert derived['test']['pipeline'] == retained_tests(upstream)['pipeline'] + [probe(pkg)]
