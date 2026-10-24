"""PHP: Wolfi's newest stable PHP, with runtime policy patches (README.md).

The hooks scripts/packages.py names. Packages keep Wolfi's names; their epoch
sorts above every Wolfi rebuild of the same PHP release."""
from copy import deepcopy
from pathlib import Path
import re
import shutil
from derive import fetch
import patching
import runtime

check_apk = runtime.check_apk

TITLE = 'PHP'
FAMILY = 'php-'
SOURCES = ('https://github.com/php/php-src.git',)
UPSTREAM = 'https://github.com/php/php-src'
PATCHES = 'werewolf/patches'  # in the build context
# What the build leaves out, and the configure flag that builds each.
REMOVED = {'${{package.name}}-dbg': '--enable-phpdbg=shared',  # phpdbg, an interactive debugger
           '${{package.name}}-pear': '--with-pear',  # PEAR, which installs code from the network
           '${{package.name}}-apache': '--with-apxs2=/usr/bin/apxs'}
OMITTED_SUBPACKAGES = tuple(REMOVED) + ('${{package.name}}-cgi', '${{package.name}}-doc')
KEPT_SUBPACKAGES = tuple('${{package.name}}' + suffix for suffix in (
    '-config', '-${{range.key}}', '-${{range.key}}-config', '-dev', '-fpm', '-fpm-config'))
# Current app forms use these. New extensions require an identified consumer.
KEPT_EXTENSIONS = frozenset('''bcmath calendar ctype curl dom exif fileinfo gd gmp iconv intl
    ldap mbstring mysqli mysqlnd openssl pcntl pdo pdo_mysql pdo_pgsql pdo_sqlite pgsql
    posix simplexml soap sodium sysvsem xml xmlreader xmlwriter zip'''.split())
OMITTED_EXTENSIONS = {
    'bz2': '--with-bz2=shared', 'gettext': '--with-gettext=shared',
    'pdo_dblib': '--with-pdo-dblib=shared', 'pdo_odbc': '--with-pdo-odbc=shared,unixODBC,/usr',
    'odbc': '--with-unixODBC=shared,/usr', 'sockets': '--enable-sockets=shared',
    'tidy': '--with-tidy=shared,/usr', 'xsl': '--with-xsl=shared',
    'ftp': '--enable-ftp=shared', 'sysvshm': '--enable-sysvshm=shared', 'phar': '--enable-phar=shared'}
# Neither command-line editing nor XPM images are used by the app forms.
OMITTED_LIBRARIES = ('--with-readline', '--with-xpm')
APXS_STEPS = ('Copy httpd.conf before install for apxs', 'Remove httpd.conf after install for apxs')
PRUNE = 'Remove manpages before splitting packages'
# Exercise the retained optional extensions as well as the core function floor.
PROBE_PACKAGES = ['${{package.name}}-pcntl', '${{package.name}}-posix',
                  '${{package.name}}-pdo', '${{package.name}}-pdo_sqlite',
                  '${{package.name}}-mbstring', 'curl']
PROBE_PHP, PROBE_SH = 'WEREWOLF_PROBE_PHP', 'WEREWOLF_PROBE_SH'
PROBE_CAPABILITIES = 'WEREWOLF_PROBE_CAPABILITIES'
# Capability groups granted in every test environment so Wolfi's own tests
# (and the subpackages') run `php`, scripts and unserialize; the probe clears
# them and proves deny-by-default. PHP has no `environment` group.
GROUPS = ['exec', 'native', 'deserialize', 'inline', 'writable-source', 'upload', 'upload-progress',
          'namespace', 'process', 'listen', 'special-files', 'debug']

def grant_all():
    return (runtime.build_root() + 'mkdir -p /etc/runtime-hardening/allow/all/php\n'
            'for f in ' + ' '.join(GROUPS) + '; do\n'
            '  : > /etc/runtime-hardening/allow/all/php/$f\n'
            'done\n')

def source_commit(config):
    source = config['pipeline'][0]
    return source.get('with', {}).get('expected-commit') if source.get('uses') == 'git-checkout' else None

def epoch(wolfi_epoch, revision):
    """Our epoch: above every Wolfi rebuild of the same PHP release, so apk
    prefers ours, and moving when either Wolfi's epoch or our revision does."""
    if not 0 <= wolfi_epoch < 1000:
        raise ValueError('Review Wolfi epoch ' + str(wolfi_epoch) + ': ours would no longer sort above it')
    return 1000 * revision + wolfi_epoch

def full_version(lock):
    return lock['version'] + '-r' + str(epoch(lock['epoch'], lock['revision']))

def context(pkg, destination):
    # Beside Wolfi's files, under a name php-src does not use at its top.
    shutil.copytree(pkg.dir / 'patches', destination / PATCHES)

