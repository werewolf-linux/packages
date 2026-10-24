"""The packages this repository rebuilds, one directory each under packages/.

A package directory holds Werewolf's patches, its tests, a policy.json and a
package.py of hooks: which Wolfi recipe family it follows, where its source
lives, and how its recipe changes. Everything else is shared; README.md lists
the hooks."""
import importlib.util
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PUBLIC = 'https://dist.werewolf-linux.org/'


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
        self.dir = root / 'packages' / name
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
    return sorted(path.parent.name for path in (root / 'packages').glob('*/package.py'))
