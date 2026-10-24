"""Record LangSmith's supported uncompressed profile in installed metadata."""
import base64
import csv
import hashlib
from pathlib import Path
import re
import sys

site = Path(sys.argv[1])
infos = list(site.glob('langsmith-*.dist-info'))
if len(infos) != 1:
    raise ValueError('Expected one LangSmith distribution')
info = infos[0]
metadata = info / 'METADATA'
source = metadata.read_text()
source, count = re.subn(r'^Requires-Dist: zstandard[^\n]*\n', '', source, flags=re.MULTILINE)
if count != 1:
    raise ValueError('Review changed LangSmith compression requirement')
metadata.write_text(source)
record = info / 'RECORD'
with record.open(newline='') as handle:
    rows = list(csv.reader(handle))
for row in rows:
    if row[0] == str(metadata.relative_to(site)):
        data = metadata.read_bytes()
        row[1:] = ['sha256=' + base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip('='), str(len(data))]
with record.open('w', newline='') as handle:
    csv.writer(handle).writerows(rows)
