"""Wolfi overlays resolved from the categorized package catalog.

Each context holds Werewolf's patches, tests, metadata.json, policy.json and a
package.py of hooks: which Wolfi recipe family it follows, where its source
lives, and how its recipe changes. Everything else is shared; README.md lists
the hooks."""
import importlib.util
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CATALOG = None
if (ROOT.parent / 'scripts/catalog.py').is_file():
    spec = importlib.util.spec_from_file_location('werewolf_catalog', ROOT.parent / 'scripts/catalog.py')
    CATALOG = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(CATALOG)

def locations(root):
    if root == ROOT and (root.parent / 'scripts/catalog.py').is_file():
        return {n: root.parent / m['path'] for n, m in CATALOG.load(root.parent).items() if m['engine'] == 'wolfi'}
    return {p.parent.name: p.parent for p in (root / 'packages').glob('*/package.py')}

PUBLIC = 'https://dist.werewolf-linux.org/'


def output_names(config):
    """The complete package set built from one derived recipe."""
    def expand(name):
        return name.replace('${{package.name}}', config['package']['name']).replace(
            '${{package.version}}', str(config['package']['version']))
    names = {config['package']['name']}
    ranges = {item['name']: item['items'] for item in config.get('data', [])}
    for sub in config['subpackages']:
        if sub.get('if'):
            raise ValueError('Review conditional package publication before updating')
        for key in ranges[sub['range']] if 'range' in sub else ['']:
            name = expand(sub['name']).replace('${{range.key}}', key)
            if '${{' in name:
                raise ValueError('Unresolved package name: ' + name)
            names.add(name)
    return names


def sibling(path, name):
    """Load module name from the file beside path, under a name of its own."""
    path = Path(path).with_name(name + '.py')
    spec = importlib.util.spec_from_file_location(
        'package_' + path.parent.name.replace('-', '_') + '_' + name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class Package:
    """A package's hooks, and where everything about it lives."""

    def __init__(self, name, root=ROOT):
        self.name, self.root = name, root
        self.dir = locations(root).get(name, root / 'packages' / name)
        if not (self.dir / 'package.py').is_file():
            raise ValueError('No such package: ' + name)
        self.hooks = sibling(self.dir / 'package.py', 'package')
        self.build = root / 'build' / name
        self.inputs = self.build / 'inputs'
        self.recipe = self.build / (name + '.yaml')
        self.context = self.build / 'context'
        self.source = self.build / 'source'
        self.packages = self.build / 'packages'
        self.prefix = 'apk/' + name + '/'
        self.public = PUBLIC + self.prefix

    def __repr__(self):
        return 'Package(' + repr(self.name) + ')'

    @property
    def policy(self):
        return json.loads((self.dir / 'policy.json').read_text())

    @property
    def title(self):
        return getattr(self.hooks, 'TITLE', self.name)

    def sources(self, lock):
        """Where the locked source commit can be fetched, first choice first."""
        found = self.hook('sources')
        return tuple(found(lock)) if found else self.hooks.SOURCES

    def tag(self, build_id):
        return self.name + '/build-' + build_id

    def hook(self, name, default=None):
        return getattr(self.hooks, name, default)


def names(root=ROOT):
    return sorted(locations(root))
