#!/usr/bin/env python3
"""Build and test a package's derived recipe with Melange.

Linux runs Melange in bubblewrap. Elsewhere, as on macOS, Melange boots a
QEMU guest: QEMU_KERNEL_IMAGE and QEMU_BASE_INITRAMFS if set, or else the one
this makes once, in build/guest/ARCH, from Alpine's linux-virt
(tools/guest/melange-guest.yaml) with apko. Melange's initramfs loads its
drivers as modules, which werewolf's own kernel does not have.

usage: build.py PACKAGE [--arch ARCH] [--test-only]"""
import argparse
import fcntl
import fnmatch
import functools
import gzip
import hashlib
import http.server
import json
import os
from pathlib import Path
import platform
import re
import shutil
import struct
import subprocess
import threading
import yaml
import packages

ROOT = Path(__file__).resolve().parents[1]

def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()

def unwrap(image):
    """The arm64 Image inside an EFI zboot image, which Melange's QEMU does not
    boot: "zimg" at 4, then the gzipped Image's offset and size. Others as they are."""
    if image[4:8] != b'zimg':
        return image
    offset, size = struct.unpack('<II', image[8:16])
    if offset + size > len(image):
        raise ValueError('Truncated zboot image')
    return gzip.decompress(image[offset:offset + size])

def guest(arch, env, melange):
    """The kernel and initramfs Melange's QEMU runner boots."""
    if env.get('QEMU_KERNEL_IMAGE') and env.get('QEMU_BASE_INITRAMFS'):
        return Path(env['QEMU_KERNEL_IMAGE']).resolve(), Path(env['QEMU_BASE_INITRAMFS']).resolve()
    out = ROOT / 'build/guest' / arch
    kernel, initramfs = out / 'vmlinuz', out / 'initramfs.cpio'
    if kernel.is_file() and initramfs.is_file():
        return kernel, initramfs
    work = out / 'work'
    if work.exists():
        shutil.rmtree(work)
    (work / 'root/usr').mkdir(parents=True)
    rootfs = work / 'rootfs.tar'
    subprocess.run(['apko', 'build-minirootfs', '--build-arch', arch, 'melange-guest.yaml', str(rootfs)],
                   cwd=ROOT / 'tools/guest', check=True)
    base = work / 'base.cpio'
    subprocess.run([melange, 'initramfs', '--arch', arch, '--output', str(base)], check=True, env=env)
    # The modules under usr/lib: under /lib they would replace the guest's
    # /lib link, and its loader. Their vmlinuz links to /boot, which dangles.
    subprocess.run(['bsdtar', '-xf', str(rootfs), '-C', str(work / 'root/usr'),
                    '--exclude', 'lib/modules/*/vmlinuz', 'lib/modules'], check=True)
    subprocess.run(['bsdtar', '-xf', str(rootfs), '-C', str(work / 'root'), 'boot/vmlinuz-virt'], check=True)
    (work / 'vmlinuz').write_bytes(unwrap((work / 'root/boot/vmlinuz-virt').read_bytes()))
    with (work / 'initramfs.cpio').open('wb') as output:
        output.write(base.read_bytes())
        output.flush()
        subprocess.run(['bsdtar', '--format', 'newc', '--uid', '0', '--gid', '0', '--numeric-owner',
                        '-cf', '-', 'usr/lib/modules'], cwd=work / 'root', stdout=output, check=True)
    (work / 'vmlinuz').replace(kernel)
    (work / 'initramfs.cpio').replace(initramfs)
    shutil.rmtree(work)
    return kernel, initramfs

