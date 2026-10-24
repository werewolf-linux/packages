#!/usr/bin/env python3
"""Derive a package's build context from its unchanged, pinned Wolfi recipe.

The package's derive hook makes the changes, and checks every step it edits
first: a recipe that changed there stops the derivation, for a person to
review, rather than being edited blind.

usage: derive.py PACKAGE"""
import argparse
import json
import re
import shutil
import yaml
import packages

BACKOFF = (0, 15, 45)

def fetch(commit, sources):
    """A step that fetches only commit, from the first of sources to answer.

    Shell and git alone, as every build guest has them: the depth-1 fetch is
    checked object by object and must be the locked commit."""
    if not re.fullmatch('[0-9a-f]{40}', commit):
        raise ValueError('Source commit must be an exact SHA-1')
    for source in sources:
        if not re.fullmatch(r'https://[A-Za-z0-9./_-]+', source):
            raise ValueError('A source is an https URL without shell syntax: ' + source)
    return {'name': 'Fetch the locked source commit', 'needs': {'packages': ['git']}, 'runs':
            'git init -q .\n'
            'for delay in ' + ' '.join(str(d) for d in BACKOFF) + '; do\n'
            '  sleep "$delay"\n'
            '  for source in ' + ' '.join(sources) + '; do\n'
            '    if git -c core.hooksPath=/dev/null -c fetch.fsckObjects=true fetch --no-tags --depth=1 '
            '"$source" ' + commit + '; then\n'
            '      test "$(git rev-parse FETCH_HEAD)" = ' + commit + '\n'
            '      git -c core.hooksPath=/dev/null checkout -q --detach ' + commit + '\n'
            '      exit 0\n'
            '    fi\n'
            '  done\n'
            'done\n'
            'echo "' + commit + ': every source failed, three times" >&2\n'
            'exit 1\n'}

def derive(pkg):
    config = yaml.safe_load((pkg.inputs / 'upstream.yaml').read_text())
    lock = json.loads((pkg.inputs / 'upstream.lock.json').read_text())
    return pkg.hooks.derive(pkg, config, lock)

def context(pkg):
    """Wolfi's recipe files, then whatever the package adds for its build."""
    if pkg.context.exists():
        shutil.rmtree(pkg.context)
    shutil.copytree(pkg.inputs / 'upstream', pkg.context)
    pkg.hooks.context(pkg, pkg.context)

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('package')
    pkg = packages.Package(parser.parse_args().package)
    context(pkg)
    pkg.recipe.write_text(yaml.safe_dump(derive(pkg), sort_keys=False, width=120))
    print(pkg.recipe)

if __name__ == '__main__':
    main()
