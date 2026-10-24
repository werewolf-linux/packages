"""Verify common identity-provider flows with the optimized feature selection."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


def request(url, data=None, token=None, form=False):
    headers = {}
    if data is not None:
        headers['Content-Type'] = 'application/x-www-form-urlencoded' if form else 'application/json'
        data = (urlencode(data) if form else json.dumps(data)).encode()
    if token:
        headers['Authorization'] = 'Bearer ' + token
    with urlopen(Request(url, data=data, headers=headers), timeout=5) as response:
        body = response.read()
        return json.loads(body) if body and 'json' in response.headers.get('Content-Type', '') else body


with tempfile.TemporaryDirectory() as directory:
    root = Path(directory)
    home = root / 'keycloak'
    home.mkdir()
    for name in ('conf', 'lib', 'providers', 'themes'):
        (home / name).symlink_to(Path('/usr/lib/keycloak') / name)
    (home / 'data').mkdir()
    args = root / 'java.args'
    args.write_text(Path('/usr/lib/keycloak/java.args').read_text().replace(
        '-Dkc.home.dir=/usr/lib/keycloak', '-Dkc.home.dir=' + str(home)))
    password = root / 'password'
    password.write_text('consumer-db-password')
    subprocess.run(['initdb', '-D', str(root / 'pg'), '-U', 'keycloak',
                    '--pwfile=' + str(password), '--auth-local=trust', '--auth-host=scram-sha-256'], check=True)
    subprocess.run(['pg_ctl', '-D', str(root / 'pg'), '-l', str(root / 'pg.log'),
                    '-o', '-h 127.0.0.1 -k ' + directory, '-w', 'start'], check=True)
    process = None
    try:
        env = dict(os.environ, KC_DB_URL='jdbc:postgresql://127.0.0.1:5432/postgres',
                   KC_DB_USERNAME='keycloak', KC_DB_PASSWORD='consumer-db-password',
                   KC_HTTP_ENABLED='true', KC_HTTP_PORT='18080', KC_HOSTNAME='http://localhost:18080',
                   KC_BOOTSTRAP_ADMIN_USERNAME='admin', KC_BOOTSTRAP_ADMIN_PASSWORD='consumer-admin-password')
        with (root / 'keycloak.log').open('w') as log:
            process = subprocess.Popen(['java', '-Xmx768m', '@' + str(args), 'start', '--optimized'],
                                       env=env, stdout=log, stderr=log)
            base = 'http://127.0.0.1:18080'
            deadline = time.monotonic() + 120
            while True:
                try:
                    request(base + '/realms/master/.well-known/openid-configuration')
                    break
                except (URLError, TimeoutError):
                    if process.poll() is not None or time.monotonic() > deadline:
                        raise RuntimeError((root / 'keycloak.log').read_text())
                    time.sleep(1)
            login = request(base + '/realms/master/protocol/openid-connect/token', {
                'grant_type': 'password', 'client_id': 'admin-cli', 'username': 'admin',
                'password': 'consumer-admin-password'}, form=True)
            token = login['access_token']
            request(base + '/admin/realms', {'realm': 'consumer', 'enabled': True}, token)
            request(base + '/admin/realms/consumer/clients', {
                'clientId': 'consumer', 'enabled': True, 'publicClient': False,
                'secret': 'consumer-client-secret', 'serviceAccountsEnabled': True}, token)
            realm = base + '/realms/consumer'
            discovery = request(realm + '/.well-known/openid-configuration')
            assert 'S256' in discovery['code_challenge_methods_supported']
            result = request(realm + '/protocol/openid-connect/token', {
                'grant_type': 'client_credentials', 'client_id': 'consumer',
                'client_secret': 'consumer-client-secret'}, form=True)
            assert len(result['access_token'].split('.')) == 3
            assert result['token_type'].lower() == 'bearer'
            try:
                request(realm + '/protocol/openid-connect/token', {
                    'grant_type': 'client_credentials', 'client_id': 'consumer',
                    'client_secret': 'wrong'}, form=True)
            except HTTPError as error:
                assert error.code == 401, error
            else:
                raise AssertionError('wrong client secret accepted')
            assert b'EntityDescriptor' in request(realm + '/protocol/saml/descriptor')
            print('PASS: Keycloak admin login, realm/client creation, OIDC tokens, PKCE metadata and SAML discovery')
    finally:
        if process:
            process.terminate()
            process.wait(timeout=20)
        subprocess.run(['pg_ctl', '-D', str(root / 'pg'), '-m', 'immediate', '-w', 'stop'], check=True)
