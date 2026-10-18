#!/usr/bin/env python3
"""Select unpublished contexts; recover staged releases without rebuilding them."""
import argparse
import json
import os
import sys
import urllib.error
import urllib.request
import common
import repository


def download(path):
    request = urllib.request.Request(common.PUBLIC + path, headers={'User-Agent': 'werewolf-packages-ci/1'})
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return response.read()
    except urllib.error.HTTPError as error:
        if error.code == 404:
            return None
        raise


def state(meta):
    current = download('contexts/' + meta['context'] + '/current.json')
    if current is not None:
        record = repository.open_envelope(current)
        if record['context'] != meta['context']:
            raise ValueError('release pointer names another context')
        if record['inputs'] == meta['inputs']:
            return 'complete'
        if record['version'] == meta['version']:
            raise ValueError('published version has different inputs; bump package.epoch: ' + meta['context'])
    snapshot = download('builds/' + meta['context'] + '/' + meta['inputs'] + '/release.json')
    if snapshot is not None:
        record = repository.open_envelope(snapshot)
        if any(record[k] != meta[k] for k in ['context', 'inputs', 'names', 'version']):
            raise ValueError('staged release differs from the recipe')
        return 'resume'
    return 'build'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--offline', action='store_true', help='parse all selected recipes without network')
    parser.add_argument('--package', default='', help='one context, or every context when empty')
    args = parser.parse_args()
    build, publish, failed = [], [], []
    for name in [args.package] if args.package else common.contexts():
        try:
            meta = common.metadata(name)
            selected = 'build' if args.offline else state(meta)
            print(name + ': ' + selected, file=sys.stderr, flush=True)
            if selected == 'build':
                build.extend(dict(package=name, arch=arch) for arch in common.ARCHES)
            if selected != 'complete':
                publish.append(name)
        except Exception as error:
            failed.append(name)
            print(name + ': ' + str(error), file=sys.stderr, flush=True)
    result = dict(build=dict(include=build), publish=publish, failed=failed)
    if os.environ.get('GITHUB_OUTPUT'):
        with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
            for key, value in result.items():
                output.write(key + '=' + json.dumps(value, separators=(',', ':')) + '\n')
            output.write('has_build=' + str(bool(build)).lower() + '\n')
    print(json.dumps(result, separators=(',', ':')))
    if args.offline and failed:
        raise SystemExit(1)


if __name__ == '__main__':
    main()
