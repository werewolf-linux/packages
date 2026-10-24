#!/usr/bin/env python3
"""Check a prior native build before resuming its release with a repaired publisher."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def checked_run(run, package, record, jobs):
    repo = os.environ['GITHUB_REPOSITORY']
    if not re.fullmatch(r'[1-9][0-9]*', run):
        raise ValueError('Expected a numeric source workflow run')
    if (str(record['id']) != run or record['repository']['full_name'] != repo or
            record['head_repository']['full_name'] != repo or
            record['head_branch'] != 'main' or
            record['event'] not in ('push', 'schedule', 'workflow_dispatch') or
            record['path'] != '.github/workflows/wolfi-refresh.yml' or
            not re.fullmatch(r'[0-9a-f]{40}', record['head_sha'])):
        raise ValueError('Source run is not this repository\'s runtime publication workflow')
    expected = {'x86_64', 'aarch64'}
    found = set()
    for job in jobs:
        match = re.fullmatch(r'build \(' + re.escape(package) + r'\) / build \((x86_64|aarch64), [^)]+\)', job['name'])
        if match:
            if job['conclusion'] != 'success' or match[1] in found:
                raise ValueError('Both native builds must have passed exactly once')
            found.add(match[1])
    if found != expected:
        raise ValueError('Both successful native builds are required')
    return record['head_sha'], int(record['run_number'])


def main():
    if sys.argv[1] == '--verify-tag':
        package, sha = sys.argv[2:]
        if not re.fullmatch(r'[a-z][a-z0-9-]*', package) or not re.fullmatch(r'[0-9a-f]{40}', sha):
            raise ValueError('Invalid package or source commit')
        lock = json.loads((Path('build') / package / 'inputs/upstream.lock.json').read_text())
        tag = package + '/build-' + lock['build_id']
        ref = json.loads(subprocess.check_output(['gh', 'api', 'repos/' + os.environ['GITHUB_REPOSITORY'] + '/git/ref/tags/' + tag], text=True))
        if ref['object']['type'] != 'commit' or ref['object']['sha'] != sha:
            raise ValueError(f"Release tag {tag} points to {ref['object']['type']} {ref['object']['sha']}; "
                             f'expected validated build commit {sha}')
        print(tag + ': verified at ' + sha)
        return
    run, package = sys.argv[1:]
    if not re.fullmatch(r'[1-9][0-9]*', run):
        raise ValueError('Expected a numeric source workflow run')
    if not re.fullmatch(r'[a-z][a-z0-9-]*', package):
        raise ValueError('Invalid package name')
    repo = os.environ['GITHUB_REPOSITORY']
    def api(path, *args):
        return json.loads(subprocess.check_output(['gh', 'api', *args, 'repos/' + repo + '/' + path], text=True))
    record = api('actions/runs/' + run)
    # Attempt 1 contains the original build jobs even after a publish-only retry.
    pages = api('actions/runs/' + run + '/attempts/1/jobs?per_page=100', '--paginate', '--slurp')
    sha, number = checked_run(run, package, record, [j for p in pages for j in p['jobs']])
    with Path(os.environ['GITHUB_OUTPUT']).open('a') as output:
        output.write('source_sha=' + sha + '\nsource_run_number=' + str(number) + '\n')
    print(package + ': both native builds passed at ' + sha)


if __name__ == '__main__':
    main()
