#!/usr/bin/env python3
"""Compare raw upstream results without reclassifying or suppressing tests."""
import argparse
from collections import Counter
import gzip
import hashlib
import io
import json
from pathlib import Path
import re
import tarfile
import yaml

PREFIX = 'usr/share/glibc-testresults/'

def read(path):
    raw = path.read_bytes()
    with tarfile.open(fileobj=io.BytesIO(gzip.decompress(raw)), mode='r:', ignore_zeros=True) as archive:
        config = yaml.safe_load(archive.extractfile('.melange.yaml').read())
        properties = dict(line.split(' = ', 1) for line in
            archive.extractfile('.PKGINFO').read().decode().splitlines() if ' = ' in line)
        summary = archive.extractfile(PREFIX+'tests.sum').read().decode()
        results = dict((name, status) for status, name in
            re.findall(r'^(PASS|FAIL|UNSUPPORTED|XFAIL|XPASS): (.+)$', summary, re.M))
        outputs = {}
        for name, status in results.items():
            if status != 'FAIL':
                continue
            outputs[name] = {}
            for extension in ['.out', '.test-result']:
                try:
                    outputs[name][extension] = archive.extractfile(PREFIX+name+extension).read().decode(errors='replace')
                except KeyError:
                    pass
    return {'sha256': hashlib.sha256(raw).hexdigest(), 'source_commit': config['vars']['glibc-expected-commit'],
        'arch': properties['arch'],
        'counts': dict(Counter(results.values())), 'results': results, 'failures': outputs}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('baseline', type=Path)
    parser.add_argument('candidate', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    baseline, candidate = read(args.baseline), read(args.candidate)
    assert baseline['source_commit'] == candidate['source_commit'], 'glibc source commits differ'
    assert baseline['arch'] == candidate['arch'], 'architectures differ'
    a, b = baseline['results'], candidate['results']
    changed = {name: {'baseline': a.get(name), 'candidate': b.get(name)}
        for name in sorted(a.keys() | b.keys()) if a.get(name) != b.get(name)}
    new_failures = {name: candidate['failures'][name] for name in sorted(b)
        if b[name] == 'FAIL' and a.get(name) != 'FAIL'}
    report = {'source_commit': baseline['source_commit'], 'baseline': baseline, 'candidate': candidate,
        'changed': changed, 'new_failures': new_failures}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2)+'\n')
    print('Baseline:', baseline['counts'])
    print('Candidate:', candidate['counts'])
    print('New failures:', len(new_failures), dict(Counter(name.split('/')[0] for name in new_failures)))
    print('Raw statuses and logs retained:', args.output)

if __name__ == '__main__':
    main()
