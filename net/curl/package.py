"""Wolfi curl/libcurl restricted to HTTP(S), TLS, cookies and gzip/deflate."""
from copy import deepcopy
from pathlib import Path
from derive import fetch
import runtime

TITLE = 'curl'
RECIPE = 'curl'
SOURCES = ('https://github.com/curl/curl.git',)
UPSTREAM = 'https://github.com/curl/curl'
PATCHES = None
check_apk = runtime.check_apk
SUBPACKAGES = ('curl-static', 'curl-dev', 'curl-doc', 'libcurl-openssl4', '${{package.name}}-oci-entrypoint')
KEPT = ('curl-dev', 'libcurl-openssl4')
REMOVED_BUILD = ('brotli-dev', 'cyrus-sasl-dev', 'krb5-dev', 'nghttp2-dev',
                 'nghttp3-dev', 'ngtcp2-dev', 'openldap-dev')
UPSTREAM_OPTIONS = '''--enable-ipv6 --enable-kerberos-auth --enable-negotiate-auth --with-gssapi
--enable-unix-sockets --with-openssl --without-rustls --with-nghttp2 --with-nghttp3
--with-ngtcp2 --with-pic --enable-ldap --disable-ntlm --without-libssh2 --with-libpsl'''
COMPAT = "$([ -d /usr/lib/oldglibc ] && echo ac_cv_func_getpass_r='no' ac_cv_header_stdatomic_h='no')"
OPTIONS = '''--enable-ipv6 --enable-threaded-resolver --disable-ares --with-openssl
--without-rustls --with-zlib --with-libpsl --with-pic --disable-static
--without-brotli --without-zstd --without-nghttp2 --without-nghttp3 --without-ngtcp2
--without-quiche --without-libssh --without-libssh2 --without-gssapi --without-gsasl
--without-libidn2 --without-libuv --disable-ldap --disable-ldaps --disable-ftp
--disable-file --disable-ipfs --disable-rtsp --disable-dict --disable-telnet
--disable-tftp --disable-pop3 --disable-imap --disable-smtp --disable-gopher
--disable-mqtt --disable-websockets --disable-kerberos-auth --disable-negotiate-auth
--disable-ntlm --disable-digest-auth --disable-aws --disable-proxy --disable-doh
--disable-unix-sockets --disable-netrc --disable-alt-svc --disable-hsts
--disable-docs --disable-manual --disable-libcurl-option --without-fish-functions-dir
--without-zsh-functions-dir'''
FEATURE_TEST = 'curl --version | grep -F "ldap ldaps"\ncurl --version | grep -F "HTTP3"'
PROBE = 'HTTP works and unsupported protocols and encodings fail closed'
TEST_FLAGS = 'TFLAGS="-j$(nproc) !2095"'


def upstream_tests(step):
    result = deepcopy(step)
    if result['runs'].count(TEST_FLAGS) != 1:
        raise ValueError('Review changed curl upstream test selection')
    # netrc is deliberately compiled out; these tests require loading its credentials.
    result['runs'] = result['runs'].replace(TEST_FLAGS, 'TFLAGS="-j$(nproc) !2095 !netrc"')
    return result


def nginx_test(step):
    result = deepcopy(step)
    if result.get('uses') != 'test/daemon-check-output':
        raise ValueError('Review changed curl HTTP server test')
    setup = result['with']['setup']
    marker = 'daemon off;\n\n'
    if setup.count(marker) != 1 or setup.count('nginx -t') != 1:
        raise ValueError('Review changed curl nginx setup')
    # Bubblewrap cannot chown its temporary directory to nginx's unmapped UID.
    result['with']['setup'] = setup.replace(marker, 'daemon off;\nuser root;\n\n')
    return result


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
    pass


def prune():
    return {'name': 'Remove the wcurl shell wrapper',
            'runs': 'rm "${{targets.destdir}}/usr/bin/wcurl"\n'}


def configure(options):
    expected = (UPSTREAM_OPTIONS + ' ' + COMPAT).split()
    if options.replace('\\\n', ' ').split() != expected:
        raise ValueError('Review changed upstream curl configure options')
    return ' '.join(OPTIONS.split()) + ' ' + COMPAT


def probe(pkg):
    code = (pkg.dir / 'tests/probe.py').read_text()
    marker = 'WEREWOLF_CURL_PROBE'
    if marker in code or '${{' in code:
        raise ValueError('curl probe cannot be embedded')
    return {'name': PROBE, 'needs': {'packages': ['python-3.15', 'openssl']},
            'runs': runtime.build_root() + "cat > /tmp/curl-probe.py <<'" + marker + "'\n" +
                    code + marker + '\npython3 /tmp/curl-probe.py\n'}