def configure_without_omissions(script):
    if re.search(r'--(?:enable|disable)-cgi\b', script):
        raise ValueError('Review changed upstream CGI configuration')
    for flag in list(REMOVED.values()) + list(OMITTED_EXTENSIONS.values()) + list(OMITTED_LIBRARIES):
        line = re.compile(r'^[ \t]*' + re.escape(flag) + r'[ \t]*\\\n', re.M)
        # Phar is enabled by default; removing its shared-build flag alone
        # would build it into the main executable instead.
        replacement = (lambda match: '  --disable-phar \\\n') if flag == OMITTED_EXTENSIONS['phar'] else ''
        script, count = line.subn(replacement, script)
        if count != 1:
            raise ValueError('Review changed upstream configure flag ' + flag + ' before deriving')
    # CGI is enabled by default upstream, independently of FPM.
    script, count = re.subn(r'(^[ \t]*--enable-cli[ \t]*\\\n)',
                            r'\g<1>  --disable-cgi \\\n', script, flags=re.M)
    if count != 1:
        raise ValueError('Review changed upstream CLI configuration')
    return script

def retained_subpackages(config):
    subs = config['subpackages']
    if sorted(s['name'] for s in subs) != sorted(KEPT_SUBPACKAGES + OMITTED_SUBPACKAGES):
        raise ValueError('Review changed upstream subpackages before deriving')
    kept = [s for s in subs if s['name'] not in OMITTED_SUBPACKAGES]
    for section in [config['package']] + kept:
        if any(name.split('=')[0] in OMITTED_SUBPACKAGES
               for name in section.get('dependencies', {}).get('runtime', [])):
            raise ValueError('Review ' + section['name'] + ': it depends on a package this build leaves out')
    return kept

def retained_extensions(config):
    ranges = [item for item in config['data'] if item['name'] == 'extensions']
    if len(ranges) != 1 or set(ranges[0]['items']) != KEPT_EXTENSIONS | set(OMITTED_EXTENSIONS):
        raise ValueError('Review changed upstream extensions before deriving')
    return {key: value for key, value in ranges[0]['items'].items() if key in KEPT_EXTENSIONS}

def prune():
    return {'name': PRUNE, 'runs': 'rm -R "${{targets.destdir}}/usr/share/man"\n'}

def retained_tests(config):
    test = deepcopy(config['test'])
    contents = test['environment']['contents']['packages']
    tidy = '${{package.name}}-tidy'
    if contents.count(tidy) != 1 or sum(s.get('uses') == 'test/php-tidy' for s in test['pipeline']) != 1:
        raise ValueError('Review changed upstream Tidy test before omitting the extension')
    contents.remove(tidy)
    test['pipeline'] = [s for s in test['pipeline'] if s.get('uses') != 'test/php-tidy']
    return test

def probe(pkg):
    files = {'probe.php': PROBE_PHP, 'probe.sh': PROBE_SH, 'capabilities.php': PROBE_CAPABILITIES}
    script = runtime.root_probe('php', ['php', '-v'])
    for name, delimiter in files.items():
        text = (pkg.dir / 'tests' / name).read_text()
        if '${{' in text or any(marker in text for marker in files.values()):
            raise ValueError('tests/probe.* cannot be embedded in a Melange recipe')
        script += "cat > /tmp/" + name + " <<'" + delimiter + "'\n" + text + delimiter + '\n'
    return script + 'sh /tmp/probe.sh /tmp/probe.php\n'

