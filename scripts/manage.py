#!/usr/bin/env python3
"""The common package interface; build engines remain responsible for their gates."""
import argparse
import os
from pathlib import Path
import subprocess
import sys
import catalog


def run(*args, cwd=catalog.ROOT):
    subprocess.run(list(map(str, args)), cwd=cwd, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['build', 'check', 'updates', 'refresh', 'discover', 'verify', 'derive', 'prepare', 'test-wolfi'])
    parser.add_argument('--package', default='')
    parser.add_argument('--arch', default=os.uname().machine.replace('arm64', 'aarch64'))
    parser.add_argument('--werewolf', type=Path, default=catalog.ROOT.parent / 'werewolf')
    parser.add_argument('--wolfi', type=Path)
    args = parser.parse_args()
    packages = catalog.load()
    if args.package:
        if args.package not in packages:
            parser.error('unknown package: ' + args.package)
        selected = {args.package: packages[args.package]}
    else:
        selected = packages
    if args.command == 'check':
        run('go', 'run', './cmd/package-update', '--validate')
        for name, meta in selected.items():
            if meta['engine'] == 'upstream':
                recipe = next((catalog.ROOT / meta['path']).glob('*.yaml'))
                run(os.environ.get('MELANGE', 'melange'), 'query', recipe, '{{.Package.Name}}-{{.Package.Version}}-r{{.Package.Epoch}}{{println}}')
        return
    if args.command == 'test-wolfi':
        run(sys.executable, '-m', 'unittest', 'discover', '-s', 'tests', '-p', 'test_*.py', cwd=catalog.ROOT / 'wolfi')
        for meta in selected.values():
            if meta['engine'] == 'wolfi':
                tests = catalog.ROOT / meta['path'] / 'tests'
                if any(tests.glob('test_*.py')):
                    run(sys.executable, '-m', 'unittest', 'discover', '-s', tests, '-p', 'test_*.py', cwd=catalog.ROOT / 'wolfi')
        return
    if not args.package and args.command in ('updates', 'refresh', 'discover'):
        if args.command == 'updates':
            run('go', 'run', './cmd/package-update')
        command = 'refresh' if args.command == 'updates' else args.command
        argv = [sys.executable, 'scripts/' + command + '.py']
        if command == 'discover':
            argv.append('--local')
        if args.wolfi:
            argv += ['--wolfi', str(args.wolfi.resolve())]
        run(*argv, cwd=catalog.ROOT / 'wolfi')
        return
    if args.command == 'build' and not args.package:
        parser.error('build needs --package NAME')
    for name, meta in selected.items():
        if meta['engine'] == 'wolfi':
            if args.command in ('build', 'verify', 'derive', 'prepare'):
                argv = ['make', 'PYTHON=' + sys.executable, 'PACKAGE=' + name, 'ARCH=' + args.arch]
                if args.wolfi:
                    argv.append('WOLFI=' + str(args.wolfi.resolve()))
                run(*argv, args.command, cwd=catalog.ROOT / 'wolfi')
            else:
                command = 'refresh' if args.command == 'updates' else args.command
                argv = [sys.executable, 'scripts/' + command + '.py']
                if command == 'discover':
                    argv.append('--local')
                if args.wolfi:
                    argv += ['--wolfi', str(args.wolfi.resolve())]
                run(*argv, name, cwd=catalog.ROOT / 'wolfi')
        elif args.command == 'build':
            recipe = next((catalog.ROOT / meta['path']).glob('*.yaml'))
            run('make', '-C', args.werewolf, 'howl')
            run(args.werewolf / 'build/host/howl', 'build-apk', recipe, '--arch', args.arch, cwd=args.werewolf)
        elif args.command == 'updates':
            run('go', 'run', './cmd/package-update', '--package', name)
        elif args.package:
            parser.error(args.command + ' applies to Wolfi-derived packages')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error))
