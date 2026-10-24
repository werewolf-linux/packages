#!/usr/bin/env python3
"""Exercise the actual patched loader/libc; fixtures must prove injection."""
import argparse
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--build', required=True, type=Path)
    parser.add_argument('--cc', default='gcc')
    parser.add_argument('--cxx', default='g++')
    parser.add_argument('--runner', default='', help='e.g. qemu-x86_64 for local cross validation')
    parser.add_argument('--baseline-loader', type=Path, help='unpatched loader with a matching ABI, for substitution controls')
    parser.add_argument('--report', type=Path, help='record compiler and ELF hardening evidence')
    parser.add_argument('--secure', action='store_true', help='also test actual setuid transitions; requires real root, no emulator')
    parser.add_argument('--trace-spawn', action='store_true', help='verify child IFS sanitation with native strace')
    args = parser.parse_args()
    build = args.build.resolve()
    loader = build / 'elf/ld.so'
    assert loader.is_file() and (build / 'libc.so').is_file()
    source = Path(__file__).resolve().parent
    cc = shlex.split(args.cc)
    runner = shlex.split(args.runner)
    evidence = {'compiler': subprocess.check_output(cc+['--version'], text=True), 'elf': {}}
    options = subprocess.run(cc+['-###', '-O2', '-c', '-x', 'c', '-'], input='', capture_output=True, text=True, check=True)
    evidence['resolved_compiler_options'] = options.stderr
    # This is independent of the upstream suite's permitted failure count.
    subprocess.run(['make', '-C', str(build), 'test', 't=elf/tst-werewolf-audit',
                    'test-wrapper=' + args.runner],
                   check=True)
    result = subprocess.run(runner + [str(build/'elf/tst-werewolf-audit')],
                            capture_output=True, text=True, timeout=30, check=True)
    undefined = subprocess.check_output(['nm', '-u', str(build/'elf/dl-audit.os')], text=True)
    assert '_rtld_global' not in undefined, 'audit dispatch still reads loader state'
    evidence['audit_dispatch'] = {'status': 'PASS', 'output': result.stdout,
                                  'undefined_symbols': undefined}
    for path in [loader, build/'libc.so']:
        headers = subprocess.check_output(['readelf', '-lW', str(path)], text=True)
        dynamic = subprocess.check_output(['readelf', '-dW', str(path)], text=True)
        stack = [line for line in headers.splitlines() if 'GNU_STACK' in line]
        assert len(stack) == 1 and 'RW ' in stack[0] and 'RWE' not in stack[0], path
        assert 'GNU_RELRO' in headers, path
        assert 'BIND_NOW' in dynamic or 'Flags: NOW' in dynamic, path
        # Headers and read-only data are not executable (-z separate-code).
        loads = [line.split() for line in headers.splitlines() if line.split()[:1] == ['LOAD']]
        assert loads and 'E' not in ''.join(loads[0][6:-1]), path
        evidence['elf'][str(path.relative_to(build))] = {
            'program_headers': headers, 'dynamic_tags': dynamic,
            'notes': subprocess.check_output(['readelf', '-nW', str(path)], text=True)}
    env = dict(os.environ, NEUTERED_PUBLIC='application-value')
    for key in list(env):
        if key.startswith('LD_') or key == 'GLIBC_TUNABLES':
            del env[key]
    with tempfile.TemporaryDirectory(prefix='neutered-test-') as directory:
        work = Path(directory)
        marker = work / 'executed'
        env['NEUTERED_MARKER'] = str(marker)
        env['NEUTERED_CONVERTERS'] = str(work)
        runtime = [str(build), str(build/'math'), str(build/'elf'), str(work)]
        for compiler, library in [(cc, 'libgcc_s.so.1'), (shlex.split(args.cxx), 'libstdc++.so')]:
            path = subprocess.check_output(compiler+['-print-file-name='+library], text=True).strip()
            if Path(path).is_file():
                runtime.append(str(Path(path).resolve().parent))
        flags = ['-O2', '-Wall', '-Wextra', '-Werror', '-L'+str(build),
                 '-Wl,--disable-new-dtags',
                 '-Wl,-rpath,'+':'.join(runtime),
                 '-Wl,-rpath-link,'+str(build/'elf'),
                 '-Wl,--dynamic-linker='+str(loader)]
        def compile(extra, output, filename='policy.c'):
            startup = []
            libraries = [str(build/'libc_nonshared.a'), str(loader)]
            linkflags = list(flags)
            if '-static' in extra:
                linkflags = [x for x in linkflags if not x.startswith('-Wl,--dynamic-linker=')]
                startup = ['-B'+str(build/'csu')+'/', '-no-pie']
                libraries = []
            compiler = shlex.split(args.cxx) if filename.endswith('.cc') else cc
            subprocess.run(compiler + linkflags + startup + extra + [str(source/filename)] + libraries + ['-o', str(output)], check=True)
        binary = work / 'policy'
        compile(['-pthread', '-ldl'], binary)
        dso = work / 'inject.so'
        compile(['-shared', '-fPIC'], dso, 'inject.c')
        (work/'gconv-modules').write_text('module INTERNAL NEUTERED-PROBE// inject 1\n')
        def run(command, changes=None, success=True, cwd=None):
            target_env = dict(env, **(changes or {}))
            prefix = list(runner)
            if runner:
                # Host qemu must not interpret target LD_* before emulation.
                for key in list(target_env):
                    if key.startswith('LD_') or key == 'GLIBC_TUNABLES':
                        prefix += ['-E', key+'='+target_env.pop(key)]
            result = subprocess.run(prefix + [str(x) for x in command], env=target_env,
                                    capture_output=True, text=True, timeout=30, cwd=cwd)
            assert (result.returncode == 0) == success, (command, result.returncode, result.stdout, result.stderr)
            return result
        # Control: explicit loading really executes the test constructor.
        run([binary, 'load', dso])
        assert marker.read_text() == 'injected\n'
        marker.unlink()
        run([binary])
        assert run([binary, 'command-path']).stdout == 'implicit command lookup denied\n'
        assert run([binary, 'exec-explicit', binary]).stdout == 'explicit exec passed\n'
        unwinder = work / 'unwind'
        compile([], unwinder, 'unwind.cc')
        run([unwinder])
        # Retain IFUNC execution at startup and through later symbol lookup.
        ifunc = work/'ifunc'
        compile([], ifunc, 'ifunc.c')
        run([ifunc])
        ifunc_dso = work/'ifunc.so'
        compile(['-shared', '-fPIC', '-DIFUNC_DSO'], ifunc_dso, 'ifunc.c')
        symbols = subprocess.check_output(['readelf', '-sW', str(ifunc_dso)], text=True)
        assert any('IFUNC' in line and 'neutered_ifunc' in line
                   for line in symbols.splitlines()), 'IFUNC fixture lost its resolver'
        ifunc_load = work/'ifunc-load'
        compile(['-DIFUNC_LOAD', '-ldl'], ifunc_load, 'ifunc.c')
        run([ifunc_load, ifunc_dso])
        evidence['ifunc'] = {'status': 'PASS', 'paths': ['startup', 'dlopen', 'dlmopen', 'dlsym']}
        # Denying LD_BIND_NOW must not undo eager binding of lazy objects.
        unresolved_source = work/'unresolved.c'
        unresolved_source.write_text('extern int neutered_missing_symbol(void);\nint neutered_call(void) { return neutered_missing_symbol(); }\n')
        unresolved = work/'unresolved.so'
        compile(['-shared', '-fPIC', '-Wl,-z,lazy'], unresolved, str(unresolved_source))
        assert 'BIND_NOW' not in subprocess.check_output(['readelf', '-dW', str(unresolved)], text=True)
        result = run([binary, 'load-lazy', unresolved], success=False)
        assert 'neutered_missing_symbol' in result.stderr
        # Refuse executable-stack requests before the application's entry point.
        for extra, label in [([], 'dynamic'), (['-static'], 'static')]:
            executable = work/('execstack-'+label)
            compile(extra+['-Wl,-z,execstack', '-pthread', '-ldl'], executable)
            headers = subprocess.check_output(['readelf', '-lW', str(executable)], text=True)
            assert any('GNU_STACK' in line and 'RWE' in line for line in headers.splitlines())
            result = run([executable], {'GLIBC_TUNABLES': 'glibc.rtld.execstack=2'}, success=False)
            assert 'executable stack is not allowed' in result.stderr
        controls = {
            'LD_PRELOAD': str(dso), 'LD_AUDIT': str(dso), 'LD_LIBRARY_PATH': str(work),
            'LD_DEBUG': 'all', 'LD_DEBUG_OUTPUT': str(work/'debug'),
            'LD_PROFILE': 'libc.so.6', 'LD_PROFILE_OUTPUT': str(work),
            'GLIBC_TUNABLES': 'glibc.malloc.tcache_count=0:glibc.malloc.arena_max=1',
            'MALLOC_ARENA_MAX': '1', 'MALLOC_CHECK_': '3',
            'LANG': 'neutered/nonexistent', 'LANGUAGE': 'neutered/nonexistent',
            'LC_ALL': 'neutered/nonexistent', 'LC_CTYPE': 'neutered/nonexistent',
            'LOCPATH': str(work), 'GCONV_PATH': str(work), 'TZ': 'GMT+11',
            'RES_OPTIONS': 'rotate attempts:1', 'HOSTALIASES': str(work/'aliases'),
            'TMPDIR': str(work/'missing'), 'GMON_OUT_PREFIX': str(work/'profile'),
        }
        result = run([binary], controls)
        assert result.stderr == '', result.stderr
        assert not marker.exists()
        assert not list(work.glob('debug*')) and not list(work.glob('*.profile'))
        defaults = run([loader, '--list-tunables']).stdout
        assert 'glibc.rtld.execstack: 0 ' in defaults
        assert run([loader, '--list-tunables'], controls).stdout == defaults
        # A real dependency substitution attempt, not merely setting LD_LIBRARY_PATH.
        good, bad = work/'good', work/'bad'
        good.mkdir(); bad.mkdir()
        good_source = work/'good.c'
        good_source.write_text('int neutered_dependency(void) { return 42; }\n')
        good_dso = good/'libneutered-needed.so.1'
        compile(['-shared', '-fPIC', '-Wl,-soname,libneutered-needed.so.1'], good_dso, str(good_source))
        bad_dso = bad/'libneutered-needed.so.1'
        bad_dso.write_bytes(dso.read_bytes())
        dependency_source = work/'needed.c'
        dependency_source.write_text('#include <assert.h>\nextern int neutered_dependency(void);\nint main(void) { assert(neutered_dependency() == 42); return 0; }\n')
        dependency = work/'needed'
        subprocess.run(cc + flags + [str(dependency_source), '-L'+str(good), '-l:libneutered-needed.so.1',
                       str(loader), '-Wl,--enable-new-dtags', '-Wl,-rpath,'+str(good), '-o', str(dependency)], check=True)
        run([binary, 'load', bad_dso])
        assert marker.exists(); marker.unlink()
        if args.baseline_loader:
            # Prove this dependency can be substituted through the actual
            # CLI search mechanism, using the build host's unpatched loader.
            run([args.baseline_loader, '--library-path', ':'.join([str(bad)]+runtime), dependency])
            assert marker.read_text() == 'injected\n'
            marker.unlink()
        run([dependency], {'LD_LIBRARY_PATH': str(bad)})
        assert not marker.exists()
        result = run([loader, '--library-path', bad, dependency], success=False)
        assert 'loader overrides are disabled' in result.stderr
        assert not marker.exists()
        # Looney-Tunables-like nesting/duplicates and long malformed settings.
        run([binary], {'GLIBC_TUNABLES': 'glibc.malloc.mxfast='*2000,
                       'LD_PRELOAD': str(dso)+':'+('x'*32000)})
        for option in ['--preload', '--audit', '--library-path', '--inhibit-rpath',
                       '--inhibit-cache', '--glibc-hwcaps-prepend', '--glibc-hwcaps-mask']:
            result = run([loader, option, str(dso), binary], success=False)
            assert 'loader overrides are disabled' in result.stderr
            assert not marker.exists()
        # Both ELF audit mechanisms, rather than just the environment alias.
        for option in ['--audit', '--depaudit']:
            audited = work / ('elf-'+option[2:])
            compile(['-pthread', '-ldl', '-Wl,'+option+','+str(dso)], audited)
            result = run([audited], success=False)
            assert 'ELF audit requests are disabled' in result.stderr
            assert not marker.exists()
        # Static applications use a separate initialization path.
        static = work / 'static-policy'
        compile(['-static', '-pthread', '-ldl'], static)
        run([static], controls)
        assert run([static, 'command-path']).stdout == 'implicit command lookup denied\n'
        # Denying a private profiling path must not write into the current directory.
        for extra, label in [([], 'dynamic'), (['-static'], 'static')]:
            profile = work/('profile-'+label)
            compile(extra+['-pg', '-pthread', '-Wl,--enable-new-dtags'], profile, 'benchmark.c')
            prefix = work/'profile-output'
            if args.baseline_loader and not extra:
                run([args.baseline_loader, '--library-path', args.baseline_loader.resolve().parent, profile, 'startup'],
                    {'GMON_OUT_PREFIX': str(prefix)}, cwd=work)
                outputs = list(work.glob('profile-output.*'))
                assert outputs, 'baseline profiling control did not produce output'
                for output in outputs:
                    output.unlink()
            run([profile, 'startup'], {'GMON_OUT_PREFIX': str(prefix)}, cwd=work)
            assert not list(work.glob('profile-output.*')) and not (work/'gmon.out').exists()
        if args.trace_spawn:
            assert not runner, 'child-environment tracing requires native execution'
            for executable in [binary, static]:
                for mode in ['spawn-ifs', 'wordexp-ifs']:
                    trace = work/'spawn.trace'
                    run(['strace', '-f', '-v', '-s', '65536', '-e', 'trace=execve', '-o', trace, executable, mode])
                    child = [line for line in trace.read_text().splitlines() if 'execve("/bin/sh",' in line]
                    assert len(child) == 1, 'expected one shell execve'
                    assert 'NEUTERED_CHILD_ENV=forwarded' in child[0], 'application environment was lost'
                    assert ('IFS=NEUTERED_IFS_PROBE' in child[0]) == (mode == 'spawn-ifs'), 'IFS sanitation mismatch'
        # Default library paths/RPATH can see this DSO; external NSS must not.
        nss = work / 'libnss_neutered_probe.so.2'
        nss.write_bytes(dso.read_bytes())
        run([binary, 'nss'])
        assert not marker.exists()
        if args.secure:
            assert os.geteuid() == 0 and not runner, 'secure tests require real root and native execution'
            work.chmod(0o755)
            for executable in [binary, static]:
                os.chown(executable, 0, 0)
                executable.chmod(0o4755)
                def nobody():
                    os.setgroups([])
                    os.setgid(65534)
                    os.setuid(65534)
                result = subprocess.run([str(executable), 'secure'], env=dict(env, **controls), preexec_fn=nobody,
                                        capture_output=True, text=True, timeout=30)
                assert result.returncode == 0, (result.stdout, result.stderr)
        if args.report:
            args.report.write_text(json.dumps(evidence, indent=2)+'\n')
        print('PASS: ELF hardening, public APIs, C locale, ignored configuration, preload/audit/CLI, forged audit state, ELF tags, built-in converters/NSS, static startup')

if __name__ == '__main__':
    main()
