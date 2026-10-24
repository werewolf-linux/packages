"""Select official upstream releases independently of the captured Wolfi recipe."""
from copy import deepcopy
import json
import os
import re
import shlex
import subprocess

STABLE = re.compile(r'[0-9]+(?:\.[0-9]+)*(?:_p[0-9]+)?')
STREAM_PARTS = {'python': 2, 'php': 2, 'glibc': 2, 'ffmpeg': 2}


def discover(pkg):
    metadata = pkg.dir / 'metadata.json'
    if not metadata.is_file():
        return None  # Standalone fixtures do not have a catalog.
    policy = json.loads(metadata.read_text())['update']['upstream']
    command = shlex.split(os.environ.get('UPSTREAM_DISCOVERER', 'go run ./cmd/package-update'))
    result = subprocess.run(command + ['--discover-release'], cwd=pkg.root.parent,
                            input=json.dumps(policy), text=True, capture_output=True)
    if result.returncode:
        raise ValueError('Upstream release discovery failed: ' + result.stderr.strip())
    release = json.loads(result.stdout)
    if (not STABLE.fullmatch(release.get('version', '')) or
            not re.fullmatch('[0-9a-f]{40}', release.get('commit', '')) or
            not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', release.get('repository', '')) or
            type(release.get('timestamp')) is not int or release['timestamp'] <= 0):
        raise ValueError('Invalid upstream release identity')
    return release


def key(version):
    return tuple(int(p) for p in re.findall(r'\d+', version.split('_git')[0]))


def effective(pkg, baseline, lock):
    """Return the effective recipe; upstream.yaml always remains byte-for-byte Wolfi."""
    config = deepcopy(baseline)
    if not lock.get('release'):
        return config
    old_name = config['package']['name']
    family = pkg.hook('FAMILY')
    if family:
        stream = '.'.join(lock['version'].split('.')[:STREAM_PARTS.get(pkg.name, 1)])
        new_name = family + stream
        def rename(value):
            if isinstance(value, str):
                return re.sub(re.escape(old_name) + r'(?![0-9.])', new_name, value)
            if isinstance(value, list):
                return [rename(v) for v in value]
            if isinstance(value, dict):
                return {k: rename(v) for k, v in value.items()}
            return value
        config = rename(config)
    config['package']['version'], config['package']['epoch'] = lock['version'], lock['epoch']
    checkouts = [s for s in config['pipeline'] if s.get('uses') == 'git-checkout']
    if len(checkouts) != 1:
        raise ValueError('Upstream bumps need one exact source checkout')
    source = checkouts[0]['with']
    pin = source.get('expected-commit', '')
    variable = re.fullmatch(r'\$\{\{vars\.([a-zA-Z0-9-]+)\}\}', pin)
    if variable:
        config['vars'][variable[1]] = lock['source_commit']
    else:
        source['expected-commit'] = lock['source_commit']
    # The derivation replaces the checkout with an immutable fetch; retain its
    # reviewed template shape rather than inventing a new build pipeline.
    if pkg.name == 'python':
        for transform in config.get('var-transforms', []):
            if transform.get('to') == 'upstreamversion':
                transform['match'] = r'^([0-9]+\.[0-9]+\.[0-9]+)(?:_git[0-9]+)?$'
    return config


def recipe(pkg, lock):
    import yaml
    baseline = yaml.safe_load((pkg.inputs / 'upstream.yaml').read_text())
    return effective(pkg, baseline, lock)


def baseline_version(lock):
    original = lock.get('wolfi', lock)
    return original['version'] + '-r' + str(original['epoch'])
