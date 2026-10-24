"""ImageMagick for server-side document and image conversion."""
from copy import deepcopy
from derive import fetch
import runtime

TITLE = 'ImageMagick'
RECIPE = 'imagemagick-7'
SOURCES = ('https://github.com/ImageMagick/ImageMagick.git',)
PATCHES = None
check_apk = runtime.check_apk
REMOVED = {'fftw-dev', 'libx11-dev', 'libxext-dev', 'pango-dev', 'perl-dev'}
OPTIONS = '--without-x --without-fftw --without-pango --without-perl --without-gvc --without-dps'


def source_commit(config):
    first = config['pipeline'][0]
    return first.get('with', {}).get('expected-commit') if first.get('uses') == 'git-checkout' else None


def epoch(value, revision):
    if not 0 <= value < 1000:
        raise ValueError('Review Wolfi epoch')
    return revision * 1000 + value


def full_version(lock):
    return lock['version'] + '-r' + str(epoch(lock['epoch'], lock['revision']))


def context(pkg, destination):
    pass


def derive(pkg, config, lock):
    if config['package']['name'] != RECIPE or lock['recipe'] != RECIPE + '.yaml':
        raise ValueError('Review changed ImageMagick identity')
    steps = config['pipeline']
    if [s.get('uses') for s in steps] != ['git-checkout', 'autoconf/configure', 'autoconf/make', 'autoconf/make-install', 'strip']:
        raise ValueError('Review changed ImageMagick pipeline')
    if steps[1] != {'uses': 'autoconf/configure'} or source_commit(config) != lock['source_commit']:
        raise ValueError('Review changed ImageMagick configure/source')
    deps = config['environment']['contents']['packages']
    if not REMOVED <= set(deps):
        raise ValueError('Review changed optional build dependencies')
    steps[0] = fetch(lock['source_commit'], SOURCES)
    steps[1]['with'] = {'opts': OPTIONS}
    config['environment']['contents']['packages'] = [d for d in deps if d not in REMOVED]
    config['package']['epoch'] = epoch(lock['epoch'], lock['revision'])
    origin = 'upstream-origin:' + RECIPE + ':' + lock['version'] + '-r' + str(lock['epoch'])
    for section in [config['package']] + config['subpackages']:
        section.setdefault('dependencies', {}).setdefault('provides', []).append(origin)
    config['test']['pipeline'].append({'name': 'Server-side document and image consumers',
        'needs': {'packages': ['ghostscript', 'ttf-dejavu']},
        'runs': (pkg.dir / 'tests/probe.sh').read_text()})
    config['update'] = {'enabled': False}
    return config


def check(pkg, upstream, derived):
    expected = deepcopy(upstream)
    lock = {'recipe': RECIPE + '.yaml', 'source_commit': source_commit(upstream),
            'version': str(upstream['package']['version']), 'epoch': upstream['package']['epoch'],
            'revision': pkg.policy['revision']}
    assert derive(pkg, expected, lock) == derived
