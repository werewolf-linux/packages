#!/usr/bin/env python3
"""Release notes for a validated build: what it is, what changed, and its evidence.

Pure functions of facts that scripts/publish.py gathers; nothing here reads
the network or the disk, so a release's notes are made once and kept."""
import html
import json
import re

FORMAT = 'glibc-build-release-notes/1'
STATUSES = ['PASS', 'FAIL', 'XFAIL', 'XPASS', 'UNSUPPORTED']
SOURCE = 'https://github.com/werewolf-linux/glibc-build'
DESIGN = 'https://github.com/werewolf-linux/werewolf/blob/main/docs/design/glibc-neutered.md'
SECDB = 'https://packages.wolfi.dev/os/security.json'
WOLFI = 'https://github.com/wolfi-dev/os'
GLIBC = 'https://sourceware.org/git/?p=glibc.git;a=commit;h='

def encode(value):
    return (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()

def version_key(version):
    """apk's order for the versions glibc carries (2.44-r8), or None."""
    match = re.fullmatch(r'(\d+(?:\.\d+)*)-r(\d+)', version)
    return (tuple(int(x) for x in match.group(1).split('.')), int(match.group(2))) if match else None

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
    return {'stream': stream, 'previous': previous, 'current': current, 'fixed': window, 'through': through,
            'cumulative': sum(len(x['ids']) for x in through), 'not_affected': len(fixes.get('0', []))}

def statuses(summary):
    """tests.sum's statuses by test name."""
    return dict((name, status) for status, name in
                re.findall(r'^(' + '|'.join(STATUSES) + r'): (.+)$', summary, re.M))

def tests(before, after, ceiling):
    """Counts, and what changed against the previous release when it is known."""
    counts = {s: sum(1 for v in after.values() if v == s) for s in STATUSES}
    ran = counts['PASS'] + counts['FAIL']
    result = {'counts': counts, 'fail_per_mille': round(1000 * counts['FAIL'] / ran, 1) if ran else None,
              'ceiling_per_mille': ceiling, 'compared': before is not None}
    if before is not None:
        changed = {n: (before.get(n), after.get(n)) for n in sorted(before.keys() | after.keys())
                   if before.get(n) != after.get(n)}
        result['new_failures'] = [n for n, (b, a) in changed.items() if a == 'FAIL']
        result['fixed'] = [n for n, (b, a) in changed.items() if b == 'FAIL' and a == 'PASS']
        other = {}
        for b, a in changed.values():
            if a != 'FAIL' and not (b == 'FAIL' and a == 'PASS'):
                key = (b or 'absent') + ' → ' + (a or 'absent')
                other[key] = other.get(key, 0) + 1
        result['other_changes'] = dict(sorted(other.items()))
    return result

def hardening(report):
    """The loader's and libc's ELF protections, as readelf showed them to the probes."""
    objects = {}
    for path, elf in sorted(report['elf'].items()):
        features = re.search(r'(?:AArch64|aarch64|x86) feature: ((?:[A-Z0-9_]+(?:, )?)+)', elf['notes'])
        abi = re.search(r'ABI: ([0-9.]+)', elf['notes'])
        loads = [line.split() for line in elf['program_headers'].splitlines() if line.split()[:1] == ['LOAD']]
        objects[path.removeprefix('elf/')] = {
            'control_flow': features.group(1).rstrip(', ') if features else 'none',
            'headers_executable': bool(loads) and 'E' in ''.join(loads[0][6:-1]),
            'relro': 'GNU_RELRO' in elf['program_headers'],
            'bind_now': 'BIND_NOW' in elf['dynamic_tags'] or 'Flags: NOW' in elf['dynamic_tags'],
            'minimum_kernel': abi.group(1) if abi else None}
    return {'compiler': report['compiler'].splitlines()[0], 'objects': objects}

def changes(before, after):
    """Files added, removed or changed between two {path: sha256} maps."""
    return [{'path': p, 'kind': 'added' if p not in before else 'removed' if p not in after else 'changed'}
            for p in sorted(before.keys() | after.keys()) if before.get(p) != after.get(p)]

def build(facts):
    """The notes for one release from facts publish.py gathers; see stage()."""
    previous = facts.get('previous')
    notes = {'format': FORMAT, 'version': facts['version'], 'date': facts['date'],
             'build_id': facts['build_id'], 'commit': facts['commit'], 'run': facts['run'],
             'source': facts['source'], 'repository': facts['repository'],
             'patches': facts['patches'], 'wolfi_inputs': facts['wolfi_inputs'],
             'previous': previous, 'security': facts['security'], 'commits': facts.get('commits'),
             'glibc_commits': facts.get('glibc_commits'),
             'wolfi_changes': None, 'architectures': {}}
    if previous is not None:
        notes['previous'] = {k: v for k, v in previous.items() if k != 'wolfi_inputs'}
        if previous.get('wolfi_inputs') is not None:
            notes['wolfi_changes'] = changes(previous['wolfi_inputs'], facts['wolfi_inputs'])
    for arch, a in sorted(facts['architectures'].items()):
        notes['architectures'][arch] = {
            'tests': tests(a.get('previous_tests'), a['tests'], facts['ceiling']),
            'hardening': hardening(a['report']), 'packages': a['packages']}
    return notes

def ids(entries):
    return ', '.join(i for e in entries for i in e['ids'])

def history(notes):
    # The recipe's history in Wolfi, as of the commit this build captured.
    return f"{WOLFI}/commits/{notes['source']['wolfi_commit']}/{notes['source']['recipe']}"

def plural(n, noun):
    return f'{n} {noun}' + ('' if n == 1 else 's')

def size(n):
    return f'{n // 1024} KiB' if n < 1 << 20 else f'{n / (1 << 20):.1f} MiB'

def summary(notes):
    """One line per change since the previous release, for both renderings."""
    previous, source, lines = notes['previous'], notes['source'], []
    if previous is None:
        return ['First release with notes; nothing to compare it with.']
    wolfi = source['wolfi_version']
    if previous.get('wolfi_version') in (None, wolfi):
        lines.append('Wolfi glibc: unchanged at ' + wolfi + '.')
    else:
        lines.append('Wolfi glibc: ' + previous['wolfi_version'] + ' → ' + wolfi + '.')
    fixed, since = notes['security']['fixed'], notes['security']['previous']
    if since is None:
        lines.append('Security: the previous Wolfi version is unknown; see the cumulative count below.')
    else:
        lines.append(('Security: Wolfi fixed ' + ids(fixed) + '.') if fixed else
                     'Security: no new Wolfi advisories since ' + since + '.')
    if notes['wolfi_changes'] is not None:
        lines.append('Wolfi recipe: ' + (plural(len(notes['wolfi_changes']), 'file') + ' changed.'
                                         if notes['wolfi_changes'] else 'unchanged.'))
    if notes['glibc_commits'] is not None:
        lines.append('glibc upstream: ' + (plural(len(notes['glibc_commits']), 'commit') + ' on its release branch.'
                                           if notes['glibc_commits'] else 'same source commit.'))
    if notes['commits'] is not None:
        lines.append('glibc-build: ' + plural(len(notes['commits']), 'commit') + ' since ' + previous['version'] + '.')
    new = {a: n['tests'].get('new_failures', []) for a, n in notes['architectures'].items()}
    lines.append('Tests: ' + ('; '.join(f"{a}: {plural(len(f), 'new failure')}" for a, f in new.items() if f)
                              if any(new.values()) else 'no new failures on any architecture.'))
    return lines

def markdown(notes):
    """GitHub's release body, and release-notes.md beside the snapshot."""
    s, src, out = notes['security'], notes['source'], []
    out += [f"glibc **{notes['version']}** for Werewolf: Wolfi's {s['stream']} **{src['wolfi_version']}** "
            f"with Werewolf's fixed policy patches ([design]({DESIGN}), [behavior changes]({SOURCE}#behavior-changes-for-applications)).", '']
    out += ['## Changes' + (' since ' + notes['previous']['version'] if notes['previous'] else ''), '']
    out += ['- ' + line for line in summary(notes)] + ['']
    out += ['## Security', '']
    if s['fixed']:
        out += ['| Fixed in Wolfi | Advisories |', '| --- | --- |']
        out += [f"| {e['version']} | {', '.join(e['ids'])} |" for e in s['fixed']]
        out += ['']
    out += [f"This build carries every fix Wolfi lists for {s['stream']} through {s['current']}: "
            f"{s['cumulative']} advisories, plus {s['not_affected']} that never applied ([source]({SECDB})).", '']
    if s['through']:
        out += ['<details><summary>Every advisory fixed through ' + s['current'] + '</summary>', '',
                '| Fixed in Wolfi | Advisories |', '| --- | --- |']
        out += [f"| {e['version']} | {', '.join(e['ids'])} |" for e in s['through']] + ['', '</details>', '']
    if notes['glibc_commits']:
        out += ['## glibc upstream commits', '']
        out += [f"- [`{c['sha'][:7]}`]({GLIBC}{c['sha']}) {c['subject']}" for c in notes['glibc_commits']] + ['']
    if notes['commits']:
        out += ['## glibc-build commits', '']
        out += [f"- [`{c['sha'][:7]}`]({SOURCE}/commit/{c['sha']}) {c['subject']}" for c in notes['commits']] + ['']
    if notes['wolfi_changes']:
        out += ['## Wolfi recipe files', '', f"Wolfi's changes to the recipe and its patches ([history]({history(notes)})):", '']
        out += [f"- {c['kind']}: `{c['path']}`" for c in notes['wolfi_changes']] + ['']
    out += ['## Tests', '', '| Architecture | ' + ' | '.join(STATUSES) + ' | Raw failure rate | New failures | Fixed |',
            '| --- |' + ' ---: |' * (len(STATUSES) + 1) + ' --- | --- |']
    for arch, a in notes['architectures'].items():
        t = a['tests']
        rate = f"{t['fail_per_mille'] / 10:.1f}% (ceiling {t['ceiling_per_mille'] / 10:.1f}%)" if t['fail_per_mille'] is not None else '—'
        new = ', '.join(f'`{n}`' for n in t['new_failures']) if t['compared'] else 'not compared'
        fixed = ', '.join(f'`{n}`' for n in t['fixed']) if t['compared'] else 'not compared'
        out.append(f"| {arch} | " + ' | '.join(str(t['counts'][x]) for x in STATUSES) +
                   f" | {rate} | {new or 'none'} | {fixed or 'none'} |")
    other = {a: n['tests'].get('other_changes') for a, n in notes['architectures'].items() if n['tests'].get('other_changes')}
    if other:
        out += ['', 'Other status changes, usually the runner\'s CPU: ' +
                '; '.join(f"{a}: " + ', '.join(f'{k} ×{v}' for k, v in c.items()) for a, c in other.items()) + '.']
    out += ['', 'Every failure is kept, unreclassified, in the `glibc-neutered-*-testresults` package.', '']
    out += ['## Hardening evidence', '', '| Architecture | Object | Control flow | Headers executable | RELRO | BIND_NOW | Minimum kernel |',
            '| --- | --- | --- | --- | --- | --- | --- |']
    for arch, a in notes['architectures'].items():
        for name, o in a['hardening']['objects'].items():
            out.append(f"| {arch} | `{name}` | {o['control_flow']} | {'yes' if o['headers_executable'] else 'no'} | "
                       f"{'yes' if o['relro'] else 'no'} | {'yes' if o['bind_now'] else 'no'} | {o['minimum_kernel'] or '—'} |")
    out += ['', 'Compilers: ' + '; '.join(f"{a}: {n['hardening']['compiler']}" for a, n in notes['architectures'].items()) + '.', '']
    out += ['## Packages', '', '| Package | Version | ' + ' | '.join(f'{a} size' for a in notes['architectures']) + ' |',
            '| --- | --- |' + ' ---: |' * len(notes['architectures'])]
    names = sorted({p['name'] for a in notes['architectures'].values() for p in a['packages']})
    for name in names:
        rows = [next((p for p in a['packages'] if p['name'] == name), None) for a in notes['architectures'].values()]
        version = next(p['version'] for p in rows if p)
        out.append(f'| `{name}` | {version} | ' + ' | '.join(size(p['size']) if p else '—' for p in rows) + ' |')
    r = notes['repository']
    out += ['', '## Install and verify', '',
            f"- Werewolf machines follow `{r['stable']}`; this build alone is `{r['snapshot']}`.",
            f"- Packages and indexes are signed with `werewolf-packages.rsa.pub` (sha256 `{r['key_sha256']}`).",
            '- Check the archives with `sha256sum -c SHA256SUMS`.', '']
    out += ['## Provenance', '',
            f"- glibc source: `{src['glibc_commit']}`, recipe `{src['recipe']}` ([history]({history(notes)})) from Wolfi [`{src['wolfi_commit'][:12]}`]({WOLFI}/commit/{src['wolfi_commit']})",
            f"- glibc-build: [`{notes['commit'][:12]}`]({SOURCE}/commit/{notes['commit']}), policy revision {src['policy_revision']}, "
            f"patches {', '.join(f'`{p}`' for p in notes['patches'])}",
            f"- Build `{notes['build_id']}`, [run]({notes['run']}), {notes['date']}"]
    return '\n'.join(out) + '\n'

def page(notes):
    """release-notes.html beside the snapshot, in Werewolf's release page style."""
    e = html.escape
    rows = lambda cells: '<tr>' + ''.join('<td>' + c + '</td>' for c in cells) + '</tr>'
    head = lambda cells: '<thead><tr>' + ''.join('<th>' + e(c) + '</th>' for c in cells) + '</tr></thead>'
    title = 'glibc ' + notes['version']
    s, src = notes['security'], notes['source']
    out = ['<!doctype html><html lang="en"><meta charset="utf-8">',
           '<meta name="viewport" content="width=device-width,initial-scale=1">',
           '<title>' + e(title) + '</title>',
           '<style>body{font:16px system-ui;max-width:80rem;margin:2rem auto;padding:0 1rem}'
           'table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:.6rem;border-bottom:1px solid #bbb}'
           'code{overflow-wrap:anywhere}a{color:#1265ac}</style>',
           '<h1>' + e(title) + '</h1>',
           '<p><a href="../../index.html">All glibc releases</a> · <a href="release-notes.json">JSON</a> · '
           '<a href="release-notes.md">Markdown</a> · <a href="SHA256SUMS">SHA256SUMS</a></p>',
           '<p>Wolfi\'s ' + e(s['stream']) + ' <b>' + e(src['wolfi_version']) + '</b> with Werewolf\'s fixed policy '
           'patches. <a href="' + DESIGN + '">Design</a>.</p>',
           '<h2>Changes' + (' since ' + e(notes['previous']['version']) if notes['previous'] else '') + '</h2><ul>']
    out += ['<li>' + e(line) + '</li>' for line in summary(notes)] + ['</ul>', '<h2>Security</h2>']
    if s['fixed']:
        out += ['<table>' + head(['Fixed in Wolfi', 'Advisories']) + '<tbody>']
        out += [rows([e(x['version']), e(', '.join(x['ids']))]) for x in s['fixed']] + ['</tbody></table>']
    out.append('<p>Every fix Wolfi lists for ' + e(s['stream']) + ' through ' + e(s['current']) + ': ' +
               str(s['cumulative']) + ' advisories, plus ' + str(s['not_affected']) +
               ' that never applied (<a href="' + SECDB + '">source</a>).</p>')
    if s['through']:
        out += ['<details><summary>Every advisory fixed through ' + e(s['current']) + '</summary><table>' +
                head(['Fixed in Wolfi', 'Advisories']) + '<tbody>']
        out += [rows([e(x['version']), e(', '.join(x['ids']))]) for x in s['through']] + ['</tbody></table></details>']
    if notes['glibc_commits']:
        out += ['<h2>glibc upstream commits</h2><ul>']
        out += ['<li><a href="' + e(GLIBC + c['sha'], quote=True) + '"><code>' + e(c['sha'][:7]) +
                '</code></a> ' + e(c['subject']) + '</li>' for c in notes['glibc_commits']] + ['</ul>']
    if notes['wolfi_changes']:
        out += ['<h2>Wolfi recipe files</h2><p>Wolfi\'s changes to the recipe and its patches (<a href="' +
                e(history(notes), quote=True) + '">history</a>):</p><ul>']
        out += ['<li>' + e(c['kind']) + ': <code>' + e(c['path']) + '</code></li>' for c in notes['wolfi_changes']] + ['</ul>']
    if notes['commits']:
        out += ['<h2>glibc-build commits</h2><ul>']
        out += ['<li><a href="' + SOURCE + '/commit/' + e(c['sha'], quote=True) + '"><code>' + e(c['sha'][:7]) +
                '</code></a> ' + e(c['subject']) + '</li>' for c in notes['commits']] + ['</ul>']
    out += ['<h2>Tests</h2><table>' + head(['Architecture'] + STATUSES + ['New failures', 'Fixed']) + '<tbody>']
    for arch, a in notes['architectures'].items():
        t = a['tests']
        listed = lambda names: e(', '.join(names) or 'none') if t['compared'] else 'not compared'
        out.append(rows([e(arch)] + [str(t['counts'][x]) for x in STATUSES] +
                        [listed(t.get('new_failures', [])), listed(t.get('fixed', []))]))
    out += ['</tbody></table>', '<h2>Hardening evidence</h2><table>' +
            head(['Architecture', 'Object', 'Control flow', 'Headers executable', 'RELRO', 'BIND_NOW', 'Minimum kernel']) + '<tbody>']
    for arch, a in notes['architectures'].items():
        for name, o in a['hardening']['objects'].items():
            out.append(rows([e(arch), '<code>' + e(name) + '</code>', e(o['control_flow'])] +
                            ['yes' if o[k] else 'no' for k in ('headers_executable', 'relro', 'bind_now')] +
                            [e(o['minimum_kernel'] or '—')]))
    r = notes['repository']
    out += ['</tbody></table>', '<h2>Install and verify</h2><ul>',
            '<li>Werewolf machines follow <code>' + e(r['stable']) + '</code>; this build alone is <code>' + e(r['snapshot']) + '</code>.</li>',
            '<li>Signed with <code>werewolf-packages.rsa.pub</code>, sha256 <code>' + e(r['key_sha256']) + '</code>.</li>',
            '</ul><h2>Provenance</h2><ul>',
            '<li>glibc source <code>' + e(src['glibc_commit']) + '</code>, recipe <code>' + e(src['recipe']) +
            '</code> from Wolfi <a href="https://github.com/wolfi-dev/os/commit/' + e(src['wolfi_commit'], quote=True) + '">' +
            e(src['wolfi_commit'][:12]) + '</a></li>',
            '<li>glibc-build <a href="' + SOURCE + '/commit/' + e(notes['commit'], quote=True) + '">' + e(notes['commit'][:12]) +
            '</a>, policy revision ' + str(src['policy_revision']) + '</li>',
            '<li>Build <code>' + e(notes['build_id']) + '</code>, <a href="' + e(notes['run'], quote=True) + '">run</a>, ' +
            e(notes['date']) + '</li></ul>']
    return '\n'.join(out + ['</html>']) + '\n'

def entry(notes):
    """This release's line in the repository's releases.json."""
    new = sum(len(a['tests'].get('new_failures', [])) for a in notes['architectures'].values())
    return {'version': notes['version'], 'date': notes['date'], 'build_id': notes['build_id'],
            'wolfi_version': notes['source']['wolfi_version'],
            'advisories_fixed': sum(len(x['ids']) for x in notes['security']['fixed']),
            'new_test_failures': new, 'notes': 'builds/' + notes['build_id'] + '/release-notes.html'}

def releases(existing, notes):
    """releases.json with this release added once, newest first."""
    kept = [r for r in existing if r['build_id'] != notes['build_id']]
    return sorted(kept + [entry(notes)], key=lambda r: version_key(r['version']) or ((), -1), reverse=True)

def index(entries):
    """index.html at the repository's root: every release, newest first."""
    e = html.escape
    out = ['<!doctype html><html lang="en"><meta charset="utf-8">',
           '<meta name="viewport" content="width=device-width,initial-scale=1">', '<title>glibc releases</title>',
           '<style>body{font:16px system-ui;max-width:80rem;margin:2rem auto;padding:0 1rem}'
           'table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:.6rem;border-bottom:1px solid #bbb}'
           'a{color:#1265ac}</style>', '<h1>glibc releases</h1>',
           '<p>Werewolf\'s glibc: Wolfi\'s recipe with fixed policy patches (<a href="' + SOURCE + '">source</a>). '
           'Machines follow this repository; each release below links its notes and snapshot. '
           '<a href="releases.json">JSON</a>.</p>',
           '<table><thead><tr><th>Release</th><th>Date</th><th>Wolfi glibc</th><th>Advisories fixed</th>'
           '<th>New test failures</th></tr></thead><tbody>']
    for r in entries:
        out.append('<tr><td><a href="' + e(r['notes'], quote=True) + '">' + e(r['version']) + '</a></td><td>' +
                   e(r['date']) + '</td><td>' + e(r['wolfi_version']) + '</td><td>' + str(r['advisories_fixed']) +
                   '</td><td>' + str(r['new_test_failures']) + '</td></tr>')
    return '\n'.join(out + ['</tbody></table>', '</html>']) + '\n'