def derive(pkg, config, lock):
    original = config['package']['name']
    if original != Path(lock['recipe']).stem or str(config['package']['version']) != lock['version']:
        raise ValueError('Review changed upstream package identity before deriving')
    pipeline = config['pipeline']
    source = pipeline[0]
    assert source_commit(config) == lock['source_commit'], 'source pin differs from the lock'
    if source != {'uses': 'git-checkout', 'with': {'repository': UPSTREAM, 'tag': 'php-${{package.version}}',
                                                   'expected-commit': lock['source_commit']}}:
        raise ValueError('Review changed upstream source checkout before deriving')
    pipeline[0] = fetch(lock['source_commit'], SOURCES)
    # Wolfi's patches first, as Wolfi applies them; then ours.
    patched = [i for i, step in enumerate(pipeline) if step.get('uses') == 'patch']
    if not patched or patched != list(range(1, patched[-1] + 1)):
        raise ValueError('Review changed upstream patch steps before deriving')
    pipeline.insert(patched[-1] + 1, patching.step(PATCHES))
    # The build runs the new php on build-tree scripts, so the build
    # guest grants every group; packages hold nothing under /etc/runtime-hardening.
    pipeline.insert(patched[-1] + 2, {'name': 'Allow every group in the build guest', 'runs': grant_all()})
    configure = [step for step in pipeline if step.get('name') == 'Configure']
    if len(configure) != 1:
        raise ValueError('Review changed upstream configure step before deriving')
    configure[0]['runs'] = configure_without_omissions(configure[0]['runs'])
    for name in APXS_STEPS:
        if sum(s.get('name') == name for s in pipeline) != 1:
            raise ValueError('Review changed upstream Apache install steps')
    pipeline[:] = [s for s in pipeline if s.get('name') not in APXS_STEPS]
    pipeline.append(prune())
    wolfi = str(config['package']['version']) + '-r' + str(config['package']['epoch'])
    config['package']['epoch'] = epoch(config['package']['epoch'], lock['revision'])
    config['package']['description'] += ' (Werewolf fixed hardening policy)'
    kept = retained_subpackages(config)
    config['subpackages'] = kept
    extensions = retained_extensions(config)
    next(item for item in config['data'] if item['name'] == 'extensions')['items'] = extensions
    # Each package names the Wolfi build it rebuilds: werewolf matches Wolfi's
    # CVE fixes against that (lib/prebuilt.zig), not against our epoch.
    for section in [config['package']] + kept:
        section.setdefault('dependencies', {}).setdefault('provides', []).append(
            'wolfi-origin:' + original + ':' + wolfi)
    test = config['test'] = retained_tests(config)
    test.setdefault('environment', {}).setdefault('contents', {}).setdefault('packages', []).extend(PROBE_PACKAGES)
    test['pipeline'].append({'name': 'Werewolf protections hold, and allowances take each back',
                             'needs': {'packages': ['build-base']},
                             'runs': probe(pkg)})
    config['update'] = {'enabled': False}
    return config

def test_recipe(config, repository):
    # Grant every group in every test environment (main and subpackages) so
    # Wolfi's own tests run; the probe (in the main test) clears them and proves
    # deny-by-default. Done only when tests run, not in the derived recipe.
    grant = {'name': 'Grant every group for the suite (the probe clears them)', 'runs': grant_all()}
    for section in [config] + config.get('subpackages', []):
        test = section.get('test')
        if test and test.get('pipeline'):
            test['pipeline'].insert(0, dict(grant))
    return config

def check(pkg, upstream, derived):
    """Keep Wolfi's FPM/CLI build and used extensions, with documented omissions."""
    configure = lambda c: next(x['runs'] for x in c['pipeline'] if x.get('name') == 'Configure')
    assert configure(derived) == configure_without_omissions(configure(upstream)), 'upstream configure flags changed'
    assert derived['environment'] == upstream['environment'], 'upstream build environment changed'
    patches = lambda c: [x for x in c['pipeline'] if x.get('uses') == 'patch']
    assert patches(upstream) == patches(derived), "Wolfi's patches changed"
    ours = next(i for i, x in enumerate(derived['pipeline']) if x.get('name') == 'Apply Werewolf policy patches')
    assert derived['pipeline'][ours - 1] == patches(upstream)[-1], "our patches must follow Wolfi's"
    assert derived['pipeline'][-1] == prune(), 'manpages must be removed before splitting'
    assert sum(s.get('name') == PRUNE for s in derived['pipeline']) == 1, 'one manpage cleanup step'
    assert not any(s.get('name') in APXS_STEPS for s in derived['pipeline']), 'Apache install steps remain'
    steps = lambda c, omitted: [x for x in c['pipeline'][1:] if x.get('name') not in omitted]
    ours_only = ('Apply Werewolf policy patches', 'Configure', 'Allow every group in the build guest', PRUNE)
    assert steps(upstream, ('Configure',) + APXS_STEPS) == steps(derived, ours_only), 'an upstream build step changed'
    strip = lambda subs: [{**s, 'dependencies': {**s.get('dependencies', {}), 'provides': [
        p for p in s.get('dependencies', {}).get('provides', []) if not p.startswith('wolfi-origin:')]}}
        for s in subs]
    assert strip(retained_subpackages(upstream)) == strip(derived['subpackages']), 'an upstream subpackage changed'
    expected_data = [{**item, 'items': retained_extensions(upstream)} if item['name'] == 'extensions' else item
                     for item in upstream['data']]
    assert derived['data'] == expected_data, 'upstream extension selection changed'
    expected_test = retained_tests(upstream)
    expected_test['environment']['contents']['packages'].extend(PROBE_PACKAGES)
    assert derived['test']['environment'] == expected_test['environment'], 'upstream test environment changed'
    assert derived['test']['pipeline'][:-1] == expected_test['pipeline'], "a retained package test changed"
