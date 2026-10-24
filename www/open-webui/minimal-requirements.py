"""Filter the verified upstream slim requirements; leave common chat/storage intact."""
from pathlib import Path
import re
import sys

path = Path(sys.argv[1])
omit = {'aiodns', 'starlette-compress', 'brotli', 'ldap3',
        'azure-ai-documentintelligence', 'azure-identity', 'azure-search-documents',
        'googleapis-common-protos'}
seen = set()
kept = []
for line in path.read_text().splitlines():
    name = re.split(r'[=\[ ;]', line.lower(), 1)[0]
    if name in omit or name.startswith('opentelemetry-'):
        seen.add(name)
        continue
    if name == 'httpx':
        if line != 'httpx[socks,http2,zstd,cli,brotli]==0.28.1':
            raise ValueError('Review changed HTTPX extras')
        line = 'httpx[http2]==0.28.1'
    kept.append(line)
if not omit <= seen or not any(n.startswith('opentelemetry-') for n in seen):
    raise ValueError('Review changed upstream optional dependencies: ' + repr(omit - seen))
path.write_text('\n'.join(kept) + '\n')
