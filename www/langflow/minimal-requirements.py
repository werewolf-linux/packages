"""Drop optional tracing integrations and development helpers from the hashed lock."""
from pathlib import Path
import re
import sys

source, target = map(Path, sys.argv[1:])
omit = {'cassandra-driver', 'duckdb', 'ibm-db', 'oracledb', 'arize-phoenix-otel', 'langfuse', 'openlayer', 'opik', 'traceloop-sdk',
        'uvloop', 'pyright', 'nodeenv', 'pytest', 'pytest-asyncio',
        'pytest-json-report', 'pytest-metadata'}
# Chroma's import path still needs the OTel API, SDK and gRPC exporter.
# Keep that interface until upstream makes it optional; remove instrumentation.
omit_prefixes = ('opentelemetry-instrumentation',)
blocks = re.split(r'(?=^[A-Za-z0-9][A-Za-z0-9_.-]*==)', source.read_text(), flags=re.MULTILINE)
seen = set()
kept = []
for block in blocks:
    name = block.split('==', 1)[0]
    if name in omit or name.startswith(omit_prefixes):
        seen.add(name)
    else:
        kept.append(block)
target.write_text(''.join(kept))
print('Omitted optional packages:', ', '.join(sorted(seen)))
