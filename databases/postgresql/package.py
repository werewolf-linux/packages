"""Latest stable PostgreSQL from Wolfi's recipe, without optional authentication."""
from copy import deepcopy
from pathlib import Path
import re
from derive import fetch

TITLE = 'PostgreSQL'
FAMILY = 'postgresql-'
SOURCES = ('https://github.com/postgres/postgres.git',)
UPSTREAM = 'https://github.com/postgres/postgres'
PATCHES = None
REMOVED_BUILD = {'krb5-dev', 'openldap-dev', 'linux-pam-dev', 'perl-dev',
                 'py3-supported-build-base-dev', 'execline-dev', 'curl'}
AUTH = {'--with-gssapi':'--without-gssapi', '--with-ldap':'--without-ldap', '--with-pam':'--without-pam'}
REMOVED_SUBPACKAGES = {'${{package.name}}-pgadmin-compat', '${{package.name}}-oci-entrypoint-base',
                      '${{package.name}}-oci-entrypoint', 'postgresql-${{vars.major-version}}-oci-entrypoint-compat',
                      '${{package.name}}-plperl', 'py${{range.key}}-${{vars.pypi-package}}-${{vars.major-version}}'}
PLPERL = 'Build PL/Perl transform modules'
KERBEROS = 'Test krb5 config parameter exists'
PROBED = 'Peer, SCRAM, TLS, JIT, extensions and backup/restore consumers'


def source_commit(config):
    steps = [s for s in config['pipeline'] if s.get('uses') == 'git-checkout']
    return steps[0]['with'].get('expected-commit') if len(steps) == 1 else None


def full_version(lock):
    if not 0 <= lock['epoch'] < 1000:
        raise ValueError('Review PostgreSQL epoch')
    return lock['version'] + '-r' + str(1000 * lock['revision'] + lock['epoch'])


def context(pkg, destination):
    pass


def upstream_patches(config):
    patches = config['pipeline'][1]['with']['patches'].split()
    if patches != ['per-version-dirs.patch', 'ssl-groups-fips-tolerant-default.patch']:
        raise ValueError('Review changed PostgreSQL patches')
    return ['ssl-groups-fips-tolerant-default.patch']


