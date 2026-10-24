"""Wolfi's OpenSSH without PAM, SELinux, zlib or PKCS#11 support."""
from pathlib import Path
from derive import fetch

TITLE = 'OpenSSH'
RECIPE = 'openssh'
SOURCES = ('https://github.com/openssh/openssh-portable.git',)
UPSTREAM = 'https://github.com/openssh/openssh-portable'
# The policy is build options, not a patch: patches/series is empty.
PATCHES = None
SUBPACKAGES = ('openssh-doc', 'openssh-keygen', 'openssh-keyscan', 'openssh-keysign',
               'openssh-sftp-server', 'openssh-pkcs11-helper', 'openssh-sk-helper',
               'openssh-server', 'openssh-client', 'openssh-service', 'openssh-server-config',
               'openssh-pam-config')
# PAM configuration for a server built without PAM.
REMOVED = ('openssh-pam-config',)
REMOVED_BUILD = ('libselinux-dev', 'linux-pam-dev', 'zlib-dev')
# Wolfi's options. Werewolf runs sshd with UsePAM no on a kernel without
# SELinux, so libpam, libselinux and libpcre2 are only code mapped into the
# root processes that face the network, as liblzma was (CVE-2024-3094).
# Compression's bugs (CVE-2016-10012, 10.6's cross-channel plaintext
# recovery and inflation past the packet limit) go with zlib; PKCS#11's
# agent bugs (CVE-2023-38408) with its support. OpenSSL stays: Apple's
# Secure Enclave keys are P-256 security keys, which need its ECDSA.
CONFIGURE = '''--sbindir=/usr/bin --sysconfdir=/etc/ssh --libexecdir=/usr/lib/ssh --with-pid-dir=/run
--with-mantype=doc --disable-utmp --disable-wtmp --disable-lastlog --disable-strip
--with-privsep-path=/var/empty --with-xauth=/usr/bin/xauth
--with-default-path='/usr/local/sbin:/usr/local/bin:/usr/bin:/usr/sbin:/sbin:/bin'
--with-privsep-user=sshd --with-pam --with-pam-service=sshd --with-libedit --with-selinux'''
HARDENED = '''--sbindir=/usr/bin --sysconfdir=/etc/ssh --libexecdir=/usr/lib/ssh --with-pid-dir=/run
--with-mantype=doc --disable-utmp --disable-wtmp --disable-lastlog --disable-strip
--with-privsep-path=/var/empty --with-xauth=/usr/bin/xauth
--with-default-path='/usr/local/sbin:/usr/local/bin:/usr/bin:/usr/sbin:/sbin:/bin'
--with-privsep-user=sshd --without-pam --with-libedit --without-selinux --without-zlib
--disable-pkcs11'''
PROBED = 'Programs link no PAM, SELinux or zlib, and refuse compression and PKCS#11'
PROBE = 'WEREWOLF_OPENSSH_PROBE'


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


def configure(options):
    if options.replace('\\\n', ' ').split() != CONFIGURE.split():
        raise ValueError('Review changed upstream OpenSSH configure options')
    return ' '.join(HARDENED.split())


def probe(pkg):
    script = (pkg.dir / 'tests/probe.sh').read_text()
    if '${{' in script or PROBE in script:
        raise ValueError('OpenSSH probe cannot be embedded in the recipe')
    return {'name': PROBED, 'runs': "cat > /tmp/openssh-probe.sh <<'" + PROBE + "'\n" +
            script + PROBE + '\nsh /tmp/openssh-probe.sh\n'}


def derive(pkg, config, lock):
    if (config['package']['name'] != Path(lock['recipe']).stem or
            str(config['package']['version']) != lock['version']):
        raise ValueError('Review changed upstream OpenSSH identity')
    pipeline = config['pipeline']
    if [s.get('uses') or s.get('runs', '').strip() for s in pipeline] != [
            'git-checkout', 'patch', 'autoreconf -fi', 'autoconf/configure', 'autoconf/make',
            'autoconf/make-install', 'strip']:
        raise ValueError('Review changed upstream OpenSSH pipeline')
    if pipeline[0]['with'] != {'repository': UPSTREAM, 'tag': '${{vars.tag-name}}',
                              'expected-commit': lock['source_commit']}:
        raise ValueError('Review changed upstream OpenSSH source')
    if sorted(s['name'] for s in config['subpackages']) != sorted(SUBPACKAGES):
        raise ValueError('Review changed upstream OpenSSH subpackages')
    build = config['environment']['contents']['packages']
    for name in REMOVED_BUILD:
        if build.count(name) != 1:
            raise ValueError('Review changed OpenSSH build dependency ' + name)
        build.remove(name)
    pipeline[3]['with']['opts'] = configure(pipeline[3]['with']['opts'])
    pipeline[0] = fetch(lock['source_commit'], SOURCES)
    config['subpackages'] = [s for s in config['subpackages'] if s['name'] not in REMOVED]
    # Every package keeps its Wolfi name and outranks Wolfi's build of it.
    wolfi = lock['version'] + '-r' + str(lock['epoch'])
    config['package']['epoch'] = epoch(lock['epoch'], lock['revision'])
    config['package']['description'] += ' (Werewolf: no PAM, SELinux, zlib or PKCS#11)'
    for section in [config['package']] + config['subpackages']:
        section.setdefault('dependencies', {}).setdefault('provides', []).append(
            'wolfi-origin:openssh:' + wolfi)
    config['test']['pipeline'].append(probe(pkg))
    config['update'] = {'enabled': False}
    return config


def check(pkg, upstream, derived):
    environment = {**upstream['environment'], 'contents': {
        **upstream['environment']['contents'],
        'packages': [p for p in upstream['environment']['contents']['packages'] if p not in REMOVED_BUILD]}}
    assert derived['environment'] == environment, 'unreviewed build environment change'
    assert derived['pipeline'][1:3] == upstream['pipeline'][1:3], "Wolfi's patch or autoreconf changed"
    configure_step = {**upstream['pipeline'][3], 'with': {
        **upstream['pipeline'][3]['with'], 'opts': configure(upstream['pipeline'][3]['with']['opts'])}}
    assert derived['pipeline'][3] == configure_step, 'the OpenSSH build options changed'
    assert derived['pipeline'][4:] == upstream['pipeline'][4:], 'an upstream build step changed'
    assert derived['vars'] == upstream['vars'] and derived['var-transforms'] == upstream['var-transforms']
    assert derived['test']['pipeline'][:-1] == upstream['test']['pipeline'], 'upstream tests changed'
    assert derived['test']['pipeline'][-1] == probe(pkg), 'the OpenSSH policy probe changed'
    expected = [s['name'] for s in upstream['subpackages'] if s['name'] not in REMOVED]
    assert [s['name'] for s in derived['subpackages']] == expected, 'subpackages changed'
    for sub in derived['subpackages']:
        original = next(s for s in upstream['subpackages'] if s['name'] == sub['name'])
        cleaned = {**sub, 'dependencies': {**sub['dependencies']}}
        provides = [p for p in cleaned['dependencies'].pop('provides') if not p.startswith('wolfi-origin:')]
        if provides:
            cleaned['dependencies']['provides'] = provides
        if not cleaned['dependencies']:
            del cleaned['dependencies']
        assert cleaned == original, 'unreviewed subpackage change: ' + sub['name']
