#!/usr/bin/env python3
"""File what stops, or soon may stop, a new Wolfi build as a GitHub issue.

A package's scheduled check (scripts/discover.py) files one open issue per
failure, labelled drift and package/NAME, titled by Wolfi's version and what
failed, with the failing output: a recipe that cannot be captured, a recipe
the derivation no longer recognizes, a patch that no longer applies. Those
block its releases. A patch that applies only with fuzz is filed too, labelled
fuzz, without blocking: refresh it before it stops applying.

A run that fails the same way again adds nothing; the same title failing
differently adds a comment. A run that passes closes them.

usage: drift.py refresh|derivation|patches|fuzz PACKAGE LOG
       drift.py resolved PACKAGE"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import packages

LABEL = 'drift'
FUZZ = 'fuzz'
TAIL = 200          # lines of output kept: the end is where a failure is
LIMIT = 50000       # characters of output kept; GitHub refuses bodies over 65536
ANSI = re.compile(r'\x1b\[[0-9;?]*[A-Za-z]')
BLOCKING = {'refresh': 'capturing the recipe stopped',
            'derivation': 'the derivation stopped',
            'patches': 'a patch no longer applies'}

def gh(*args):
    return subprocess.run(['gh', *args], check=True, capture_output=True, text=True).stdout

def output(text):
    """The end of a step's output, as an issue quotes it."""
    lines = ANSI.sub('', text).replace('```', "'''").splitlines()[-TAIL:]
    kept = '\n'.join(lines)
    return kept[-LIMIT:] if len(kept) > LIMIT else kept

def failing_patch(log):
    found = re.search(r'^prepare: (.+?) does not apply$', log, re.M)
    return found.group(1) if found else None

def fuzzy_patches(log):
    return re.findall(r'^prepare: (.+?) applied with fuzz', log, re.M)

def labels(pkg, kind):
    return [LABEL, 'package/' + pkg.name] + ([FUZZ] if kind == 'fuzz' else [])

def run_url():
    if not os.environ.get('GITHUB_RUN_ID'):
        return None
    return '{}/{}/actions/runs/{}'.format(os.environ.get('GITHUB_SERVER_URL', 'https://github.com'),
                                          os.environ['GITHUB_REPOSITORY'], os.environ['GITHUB_RUN_ID'])

def report(pkg, kind, log, lock, run):
    """An issue's title, body and the marker that says the same report was filed."""
    quoted = output(log)
    if lock is not None:
        version = '{} {}-r{}'.format(pkg.title, lock['version'], lock['epoch'])
        facts = ('- Wolfi recipe: `{}` at [wolfi-dev/os@{}](https://github.com/wolfi-dev/os/blob/{}/{})\n'
                 '- Source: `{}`\n'.format(lock['recipe'], lock['wolfi_commit'][:12], lock['wolfi_commit'],
                                           lock['recipe'], lock['source_commit']))
    else:
        version, facts = pkg.title + ' (latest Wolfi)', '- Wolfi recipe: `{}`, not captured\n'.format(
            pkg.hooks.RECIPE + '.yaml' if pkg.hook('RECIPE') else pkg.hooks.FAMILY + '*.yaml')
    if kind == 'fuzz':
        fuzzy = fuzzy_patches(log)
        title = '{}: {} applies only with fuzz'.format(version, ', '.join(fuzzy) or 'a patch')
        lead = ('Wolfi\'s {} builds, but {} applied only with fuzz: its context no longer matches the source '
                'exactly. Refresh it against this source before a later release moves it further. The '
                'package\'s tests show the protection still holds.'.format(version, ', '.join(fuzzy) or 'a patch'))
    else:
        patch = failing_patch(log) if kind == 'patches' else None
        title = '{}: {}'.format(version, patch + ' no longer applies' if patch else BLOCKING[kind])
        lead = ('Wolfi\'s {} no longer builds with Werewolf\'s protections: {}. Werewolf publishes no {} '
                'build from this recipe until it is fixed.'.format(
                    version, patch + ' does not apply' if patch else BLOCKING[kind], pkg.title))
    body = '{}\n\n{}{}\n```\n{}\n```\n'.format(lead, facts, '- Run: ' + run + '\n' if run else '', quoted)
    return title, body, kind + ':' + hashlib.sha256((title + '\n' + quoted).encode()).hexdigest()[:16]

def open_issues(pkg, run=gh):
    run('label', 'create', LABEL, '--color', 'B60205', '--force',
        '--description', 'A Wolfi release no longer builds with these protections')
    run('label', 'create', FUZZ, '--color', 'FBCA04', '--force',
        '--description', 'A patch applies only with fuzz; refresh it')
    run('label', 'create', 'package/' + pkg.name, '--color', '1D76DB', '--force',
        '--description', 'packages/' + pkg.name)
    return json.loads(run('issue', 'list', '--label', LABEL, '--label', 'package/' + pkg.name, '--state', 'open',
                          '--json', 'number,title,labels', '--limit', '200'))

def file(pkg, kind, title, body, marker, run=gh):
    """Open the issue, comment on the open one a different failure has, or do nothing."""
    tag = '<!-- drift ' + marker + ' -->'
    issues = open_issues(pkg, run)
    with tempfile.TemporaryDirectory() as directory:
        text = Path(directory) / 'body.md'
        text.write_text(body + '\n' + tag + '\n')
        for issue in issues:
            if issue['title'] != title:
                continue
            seen = json.loads(run('issue', 'view', str(issue['number']), '--json', 'body,comments'))
            if tag in seen['body'] or any(tag in c['body'] for c in seen['comments']):
                return 'unchanged', issue['number']
            run('issue', 'comment', str(issue['number']), '--body-file', str(text))
            return 'commented', issue['number']
        args = ['issue', 'create', '--title', title, '--body-file', str(text)]
        for name in labels(pkg, kind):
            args += ['--label', name]
        created = run(*args)
    return 'opened', created.strip()

def resolve(pkg, fuzz, comment, run=gh):
    """Close the package's open blocking issues, or its fuzz issues; return their numbers."""
    closed = []
    for issue in open_issues(pkg, run):
        if (FUZZ in {label['name'] for label in issue['labels']}) == fuzz:
            run('issue', 'close', str(issue['number']), '--comment', comment)
            closed.append(issue['number'])
    return closed

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('kind', choices=sorted(BLOCKING) + ['fuzz', 'resolved'])
    parser.add_argument('package')
    parser.add_argument('log', nargs='?', type=Path)
    args = parser.parse_args()
    pkg = packages.Package(args.package)
    path = pkg.inputs / 'upstream.lock.json'
    lock = json.loads(path.read_text()) if args.kind != 'refresh' and path.is_file() else None
    if args.kind == 'resolved':
        print('drift: closed', resolve(pkg, False, 'Wolfi\'s {} {}-r{} builds with these protections again{}.'.format(
            pkg.title, lock['version'], lock['epoch'], ': ' + run_url() if run_url() else '')) or 'nothing')
        return
    if args.log is None:
        parser.error(args.kind + " needs the failed step's output")
    log = args.log.read_text(errors='replace') if args.log.is_file() else '(the step wrote no output)'
    title, body, marker = report(pkg, args.kind, log, lock, run_url())
    result, issue = file(pkg, args.kind, title, body, marker)
    print('drift:', result, issue, '-', title)

if __name__ == '__main__':
    main()
