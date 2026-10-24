"""Refuse an ABI or dependency the Pi-hole Alpine tree does not provide."""
import re
import subprocess
import sys

binary = sys.argv[1]
dynamic = subprocess.check_output(['readelf', '--dynamic', binary], text=True)
needed = set(re.findall(r'\(NEEDED\).*?\[(.*?)\]', dynamic))
base = {'libssl.so.3', 'libcrypto.so.3', 'libz.so.1'}
libc = needed - base
assert base <= needed and len(libc) == 1, needed
assert re.fullmatch(r'libc\.musl-(aarch64|x86_64)\.so\.1', libc.pop()), needed
headers = subprocess.check_output(['readelf', '--program-headers', binary], text=True)
assert re.search(r'Requesting program interpreter: /lib/ld-musl-(aarch64|x86_64)\.so\.1', headers), headers
print('PASS: private curl needs only musl, OpenSSL 3 and zlib')
