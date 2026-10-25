#!/usr/bin/env python3
"""Allow and verify the private build guest's file and identity operations."""
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess

ROOT = Path(__file__).absolute().parents[1]
PROFILE = Path('/etc/apparmor.d/bwrap-userns-restrict')
CAPABILITIES = ('sys_chroot', 'setuid', 'setgid', 'chown', 'dac_override', 'sys_admin', 'net_admin')


def check_guest(executable):
    # Probe inside a read-only host mount with a private writable /tmp.
    # Account creation opens mode-000 shadow files; nginx changes cache owners.
    code = '''import os, pathlib, subprocess, tempfile
os.chroot('/')
with tempfile.TemporaryDirectory() as directory:
    path = pathlib.Path(directory) / 'permissions'
    path.write_bytes(b'probe')
    path.chmod(0)
    with path.open('r+b') as stream:
        assert stream.read() == b'probe'
    os.chown(path, 65532, 65532)
    assert path.stat().st_uid == 65532 and path.stat().st_gid == 65532
# Runtime smoke tests make a nested read-only mount, then drop capabilities.
subprocess.run(['/usr/bin/bwrap', '--unshare-user', '--unshare-pid', '--unshare-net',
                '--ro-bind', '/', '/', '--dev', '/dev', '--ro-bind', '/proc', '/proc',
                '--uid', '1000', '--gid', '1000', '--cap-drop', 'ALL', '--',
                '/usr/bin/python3', '-c',
                'import os; assert os.getuid() == 1000; '
                'assert "CapEff:\\t0000000000000000" in open("/proc/self/status").read()'],
               check=True, timeout=15)
'''
    subprocess.run([str(executable), '--ro-bind', '/', '/', '--dev', '/dev', '--proc', '/proc',
                    '--tmpfs', '/tmp', '--unshare-pid', '--cap-add', 'CAP_SYS_CHROOT',
                    '--cap-add', 'CAP_CHOWN', '--cap-add', 'CAP_DAC_OVERRIDE',
                    '--', '/usr/bin/python3', '-c', code], check=True, timeout=30)
    print('setup-runner: guest file operations and unprivileged nested runtime passed')


def profile(template, executable):
    path = str(executable.resolve())
    if not re.fullmatch(r'/[a-zA-Z0-9_./-]+', path):
        raise ValueError('Runner path must be a literal AppArmor attachment')
    name = 'werewolf_packages_bwrap_' + hashlib.sha256(path.encode()).hexdigest()[:16]
    child_name = name + '_child'
    marker = 'profile unpriv_bwrap flags='
    if template.count(marker) != 1:
        raise ValueError('Review changed Ubuntu bubblewrap child profile')
    child = template.split(marker, 1)[1]
    rules = '\n'.join(line.split('#', 1)[0] for line in child.splitlines())
    if re.search(r'\bcapability\b', rules.replace('audit deny capability,', '')):
        raise ValueError('Review unexpected child capability rules')
    replacements = [
        ('profile bwrap /usr/bin/bwrap', 'profile ' + name + ' "' + path + '"'),
        ('&bwrap//&unpriv_bwrap', '&' + name + '//&' + child_name),
        (marker, 'profile ' + child_name + ' flags='),
        ('&unpriv_bwrap', '&' + child_name),
        ('audit deny capability,', '\n    '.join('allow capability ' + cap + ',' for cap in CAPABILITIES)),
        ('include if exists <local/bwrap-userns-restrict>', ''),
        ('include if exists <local/unpriv_bwrap>', ''),
    ]
    for before, after in replacements:
        if template.count(before) != 1:
            raise ValueError('Review changed Ubuntu bubblewrap profile: ' + before)
        template = template.replace(before, after)
    if 'unconfined' in template:
        raise ValueError('Refusing an unconfined runner profile')
    return template


def main():
    if os.geteuid() != 0:
        raise ValueError('Load the CI runner profile as root')
    executable = ROOT / 'build/tools/bwrap'
    # A new attachment avoids conflicts with the old globally named profile.
    binary = executable.with_name('bwrap-private') if PROFILE.is_file() else executable
    policy = profile(PROFILE.read_text(), binary) if PROFILE.is_file() else None
    executable.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile('/usr/bin/bwrap', binary)
    binary.chmod(0o755)
    if policy is None:
        print('setup-runner: no Ubuntu bubblewrap child profile; using system confinement')
        check_guest(executable)
        return
    target = binary.with_suffix('.apparmor')
    target.write_text(policy)
    subprocess.run(['/usr/sbin/apparmor_parser', '--replace', str(target)], check=True, timeout=30)
    for alias, source in ((executable, binary), (executable.with_suffix('.apparmor'), target)):
        link = alias.with_name('.' + alias.name + '-link')
        link.unlink(missing_ok=True)
        link.symlink_to(source.name)
        link.replace(alias)
    print('setup-runner: private child capabilities: ' + ', '.join(CAPABILITIES))
    check_guest(executable)


if __name__ == '__main__':
    main()
