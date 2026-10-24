#!/usr/bin/env python3
"""Release notes for a validated build: what it is, what changed, and its evidence.

The shared notes, for a package without its own (package.py's notes). Pure
functions of facts that scripts/publish.py gathers; nothing here reads the
network or the disk, so a release's notes are made once and kept."""
import json
import re

FORMAT = 'hardened-packages-release-notes/1'
SOURCE = 'https://github.com/werewolf-linux/packages'
SECDB = 'https://packages.wolfi.dev/os/security.json'
WOLFI = 'https://github.com/wolfi-dev/os'

def encode(value):
    return (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()

# apk orders a version's suffix: a release sorts below its branch snapshots
# and patch levels (3.15.0 < 3.15.0_git20261010 < 3.15.0_p1).
SUFFIXES = {'': 0, 'git': 1, 'p': 2}

def version_key(version):
    """apk's order for the versions a package carries (8.5.11-r3, 3.15.0_git20261010-r0), or None."""
    match = re.fullmatch(r'(\d+(?:\.\d+)*)(?:_(git|p)(\d+))?-r(\d+)', version)
    if not match:
        return None
    return (tuple(int(x) for x in match.group(1).split('.')), SUFFIXES[match.group(2) or ''],
            int(match.group(3) or 0), int(match.group(4)))

def security(secdb, stream, previous, current):
    """Wolfi's advisories for stream fixed in (previous, current], and how many through current."""
    fixes = {}
    for package in secdb['packages']:
        if package['pkg']['name'] == stream:
            fixes = package['pkg'].get('secfixes') or {}
    top, floor = version_key(current), version_key(previous) if previous else None
    window, through = [], []
    for version, ids in sorted(fixes.items(), key=lambda item: version_key(item[0]) or ((), -1)):
        key = version_key(version)
        if key is None or key > top:
            continue
        through.append({'version': version, 'ids': sorted(ids)})
        if floor is not None and key > floor:
            window.append({'version': version, 'ids': sorted(ids)})
    return {'stream': stream, 'previous': previous, 'current': current, 'fixed': window,
            'cumulative': sum(len(x['ids']) for x in through), 'not_affected': len(fixes.get('0', []))}

def build(facts):
    """The notes, as data: facts with what changed worked out."""
    previous = facts['previous']
    made = dict(facts, format=FORMAT)
    made['first'] = previous is None
    if previous is not None:
        made['wolfi_changed'] = previous.get('wolfi_version') != facts['source']['wolfi_version']
        made['source_changed'] = previous.get('source_commit') != facts['source']['source_commit']
    return made

def commit_link(source, commit):
    upstream = source.get('upstream')
    return '[{}]({}/commit/{})'.format(commit[:12], upstream, commit) if upstream else '`' + commit[:12] + '`'

def markdown(made):
    source, previous = made['source'], made['previous']
    context = made.get('context_path', 'packages/' + made['package'])
    lines = ['# ' + made['title'] + ' ' + made['version'], '',
             "Wolfi's `{}` {} ([wolfi-dev/os@{}]({}/tree/{})), source {}, with the {} protections of "
             '[{}/patches]({}/tree/{}/{}/patches).'.format(
                 source['recipe'], source['wolfi_version'], source['wolfi_commit'][:12], WOLFI,
                 source['wolfi_commit'], commit_link(source, source['source_commit']), len(made['patches']),
                 context, SOURCE, made['commit'], context), '',
             '- Stable repository: ' + made['repository']['stable'],
             '- This build: ' + made['repository']['snapshot'],
             '- Built by: ' + made['run'], '', '## Changes', '']
    if made['first']:
        lines.append('The first release.')
    else:
        lines += ['Since ' + previous['version'] + ':', '']
        if made['wolfi_changed']:
            lines.append("- Wolfi's build: {} → {}".format(previous.get('wolfi_version') or 'unknown',
                                                            source['wolfi_version']))
        if made['source_changed'] and previous.get('source_commit'):
            upstream = source.get('upstream')
            lines.append('- Source: ' + ('[{}...{}]({}/compare/{}...{})'.format(
                previous['source_commit'][:12], source['source_commit'][:12], upstream,
                previous['source_commit'], source['source_commit']) if upstream else
                '`{}` → `{}`'.format(previous['source_commit'][:12], source['source_commit'][:12])))
        if made['commits'] is None:
            lines.append('- This repository: unavailable (GitHub did not answer)')
        else:
            for commit in made['commits']:
                lines.append('- [{}]({}/commit/{}) {}'.format(commit['sha'][:12], SOURCE, commit['sha'],
                                                               commit['subject']))
            if not made['commits']:
                lines.append('- This repository: unchanged')
    found = made['security']
    lines += ['', '## Security', '']
    if found['fixed']:
        for fix in found['fixed']:
            lines.append("- Wolfi's {}: {}".format(fix['version'], ', '.join(fix['ids'])))
    else:
        lines.append('No advisory Wolfi lists for `{}` was fixed since the previous release.'.format(found['stream']))
    lines += ['', '{} advisories fixed through {}; {} listed as not affecting this stream.'.format(
        found['cumulative'], found['current'], found['not_affected'])]
    lines += ['', '## Patches', ''] + ['- `' + name + '`' for name in made['patches']]
    lines += ['', '## Packages', '', '| Package | ' + ' | '.join(made['architectures']) + ' |',
              '| --- |' + ' ---: |' * len(made['architectures'])]
    sizes = {}
    for arch, built in made['architectures'].items():
        for package in built['packages']:
            sizes.setdefault(package['name'], {})[arch] = package['size']
    for name in sorted(sizes):
        lines.append('| ' + name + ' | ' + ' | '.join(
            '{:,}'.format(sizes[name][a]) if a in sizes[name] else '—' for a in made['architectures']) + ' |')
    return '\n'.join(lines) + '\n'
