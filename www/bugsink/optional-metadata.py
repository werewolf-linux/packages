"""Record our Bugsink source patch and removed requirements in wheel metadata."""
import base64
import csv
import hashlib
from pathlib import Path
import re
import sys


def update(site):
    infos = list(site.glob('bugsink-*.dist-info'))
    if len(infos) != 1:
        raise ValueError('Expected exactly one Bugsink distribution')
    info = infos[0]
    metadata = info / 'METADATA'
    text = metadata.read_text()
    for name in ('brotli', 'symbolic'):
        text, count = re.subn(r'^Requires-Dist: ' + name + r'(?=[ ;(<>=!~\[]|$).*\n', '', text,
                             flags=re.MULTILINE | re.IGNORECASE)
        if count != 1:
            raise ValueError('Review changed Bugsink requirement: ' + name)
    metadata.write_text(text)
    record = info / 'RECORD'
    with record.open(newline='') as handle:
        rows = list(csv.reader(handle))
    changed = {'bugsink/streams.py', 'events/storage.py', 'files/minidump.py',
               'sentry/minidump.py', 'bugsink/conf_templates/docker.py.template',
               str(metadata.relative_to(site))}
    for row in rows:
        if row[0] in changed:
            data = (site / row[0]).read_bytes()
            row[1:] = ['sha256=' + base64.urlsafe_b64encode(hashlib.sha256(data).digest()).decode().rstrip('='),
                       str(len(data))]
            changed.remove(row[0])
    if changed:
        raise ValueError('Patched files missing from RECORD: ' + repr(changed))
    with record.open('w', newline='') as handle:
        csv.writer(handle).writerows(rows)


if __name__ == '__main__':
    update(Path(sys.argv[1]))
