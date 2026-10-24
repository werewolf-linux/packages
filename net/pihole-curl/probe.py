"""Exercise the installed CLI/libcurl using local HTTP and verified HTTPS."""
import gzip
import http.server
import os
from pathlib import Path
import ssl
import subprocess
import sys
import tempfile
import threading


def main(binary='curl'):
    env = {k: v for k, v in os.environ.items() if k.lower() not in
           ('http_proxy', 'https_proxy', 'all_proxy', 'no_proxy', 'curl_ca_bundle', 'ssl_cert_file', 'ssl_cert_dir')}

    def curl(*args, code=0):
        p = subprocess.run([binary, '-q', '-sS', '--max-time', '10', *args], env=env, capture_output=True)
        assert p.returncode == code, (args, p.returncode, p.stderr)
        return p.stdout

    version = curl('--version').decode()
    lines = dict(line.split(': ', 1) for line in version.splitlines() if ': ' in line)
    assert set(lines['Protocols'].split()) == {'http', 'https'}, version
    features = set(lines['Features'].split())
    assert {'SSL', 'IPv6', 'libz', 'AsynchDNS'} <= features, version
    assert 'PSL' not in features, version
    assert not features & {'brotli', 'zstd', 'HTTP2', 'HTTP3', 'GSS-API', 'Kerberos', 'SPNEGO',
                           'NTLM', 'UnixSockets', 'HTTPS-proxy', 'alt-svc', 'HSTS'}, version

    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            body = b'hello'
            headers = {}
            status = 200
            if self.path == '/redirect':
                status, headers = 302, {'Location': '/hello'}
            elif self.path == '/forbidden':
                status, headers = 302, {'Location': 'file:///etc/passwd'}
            elif self.path == '/gzip':
                body, headers = gzip.compress(body), {'Content-Encoding': 'gzip'}
            elif self.path in ('/br', '/zstd'):
                headers = {'Content-Encoding': self.path[1:]}
            elif self.path == '/cookie':
                headers = {'Set-Cookie': 'session=ok; Path=/'}
            elif self.path == '/check-cookie':
                body = self.headers.get('Cookie', '').encode()
            elif self.path == '/auth':
                if self.headers.get('Authorization') != 'Basic dXNlcjpwYXNz':
                    status, headers = 401, {'WWW-Authenticate': 'Basic realm="test"'}
            elif self.path == '/etag':
                headers = {'ETag': '"one"'}
                if self.headers.get('If-None-Match') == '"one"':
                    status, body = 304, b''
            self.send_response(status)
            for k, v in headers.items():
                self.send_header(k, v)
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_POST(self):
            data = self.rfile.read(int(self.headers['Content-Length']))
            self.send_response(200)
            self.send_header('Content-Length', str(len(data)))
            self.end_headers()
            self.wfile.write(data)

        do_PUT = do_POST

    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        cert, key = root / 'cert.pem', root / 'key.pem'
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                        '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost',
                        '-keyout', str(key), '-out', str(cert)], check=True, capture_output=True)
        servers = []
        try:
            for secure in (False, True):
                server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
                if secure:
                    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
                    context.load_cert_chain(cert, key)
                    server.socket = context.wrap_socket(server.socket, server_side=True)
                thread = threading.Thread(target=server.serve_forever, daemon=True)
                thread.start()
                servers.append((server, thread))
            base = 'http://127.0.0.1:' + str(servers[0][0].server_port)
            tls = 'https://localhost:' + str(servers[1][0].server_port)
            assert curl(base + '/hello') == b'hello'
            assert curl('-L', base + '/redirect') == b'hello'
            curl('-L', base + '/forbidden', code=1)
            assert curl('--compressed', base + '/gzip') == b'hello'
            for encoding in ('br', 'zstd'):
                curl('--compressed', base + '/' + encoding, code=61)
            assert curl('-d', 'posted', base + '/post') == b'posted'
            upload = root / 'upload'
            upload.write_bytes(b'uploaded')
            assert curl('-T', str(upload), base + '/put') == b'uploaded'
            assert curl('-f', '-u', 'user:pass', base + '/auth') == b'hello'
            tag = root / 'etag'
            curl('--etag-save', str(tag), base + '/etag')
            assert curl('--etag-compare', str(tag), '-w', '%{http_code}', base + '/etag') == b'304'
            curl(tls + '/hello', code=60)
            assert curl('--cacert', str(cert), '--resolve', 'localhost:' + str(servers[1][0].server_port) + ':127.0.0.1', tls + '/hello') == b'hello'
            curl('--cacert', str(cert), tls.replace('localhost', '127.0.0.1') + '/hello', code=60)
            for scheme in ('file', 'ftp', 'ldap', 'smtp', 'sftp', 'ws'):
                curl(scheme + '://127.0.0.1/unavailable', code=1)
        finally:
            for server, thread in servers:
                server.shutdown()
                thread.join()
                server.server_close()
    print('PASS: minimal curl protocols, decoding, HTTP operations and TLS verification')


if __name__ == '__main__':
    main(sys.argv[1] if len(sys.argv) > 1 else 'curl')
