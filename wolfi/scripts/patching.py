"""Apply patches as Wolfi applies its own: GNU patch, fuzz 2.

Patches are kept small and anchored on what upstream rarely changes, so most
releases take them exactly. One that needs fuzz still applies, and is reported
so a person refreshes it before it stops applying (scripts/drift.py); one that
will not apply stops that package. A hunk fuzz put in the wrong place is what
each package's tests are for: they hold the built package to every protection."""
import re
import subprocess

FUZZ = 2
OPTIONS = ['-p1', '--forward', '--batch', '--fuzz=' + str(FUZZ), '--no-backup-if-mismatch']
# GNU patch says FAILED, BSD's failed; both say how much fuzz a hunk needed.
FUZZED = re.compile(r'^Hunk #(\d+) succeeded at (\d+) with fuzz (\d+)', re.M | re.I)


class PatchError(Exception):
    """A patch that will not apply, with what patch said."""


def apply(destination, patch):
    """Apply patch in destination; return its fuzzed hunks as (hunk, line, fuzz).

    A dry run first, so a patch that fails changes nothing."""
    argv = ['patch', *OPTIONS, '-d', str(destination), '-i', str(patch)]
    dry = subprocess.run(argv[:1] + ['--dry-run', '--verbose'] + argv[1:], capture_output=True, text=True)
    if dry.returncode:
        raise PatchError((dry.stdout + dry.stderr).strip())
    subprocess.run(argv, check=True, capture_output=True, text=True)
    return [(int(hunk), int(line), int(fuzz)) for hunk, line, fuzz in FUZZED.findall(dry.stdout)]


def step(directory):
    """The build's step: each patch directory/series names, applied the same way."""
    return {'name': 'Apply Werewolf policy patches', 'needs': {'packages': ['patch']}, 'runs':
            'while IFS= read -r name; do\n'
            '  patch ' + ' '.join(OPTIONS) + ' -i "' + directory + '/$name" || exit 1\n'
            'done < ' + directory + '/series\n'}
