"""libvips with the image formats allowed by Mastodon's initializer."""
from copy import deepcopy
from derive import fetch
import runtime

TITLE = 'libvips'
RECIPE = 'libvips'
SOURCES = ('https://github.com/libvips/libvips.git',)
PATCHES = None
check_apk = runtime.check_apk
ORIGINAL_BUILD = '''bubblewrap build-base busybox cmake expat-dev freetype-dev glib-dev
    glibc-2.44-dev libexif-dev libheif-dev libjpeg-turbo-dev librsvg-dev meson'''.split()
BUILD = '''build-base busybox expat-dev glib-dev glibc-2.44-dev libexif-dev libjpeg-turbo-dev
    libpng-dev libwebp-dev lcms2-dev meson zlib-dev'''.split()
OPTIONS = '''-Dauto_features=disabled -Dheif=disabled -Dmodules=disabled
    -Dexif=enabled -Djpeg=enabled -Dpng=enabled -Dwebp=enabled -Dlcms=enabled
    -Dzlib=enabled -Dnsgif=true -Dppm=false -Danalyze=false -Dradiance=false
    -Dcplusplus=false -Ddeprecated=false -Dexamples=false -Ddocs=false'''
OMITTED_TESTS = ('Check that HEIF support is built in', 'Encode and decode HEIC and AVIF images')


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


def prune():
    return {'name': 'Omit CLI manpages', 'runs': 'rm -rf "${{targets.destdir}}/usr/share/man"\n'}


def probe(pkg):
    code = (pkg.dir / 'tests/probe.sh').read_text()
    if 'VIPS_PROBE' in code or '${{' in code:
        raise ValueError('Probe contains embedding delimiter')
    return {'name': 'Retained formats work and unused loaders are absent', 'runs': code}


def derive(pkg, config, lock):
    if config['package']['name'] != 'libvips' or lock['recipe'] != 'libvips.yaml':
        raise ValueError('Review changed libvips identity')
    if str(config['package']['version']) != lock['version']:
        raise ValueError('Review changed version')
    steps = config['pipeline']
    if [s.get('uses') for s in steps] != ['git-checkout', 'meson/configure', 'meson/compile', 'meson/install', 'strip']:
        raise ValueError('Review changed libvips pipeline')
    if steps[0]['with'] != {'repository': 'https://github.com/libvips/libvips', 'tag': 'v${{package.version}}', 'expected-commit': lock['source_commit']}:
        raise ValueError('Review changed source')
    if steps[1]['with'] != {'opts': '-Dheif=enabled'}:
        raise ValueError('Review changed configure options')
    if sorted(config['environment']['contents']['packages']) != sorted(ORIGINAL_BUILD):
        raise ValueError('Review changed build dependencies')
    if [s['name'] for s in config['subpackages']] != ['libvips-dev', 'libvips-doc']:
        raise ValueError('Review changed subpackages')
    if not all(sum(t.get('name') == name for t in config['test']['pipeline']) == 1 for name in OMITTED_TESTS):
        raise ValueError('Review changed HEIF tests')
    steps[0] = fetch(lock['source_commit'], SOURCES)
    steps[1]['with']['opts'] = ' '.join(OPTIONS.split())
    steps.append(prune())
    config['environment']['contents']['packages'] = BUILD.copy()
    config['subpackages'] = config['subpackages'][:1]
    config['test']['pipeline'] = [t for t in config['test']['pipeline'] if t.get('name') not in OMITTED_TESTS] + [probe(pkg)]
    config['package']['epoch'] = epoch(lock['epoch'], lock['revision'])
    origin = 'wolfi-origin:libvips:' + lock['version'] + '-r' + str(lock['epoch'])
    for section in [config['package']] + config['subpackages']:
        section.setdefault('dependencies', {}).setdefault('provides', []).append(origin)
    config['package']['dependencies']['provides'].append('libvips-hardened=${{package.full-version}}')
    config['update'] = {'enabled': False}
    return config


def check(pkg, upstream, derived):
    assert derived['pipeline'][2:] == upstream['pipeline'][2:] + [prune()]
    assert derived['pipeline'][1]['with']['opts'] == ' '.join(OPTIONS.split())
    environment = deepcopy(upstream['environment'])
    environment['contents']['packages'] = BUILD.copy()
    assert derived['environment'] == environment
    assert [s['name'] for s in derived['subpackages']] == ['libvips-dev']
    dev = deepcopy(derived['subpackages'][0]);dev.pop('dependencies')
    assert dev == upstream['subpackages'][0]
    assert derived['test']['pipeline'] == [t for t in upstream['test']['pipeline'] if t.get('name') not in OMITTED_TESTS] + [probe(pkg)]
