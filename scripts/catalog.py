#!/usr/bin/env python3
"""Discover categorized package contexts and validate their maintenance contracts."""
import argparse
import ast
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
NAME = re.compile(r'[a-z0-9][a-z0-9+_.-]*\Z')
CATEGORIES = {'databases', 'devel', 'games', 'graphics', 'lang', 'mail', 'math', 'multimedia', 'net', 'net-im', 'security', 'www'}
SOURCES = {'wolfi', 'github', 'github-tags', 'pypi', 'npm', 'minecraft', 'git', 'content', 'plex'}
UPDATE_FIELDS = {'source', 'separator', 'repo', 'project', 'prefix', 'pattern', 'series', 'branch', 'lock', 'reason', 'components', 'commit-var', 'recipe', 'family', 'hold'}


def keys(value, required, optional=()):
    if not isinstance(value, dict) or not set(required) <= value.keys() or value.keys() - set(required) - set(optional):
        raise ValueError('expected fields: ' + ', '.join(sorted(required)))


def text(value):
    return isinstance(value, str) and bool(value.strip())


def evidence(directory, paths):
    if not isinstance(paths, list) or not paths:
        raise ValueError('at least one evidence path is required')
    for value in paths:
        if not text(value):
            raise ValueError('invalid evidence path')
        path = directory / value
        if Path(value).is_absolute() or '..' in Path(value).parts or path.is_symlink() or not path.resolve().is_relative_to(directory.resolve()) or not path.is_file():
            raise ValueError('missing or unsafe evidence: ' + str(value))


def validate(directory, meta):
    keys(meta, {'update', 'hardening', 'wolfi'})
    name = directory.name
    if not isinstance(name, str) or not NAME.fullmatch(name) or directory.name != name:
        raise ValueError('name must match the package directory')
    update = meta['update']
    keys(update, {'source'}, UPDATE_FIELDS - {'source'})
    if update['source'] not in SOURCES:
        raise ValueError('at least one supported automatic update source is required')
    if bool(update.get('hold')) and (not name.endswith('-vuln-demo') or not text(update['hold'])):
        raise ValueError('only deliberately vulnerable demonstrations may hold updates')
    if name.endswith('-vuln-demo') and not text(update.get('hold')):
        raise ValueError('vulnerability demonstrations must explain their update hold')
    if update.get('separator', '') not in ('', '_'):
        raise ValueError('unsupported release version separator')
    if update['source'] in ('github', 'github-tags', 'git') and not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', update.get('repo', '')):
        raise ValueError('a GitHub repository is required')
    if update['source'] in ('npm', 'pypi') and not re.fullmatch(r'[A-Za-z0-9_.-]+', update.get('project', '')):
        raise ValueError('a registry project is required')
    if update['source'] in ('git', 'content', 'plex') and not text(update.get('reason')):
        raise ValueError('this update source needs a reason')
    if update['source'] == 'git' and not text(update.get('branch')):
        raise ValueError('snapshot tracking needs a branch')
    recipes = list(directory.glob('*.yaml'))
    hook = directory / 'package.py'
    if update['source'] == 'wolfi':
        if not hook.is_file() or recipes or not (directory / 'policy.json').is_file():
            raise ValueError('Wolfi contexts need hooks and policy, with no maintained recipe')
        if bool(update.get('recipe')) == bool(update.get('family')):
            raise ValueError('choose exactly one Wolfi recipe or family')
        assignments = {node.targets[0].id: ast.literal_eval(node.value)
                       for node in ast.parse(hook.read_text()).body
                       if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name)
                       and node.targets[0].id in ('RECIPE', 'FAMILY')}
        if assignments != {('RECIPE' if 'recipe' in update else 'FAMILY'): update.get('recipe', update.get('family'))}:
            raise ValueError('the update source differs from the derivation hook')
    elif len(recipes) != 1 or hook.exists():
        raise ValueError('upstream contexts need exactly one maintained recipe')
    if update['source'] == 'plex' and not (directory / 'update.py').is_file():
        raise ValueError('Plex tracking needs its signed repository updater')
    hardening = meta['hardening']
    keys(hardening, {'minimized', 'pledge', 'minimized_reason', 'pledge_reason', 'evidence'})
    for flag in ('minimized', 'pledge'):
        if type(hardening[flag]) is not bool or not text(hardening[flag + '_reason']):
            raise ValueError('hardening flags need booleans and explicit reasons')
    evidence(directory, hardening['evidence'])
    wolfi = meta['wolfi']
    keys(wolfi, {'relationship', 'recipes', 'changes', 'removal_condition'})
    if wolfi['relationship'] not in ('derived', 'replacement', 'independent'):
        raise ValueError('invalid Wolfi relationship')
    if (update['source'] == 'wolfi') != (wolfi['relationship'] == 'derived'):
        raise ValueError('derived packages must use the Wolfi engine')
    if not isinstance(wolfi['recipes'], list) or any(not text(v) for v in wolfi['recipes']):
        raise ValueError('Wolfi recipes must be a list of recipe names or patterns')
    if wolfi['relationship'] != 'independent' and not wolfi['recipes']:
        raise ValueError('a Wolfi baseline is required')
    if not text(wolfi['removal_condition']) or not isinstance(wolfi['changes'], list) or not wolfi['changes']:
        raise ValueError('explain the divergence and when it can be removed')
    for change in wolfi['changes']:
        keys(change, {'what', 'why', 'evidence'})
        if not text(change['what']) or not text(change['why']):
            raise ValueError('each divergence needs what and why')
        evidence(directory, change['evidence'])
    return meta


def load(root=ROOT):
    root = Path(root)
    candidates = {p.parent for pattern in ('*/*/metadata.json', '*/*/*.yaml', '*/*/package.py') for p in root.glob(pattern)
                  if p.relative_to(root).parts[0] not in {'wolfi', 'scripts', 'tests', '.github', 'build'}}
    found = {}
    for directory in sorted(candidates):
        try:
            if directory.parent.name not in CATEGORIES or directory.is_symlink() or directory.parent.is_symlink():
                raise ValueError('unsupported category or symlinked context')
            meta = validate(directory, json.loads((directory / 'metadata.json').read_text()))
            if directory.name in found:
                raise ValueError('duplicate package name')
            found[directory.name] = dict(meta, name=directory.name, engine='wolfi' if meta['update']['source'] == 'wolfi' else 'upstream', path=str(directory.relative_to(root)))
        except (ValueError, OSError, TypeError, KeyError) as error:
            raise ValueError(str(directory.relative_to(root)) + ': ' + str(error)) from error
    if not found:
        raise ValueError('no categorized package contexts')
    return found


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['validate', 'list', 'path', 'show'])
    parser.add_argument('name', nargs='?')
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--engine', choices=['upstream', 'wolfi'])
    parser.add_argument('--json', action='store_true')
    args = parser.parse_args()
    try:
        packages = load(args.root)
        selected = {n: m for n, m in packages.items() if not args.engine or m['engine'] == args.engine}
        if args.command == 'validate':
            print(f'{len(packages)} packages: update sources, hardening evidence and Wolfi divergence validated')
        elif args.command == 'list':
            print(json.dumps(list(selected)) if args.json else '\n'.join(selected))
        else:
            meta = selected[args.name]
            print(meta['path'] if args.command == 'path' else json.dumps(meta, indent=2))
    except (ValueError, KeyError) as error:
        parser.exit(1, str(error) + '\n')


if __name__ == '__main__':
    main()
