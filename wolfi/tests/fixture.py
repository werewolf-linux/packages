"""A stand-in package for the shared tests: packages/fixture in a temporary root."""
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import packages

HOOKS = '''
TITLE = 'Fixture'
FAMILY = 'lang-'
SOURCES = ('https://example.invalid/lang.git',)
PATCHES = 'patches'

def source_commit(config):
    return config.get('vars', {}).get('commit')

def full_version(lock):
    return lock['version'] + '-r' + str(lock['epoch'] + lock['revision'])
'''


def package(root, hooks='', policy=None, patches=None):
    """packages/fixture under root, with hooks added to HOOKS and patches as {name: text}."""
    directory = Path(root) / 'packages/fixture'
    (directory / 'patches').mkdir(parents=True)
    (directory / 'package.py').write_text(HOOKS + hooks)
    (directory / 'policy.json').write_text(json.dumps(policy or {'revision': 1}))
    patches = patches or {}
    for name, text in patches.items():
        (directory / 'patches' / name).write_text(text)
    (directory / 'patches/series').write_text(''.join(name + '\n' for name in patches))
    return packages.Package('fixture', root=Path(root))


def change(path, before, after, context=('one', 'two', 'three')):
    """A unified diff of path replacing line before with after, amid context."""
    lines = '\n'.join(' ' + line for line in context[:2])
    return ('--- a/{0}\n+++ b/{0}\n@@ -1,4 +1,4 @@\n{1}\n-{2}\n+{3}\n {4}\n'.format(
        path, lines, before, after, context[2]))