def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('package')
    parser.add_argument('--arch', default=platform.machine())
    parser.add_argument('--test-only', action='store_true', help='rerun package tests against existing signed APKs')
    args = parser.parse_args()
    pkg = packages.Package(args.package)
    arch = {'arm64': 'aarch64', 'aarch64': 'aarch64', 'x86_64': 'x86_64'}[args.arch]
    os.chdir(ROOT)
    melange = os.environ.get('MELANGE', 'melange')
    version = json.loads((ROOT / 'tools.lock.json').read_text())['melange']['version']
    if f'v{version}' not in subprocess.check_output([melange, 'version'], text=True):
        raise SystemExit(f'Install pinned Melange v{version} with scripts/install-melange.py')
    runner = 'bubblewrap' if platform.system() == 'Linux' and shutil.which('bwrap') else 'qemu'
    scratch = ROOT / 'build/runner'
    scratch.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env['TMPDIR'] = str(scratch)
    # Both architectures stamp files and tools such as jar with the same time;
    # without it Melange uses 1970, which jar refuses.
    lock = json.loads((pkg.inputs / 'upstream.lock.json').read_text())
    env['SOURCE_DATE_EPOCH'] = str(lock['source_date_epoch'])
    manifest = {'package': pkg.name, 'runner': runner, 'arch': arch, 'melange_version': version,
                'source_date_epoch': lock['source_date_epoch']}
    if runner == 'bubblewrap':
        # A package's tests run on this kernel, which it may require be recent.
        minimum = pkg.hook('MIN_KERNEL')
        host = tuple(int(x) for x in re.match(r'(\d+)\.(\d+)', platform.release()).groups())
        if minimum and host < tuple(int(x) for x in minimum.split('.')):
            raise SystemExit(f'host kernel {platform.release()} is older than {pkg.name} minimum {minimum}')
        manifest['kernel'] = platform.release()
        executable = Path(shutil.which('bwrap')).resolve()
        manifest['bubblewrap_sha256'] = sha256(executable)
        profile = executable.with_suffix('.apparmor')
        if profile.is_file():
            manifest['apparmor_profile_sha256'] = sha256(profile)
    with (scratch / 'lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        if runner == 'qemu':
            kernel, initramfs = guest(arch, env, melange)
            env.pop('QEMU_KERNEL_MODULES', None)
            env['QEMU_KERNEL_IMAGE'], env['QEMU_BASE_INITRAMFS'] = str(kernel), str(initramfs)
            env['QEMU_DISKS_PATH'] = str(scratch)
            manifest['kernel_sha256'], manifest['guest_sha256'] = sha256(kernel), sha256(initramfs)
        pkg.packages.mkdir(parents=True, exist_ok=True)
        provenance = 'test-runner.json' if args.test_only else 'runner.json'
        (pkg.packages / provenance).write_text(json.dumps(manifest, indent=2) + '\n')
        common = ['--runner', runner, '--arch', arch, '--cpu', env.get('MELANGE_CPU', '4'),
                  '--memory', env.get('MELANGE_MEMORY', '8Gi')]
        pipelines = str(pkg.inputs / 'pipelines')
        wolfi_key = str(pkg.inputs / 'wolfi-signing.rsa.pub')
        if not args.test_only:
            subprocess.run([melange, 'build', str(pkg.recipe), *common, '--source-dir', str(pkg.context),
                            '--namespace', 'werewolf', '--pipeline-dir', pipelines,
                            '--repository-append', 'https://packages.wolfi.dev/os', '--keyring-append', wolfi_key,
                            '--signing-key', 'build/keys/werewolf-packages.rsa',
                            '--cache-dir', str(scratch / 'cache'), '--out-dir', str(pkg.packages)],
                           check=True, env=env)
        # Package tests are a separate Melange command. They install from the
        # signed local index: apko resolves it on this host, at loopback, while a
        # test's own apk reaches it from QEMU through the user-network gateway.
        # They read their files from a copy of Wolfi's recipe directory, as Wolfi's.
        test_context = pkg.build / 'test-context'
        if test_context.exists():
            shutil.rmtree(test_context)
        shutil.copytree(pkg.inputs / 'upstream', test_context)
        handler = functools.partial(http.server.SimpleHTTPRequestHandler, directory=str(pkg.packages))
        with http.server.ThreadingHTTPServer(('127.0.0.1', 0), handler) as server:
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            port = server.server_address[1]
            repository = f'http://127.0.0.1:{port}'
            inside = f'http://10.0.2.2:{port}' if runner == 'qemu' else repository
            config = yaml.safe_load(pkg.recipe.read_text())
            config = pkg.hook('test_recipe', lambda config, repository: config)(config, inside)
            recipe = pkg.build / (pkg.name + '-test.yaml')
            recipe.write_text(yaml.safe_dump(config, sort_keys=False))
            # Wolfi's shared test pipelines use apk to list what a package holds.
            # Test the captured package set even if Wolfi publishes newer
            # versions while this build runs. Pin every subpackage so each
            # test resolves the corresponding runtime dependencies as well.
            package = config['package']
            version = f"{package['version']}-r{package['epoch']}"
            # A package names, or matches by pattern, subpackages that must not be
            # in every test: two that conflict (libffi's dev and pic-dev both
            # provide pc:libffi), or files a test requires absent (glibc's
            # locales, which its main package must not hold). Each resolves alone.
            unpinned = pkg.hook('TEST_UNPINNED', ())
            candidates = [name + '=' + version for name in sorted(packages.output_names(config))
                          if not any(fnmatch.fnmatchcase(name, p) for p in unpinned)]
            extra = [arg for name in candidates + pkg.hook('TEST_PACKAGES', ['busybox', 'apk-tools'])
                     for arg in ('--test-package-append', name)]
            try:
                subprocess.run([melange, 'test', str(recipe), *common, '--source-dir', str(test_context),
                                '--pipeline-dirs', pipelines, *extra,
                                '--repository-append', repository, '--repository-append', 'https://packages.wolfi.dev/os',
                                '--keyring-append', 'build/keys/werewolf-packages.rsa.pub', '--keyring-append', wolfi_key],
                               check=True, env=env)
            finally:
                server.shutdown()
                thread.join()

if __name__ == '__main__':
    main()
