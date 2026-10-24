#!/usr/bin/env python3
"""Place upstream locale fixtures at libc's compiled path, without LOCPATH."""
import argparse
from pathlib import Path
import re
import shutil
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--build', type=Path, required=True)
parser.add_argument('--source', type=Path)
args = parser.parse_args()
build = args.build.resolve()
source = args.source.resolve() if args.source else build.parent
for makefile in sorted(source.glob('*/Makefile')):
    if makefile.parent.name == 'benchtests' or not re.search(r'^LOCALES\s*[:+?]?=', makefile.read_text(), re.M):
        continue
    subprocess.run(['make', '-C', str(makefile.parent), 'objdir='+str(build),
        '-f', 'Makefile', '-f', str(Path(__file__).with_name('locales.mk').resolve()),
        'rtld-prefix=', 'stop-on-test-failure=yes', '-j4', 'neutered-locale-fixtures'], check=True)
destination = Path('/usr/lib/locale')
destination.mkdir(parents=True, exist_ok=True)
count = 0
for locale in sorted((build/'localedata').iterdir()):
    if locale.is_dir() and (locale/'LC_CTYPE').is_file():
        shutil.copytree(locale, destination/locale.name, dirs_exist_ok=True)
        count += 1
assert count > 0, 'no upstream locale fixtures generated'
print(f'Installed {count} explicit locale fixtures at the compiled default path')
