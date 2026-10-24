#!/usr/bin/env python3
"""Compare native Wolfi APKs serially; retain measurements and provenance."""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import random
import shlex
import statistics
import subprocess
import tarfile
import tempfile
import time
import yaml

parser = argparse.ArgumentParser(description=__doc__)
for label in ['baseline', 'candidate']:
    parser.add_argument('--'+label+'-libc', type=Path, required=True)
    parser.add_argument('--'+label+'-loader', type=Path, required=True)
parser.add_argument('--cc', default='gcc')
parser.add_argument('--rounds', type=int, default=9)
parser.add_argument('--output', type=Path, required=True)
args = parser.parse_args()
assert args.rounds >= 5, 'at least five interleaved rounds required'
assert platform.system() == 'Linux', 'run on native Linux, without user-mode emulation'
compiler = shlex.split(args.cc)
report = {'host': platform.uname()._asdict(), 'rounds': args.rounds, 'packages': {}, 'samples': {}}
report['compiler'] = subprocess.check_output(compiler+['--version'], text=True)
env = dict(os.environ)
for name in list(env):
    if name.startswith(('LD_', 'MALLOC_', 'LC_')) or name in ['GLIBC_TUNABLES', 'LANG', 'LANGUAGE']:
        del env[name]

with tempfile.TemporaryDirectory(prefix='glibc-benchmark-') as directory:
    work = Path(directory)
    binaries = {}
    sources, architectures = set(), set()
    for label in ['baseline', 'candidate']:
        root = work/label
        root.mkdir()
        report['packages'][label] = []
        for kind in ['libc', 'loader']:
            path = getattr(args, label+'_'+kind)
            raw = path.read_bytes()
            with tarfile.open(fileobj=io.BytesIO(gzip.decompress(raw)), mode='r:', ignore_zeros=True) as archive:
                config = yaml.safe_load(archive.extractfile('.melange.yaml').read())
                properties = dict(line.split(' = ', 1) for line in
                    archive.extractfile('.PKGINFO').read().decode().splitlines() if ' = ' in line)
                sources.add(config['vars']['glibc-expected-commit'])
                architectures.add(properties['arch'])
                # Extract only runtime libraries; APK metadata/scripts never run.
                for member in archive:
                    if member.name.startswith('usr/lib/'):
                        archive.extract(member, root, filter='data')
            report['packages'][label].append({'name': path.name, 'sha256': hashlib.sha256(raw).hexdigest()})
        lib = root/'usr/lib'
        loaders = list(lib.glob('ld-linux-*.so.*'))
        assert len(loaders) == 1, loaders
        binary = root/'benchmark'
        subprocess.run(compiler+['-O2', '-fno-builtin', '-Wall', '-Wextra', '-Werror', '-pthread',
            str(Path(__file__).resolve().parents[1]/'tests/benchmark.c'), str(lib/'libc.so.6'),
            '-Wl,--dynamic-linker='+str(loaders[0]), '-Wl,--disable-new-dtags',
            '-Wl,-rpath,'+str(lib), '-o', str(binary)], check=True)
        binaries[label] = binary
        report['packages'][label+'-elf'] = subprocess.check_output(['readelf', '-lWd', str(binary)], text=True)
    assert len(sources) == len(architectures) == 1, 'source commits or architectures differ'
    native_arch = {'arm64': 'aarch64'}.get(platform.machine(), platform.machine())
    assert architectures == {native_arch}, 'APKs must match native architecture'
    report['source_commit'] = sources.pop()
    generator = random.Random(0)
    for mode in ['startup', 'allocate', 'threads', 'memory', 'identity', 'hosts-file']:
        samples = {label: [] for label in binaries}
        for binary in binaries.values():
            subprocess.run([binary, mode], env=env, stdout=subprocess.DEVNULL, check=True)
        for _ in range(args.rounds):
            order = list(binaries)
            generator.shuffle(order)
            for label in order:
                if mode == 'startup':
                    start = time.perf_counter()
                    for _ in range(200):
                        subprocess.run([binaries[label], mode], env=env, check=True)
                    elapsed = (time.perf_counter()-start)/200
                else:
                    elapsed = float(subprocess.check_output([binaries[label], mode], env=env, text=True))
                samples[label].append(elapsed)
        report['samples'][mode] = samples
        medians = {label: statistics.median(values) for label, values in samples.items()}
        regression = 100 * (medians['candidate']/medians['baseline'] - 1)
        print(f'{mode}: {regression:+.2f}% median elapsed time', flush=True)
        report.setdefault('medians', {})[mode] = dict(medians, regression_percent=regression)
args.output.parent.mkdir(parents=True, exist_ok=True)
args.output.write_text(json.dumps(report, indent=2)+'\n')
print('Microbenchmarks only; external DNS, RSS and service workloads still require validation.')