def derive(pkg, config, lock):
    if (config['package']['name'] != 'curl' or lock['recipe'] != 'curl.yaml' or
            str(config['package']['version']) != lock['version']):
        raise ValueError('Review changed upstream curl identity')
    pipeline = config['pipeline']
    if [s.get('uses') for s in pipeline] != [
            'git-checkout', 'autoconf/configure', 'autoconf/make', None, 'autoconf/make-install', 'strip'] or \
            pipeline[3].get('name') != 'Run upstream test suite' or not pipeline[3].get('runs'):
        raise ValueError('Review changed upstream curl pipeline')
    if pipeline[0]['with'] != {'repository': UPSTREAM + '.git', 'tag': 'curl-${{vars.mangled-package-version}}',
                              'expected-commit': lock['source_commit']}:
        raise ValueError('Review changed upstream curl source')
    if sorted(s['name'] for s in config['subpackages']) != sorted(SUBPACKAGES):
        raise ValueError('Review changed upstream curl subpackages')
    pipeline[1]['with']['opts'] = configure(pipeline[1]['with']['opts'])
    pipeline[3] = upstream_tests(pipeline[3])
    pipeline[0] = fetch(lock['source_commit'], SOURCES)
    pipeline.append(prune())
    build = config['environment']['contents']['packages']
    for name in REMOVED_BUILD:
        if build.count(name) != 1:
            raise ValueError('Review changed curl build dependency ' + name)
        build.remove(name)
    config['subpackages'] = [s for s in config['subpackages'] if s['name'] in KEPT]
    dev = next(s for s in config['subpackages'] if s['name'] == 'curl-dev')
    for name in ('brotli-dev', 'nghttp2-dev'):
        dev['dependencies']['runtime'].remove(name)
    # The CLI and shared library keep their ABI relationship and Wolfi identity.
    wolfi = lock['version'] + '-r' + str(lock['epoch'])
    config['package']['epoch'] = epoch(lock['epoch'], lock['revision'])
    config['package']['description'] += ' (Werewolf HTTP(S)-only policy)'
    for section in [config['package']] + config['subpackages']:
        section.setdefault('dependencies', {}).setdefault('provides', []).append('wolfi-origin:curl:' + wolfi)
    config['package']['dependencies']['provides'].append('curl-hardened=${{package.full-version}}')
    config['package']['dependencies']['runtime'].append('libcurl-hardened=${{package.full-version}}')
    lib = next(s for s in config['subpackages'] if s['name'] == 'libcurl-openssl4')
    lib['dependencies']['provides'].append('libcurl-hardened=${{package.full-version}}')
    tests = config['test']['pipeline']
    features = [s for s in tests if s.get('runs', '').strip() == FEATURE_TEST]
    if len(features) != 1:
        raise ValueError('Review changed curl feature test')
    tests.remove(features[0])
    tests[3] = nginx_test(tests[3])
    tests.append(probe(pkg))
    config['update'] = {'enabled': False}
    return config


def check(pkg, upstream, derived):
    assert sorted(s['name'] for s in derived['subpackages']) == sorted(KEPT)
    expected_build = deepcopy(upstream['pipeline'][2:])
    expected_build[1] = upstream_tests(expected_build[1])
    assert derived['pipeline'][2:] == expected_build + [prune()], 'upstream build changed'
    assert derived['package']['dependencies']['runtime'] == upstream['package']['dependencies']['runtime'] + [
        'libcurl-hardened=${{package.full-version}}'], 'CLI must use the matching hardened library'
    assert derived['pipeline'][1]['with']['opts'] == configure(upstream['pipeline'][1]['with']['opts'])
    environment = deepcopy(upstream['environment'])
    environment['contents']['packages'] = [p for p in environment['contents']['packages'] if p not in REMOVED_BUILD]
    assert derived['environment'] == environment, 'unreviewed build environment change'
    assert derived['vars'] == upstream['vars'] and derived['var-transforms'] == upstream['var-transforms']
    expected = [s for s in upstream['test']['pipeline'] if s.get('runs', '').strip() != FEATURE_TEST]
    expected[3] = nginx_test(expected[3])
    assert derived['test']['pipeline'] == expected + [probe(pkg)], 'upstream test removed'
    assert derived['test']['environment'] == upstream['test']['environment']
    for sub in derived['subpackages']:
        original = deepcopy(next(s for s in upstream['subpackages'] if s['name'] == sub['name']))
        deps = original.setdefault('dependencies', {})
        if sub['name'] == 'curl-dev':
            deps['runtime'] = [p for p in deps['runtime'] if p not in ('brotli-dev', 'nghttp2-dev')]
        cleaned = deepcopy(sub)
        provides = [p for p in cleaned['dependencies'].get('provides', [])
                    if not p.startswith(('wolfi-origin:', 'libcurl-hardened='))]
        if provides:
            cleaned['dependencies']['provides'] = provides
        else:
            cleaned['dependencies'].pop('provides', None)
        assert cleaned == original, 'unreviewed subpackage change'