def derive(pkg, config, lock):
    original = config['package']['name']
    if not re.fullmatch(r'postgresql-[0-9]+', original) or str(config['package']['version']) != lock['version']:
        raise ValueError('Review changed PostgreSQL identity')
    steps = config['pipeline']
    if [s.get('uses', s.get('name')) for s in steps] != [
            'git-checkout', 'patch', 'autoconf/configure', 'autoconf/make',
            'autoconf/make-install', None, PLPERL, 'strip']:
        raise ValueError('Review changed PostgreSQL build pipeline')
    if source_commit(config) != lock['source_commit']:
        raise ValueError('PostgreSQL source differs from lock')
    if steps[0]['with'].get('repository') != UPSTREAM:
        raise ValueError('Review changed PostgreSQL repository')
    flags = config['environment']['environment']['CONFIGURE_FLAGS']
    for before, after in AUTH.items():
        if flags.split().count(before) != 1:
            raise ValueError('Review changed PostgreSQL authentication options')
        flags = flags.replace(before, after)
    config['environment']['environment']['CONFIGURE_FLAGS'] = flags
    build = config['environment']['contents']['packages']
    if not REMOVED_BUILD <= set(build):
        raise ValueError('Review changed PostgreSQL build dependencies')
    config['environment']['contents']['packages'] = [p for p in build if p not in REMOVED_BUILD]
    if steps[2]['with'] != {'opts':'$CONFIGURE_FLAGS --with-perl'}:
        raise ValueError('Review changed PostgreSQL configure step')
    steps[2]['with']['opts'] = '$CONFIGURE_FLAGS --without-perl --without-python --without-tcl'
    # Only one major is shipped, so use PostgreSQL's standard extension and
    # header directories; pg-gvm and PGXS then share the same installation.
    steps[1]['with']['patches'] = '\n'.join(upstream_patches(config)) + '\n'
    steps[0] = fetch(lock['source_commit'], SOURCES)
    config['pipeline'] = [s for s in steps if s.get('name') != PLPERL]
    if not REMOVED_SUBPACKAGES <= {s['name'] for s in config['subpackages']}:
        raise ValueError('Review changed PostgreSQL optional subpackages')
    config['subpackages'] = [s for s in config['subpackages'] if s['name'] not in REMOVED_SUBPACKAGES]
    config.pop('data')
    config['package']['name'] = 'postgresql'
    config['package']['epoch'] = 1000 * lock['revision'] + lock['epoch']
    runtime = config['package']['dependencies']['runtime']
    if runtime.count('ecpg=${{package.full-version}}') != 1:
        raise ValueError('Review changed PostgreSQL ECPG dependency')
    runtime.remove('ecpg=${{package.full-version}}')
    # Keep package names stable across major releases, including literal names
    # in Wolfi's tests and dependencies. The recipe baseline remains unchanged.
    def names(value):
        if isinstance(value, str):
            value = re.sub(r'\bpostgresql-[0-9]+(?=[-<=>\s]|$)', 'postgresql', value)
            return re.sub(r'\b(libpq|libecpg|ecpg)-[0-9]+(?=[-<=>\s]|$)', r'\1', value)
        if isinstance(value, list): return [names(v) for v in value]
        if isinstance(value, dict): return {k:names(v) for k,v in value.items()}
        return value
    config = names(config)
    for section in [config['package']] + config['subpackages']:
        deps = section.get('dependencies', {})
        deps.pop('provider-priority', None)
        own = section['name'].replace('${{package.name}}','postgresql')
        deps['provides'] = [p for p in deps.get('provides', []) if p != own + '=${{package.full-version}}']
        deps['provides'].append('upstream-origin:' + original + ':' + lock['version'] + '-r' + str(lock['epoch']))
        section['dependencies'] = deps
    config['test']['pipeline'] = [s for s in config['test']['pipeline'] if s.get('name') != KERBEROS]
    probe = (pkg.dir / 'tests/consumers.sh').read_text()
    marker = 'WEREWOLF_POSTGRESQL_CONSUMERS'
    if marker in probe or '${{' in probe:
        raise ValueError('PostgreSQL consumer probe cannot be embedded')
    config['test']['pipeline'].append({'name':PROBED,
        'needs':{'packages':['postgresql-dev', 'postgresql-contrib']},
        'runs':"cat > /tmp/postgresql-consumers.sh <<'" + marker + "'\n" + probe + marker + '\nsh /tmp/postgresql-consumers.sh\n'})
    config['update'] = {'enabled':False}
    return config


def check(pkg, upstream, derived):
    lock = dict(recipe=upstream['package']['name']+'.yaml', version=str(upstream['package']['version']),
                source_commit=source_commit(upstream), epoch=upstream['package']['epoch'], revision=pkg.policy['revision'])
    assert derive(pkg, deepcopy(upstream), lock) == derived, 'unreviewed PostgreSQL derivation change'


def test_recipe(config, repository):
    # Melange maps only one UID. PostgreSQL refuses to start under root, and
    # another UID cannot own files here. Server checks run in Werewolf's
    # native form test, where the real service account exists.
    pipeline = config['test']['pipeline']
    start = next(i for i, step in enumerate(pipeline) if step.get('name') == 'Test database creation')
    expected = ['Test database creation', 'Test basic read/write operations',
                'Test xml support', 'Test server can run and respond to requests',
                'Test XML xpath support', 'Test ICU collation presence',
                'Test JIT is enabled', 'Test toast compression set to lz4', PROBED]
    if [step.get('name') for step in pipeline[start:]] != expected:
        raise ValueError('Review changed PostgreSQL database lifecycle tests')
    pipeline[start:] = [pipeline[-1]]
    packages = config['test']['environment']['contents']['packages']
    config['test']['environment']['contents']['packages'] = [p for p in packages if p not in ('shadow', 'sudo-rs')]
    return config
