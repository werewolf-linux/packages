"""Python: Wolfi's newest stable CPython, with Werewolf's policy patches (README.md).

The hooks scripts/packages.py names. Packages keep Wolfi's names; their epoch
sorts above every Wolfi rebuild of the same CPython snapshot."""
from pathlib import Path
import re
import shutil
from derive import fetch
import patching
import runtime

check_apk = runtime.check_apk

TITLE = 'Python'
FAMILY = 'python-'
SOURCES = ('https://github.com/python/cpython.git',)
UPSTREAM = 'https://github.com/python/cpython'
PATCHES = 'werewolf/patches'  # in the build context
# The configure option this build leaves out: sqlite3 loading native code.
REMOVED_OPTION = '--enable-loadable-sqlite-extensions'
# Wolfi builds the interpreter in its -runtime subpackage, once per item of
# its variants range; python-3.15 itself is a metapackage depending on it.
VARIANTS = [{'name': 'variants', 'items': {'': '${{vars.config-opts-gil}}'}}]
OPTIONS = 'config-opts-common'
RUNTIME = '${{package.name}}${{range.key}}-runtime'
KEPT_SUBPACKAGES = (RUNTIME,) + tuple('${{package.name}}${{range.key}}' + suffix
                                      for suffix in ('-base', '-dev', '-base-dev'))
OMITTED_SUBPACKAGES = tuple('${{package.name}}' + suffix for suffix in (
    't', 't-dev', '-jit', '-privileged-netbindservice', '${{range.key}}-tk', '${{range.key}}-doc'))
CLONE = 'Clone patched source for no-GIL and JIT builds'
PRUNE = 'Remove GUI and documentation files before splitting packages'
ALLOW = 'Allow every runtime-hardening group in the build guest only'
PROBE_PY, PROBE_SH = 'WEREWOLF_PROBE_PY', 'WEREWOLF_PROBE_SH'
# Every build-guest group, also granted in the test environment so Wolfi's own
# tests (which run `python -c`, scripts and pickle) pass; the probe clears them.
GROUPS = ['native', 'deserialize', 'inline', 'writable-source', 'environment']

def grant_all():
    return (runtime.build_root() + 'mkdir -p /etc/runtime-hardening/allow/all/python\n'
            'for f in ' + ' '.join(GROUPS) + '; do\n'
            '  : > /etc/runtime-hardening/allow/all/python/$f\n'
            'done\n')

def checkout(config):
    """The index of the recipe's one git-checkout step."""
    found = [i for i, step in enumerate(config['pipeline']) if step.get('uses') == 'git-checkout']
    if len(found) != 1:
        raise ValueError('Review changed upstream source checkout before deriving')
    return found[0]

def source_commit(config):
    found = [s for s in config['pipeline'] if s.get('uses') == 'git-checkout']
    return found[0].get('with', {}).get('expected-commit') if len(found) == 1 else None

def epoch(wolfi_epoch, revision):
    """Our epoch: above every Wolfi rebuild of the same snapshot, so apk
    prefers ours, and moving when either Wolfi's epoch or our revision does."""
    if not 0 <= wolfi_epoch < 1000:
        raise ValueError('Review Wolfi epoch ' + str(wolfi_epoch) + ': ours would no longer sort above it')
    return 1000 * revision + wolfi_epoch

def full_version(lock):
    return lock['version'] + '-r' + str(epoch(lock['epoch'], lock['revision']))

def context(pkg, destination):
    # Beside Wolfi's files, under a name CPython does not use at its top.
    shutil.copytree(pkg.dir / 'patches', destination / PATCHES)

def retained_subpackages(config):
    # Publish the ordinary interpreter and development packages only. New or
    # renamed upstream subpackages need review before we choose to ship them.
    subpackages = config['subpackages']
    if sorted(s['name'] for s in subpackages) != sorted(KEPT_SUBPACKAGES + OMITTED_SUBPACKAGES):
        raise ValueError('Review changed upstream subpackages')
    return [s for s in subpackages if s['name'] in KEPT_SUBPACKAGES]

def prune():
    # Without Wolfi's -tk and -doc split steps these files would stay in the
    # runtime and base packages. The last step of -runtime, which installs
    # them, before -base takes its share. Missing paths fail the build for
    # upstream review.
    return {'name': PRUNE, 'runs':
            'rm -R "${{targets.contextdir}}/usr/lib/${{vars.python}}/tkinter"\n'
            'rm "${{targets.contextdir}}/usr/lib/${{vars.python}}/lib-dynload"/_tkinter.*.so\n'
            'rm -R "${{targets.contextdir}}/usr/share/man"\n'}

# Wolfi's C extension test imports its module from the working directory,
# which ours leaves off sys.path (patches/0002): the same build and import,
# with the module's directory named.
C_EXT = 'test/python/interpreter/c-ext'

def c_ext(python):
    return {'name': 'Build and import a C extension from its directory', 'runs':
            'python="' + python + '"\n'
            'd=$(mktemp -d)\n'
            "cat > \"$d/c_ext_check.c\" <<'EOF'\n"
            '#include <Python.h>\n'
            'static PyObject *answer(PyObject *self, PyObject *args) { return PyLong_FromLong(42); }\n'
            'static PyMethodDef methods[] = {{"answer", answer, METH_NOARGS, NULL}, {NULL, NULL, 0, NULL}};\n'
            'static struct PyModuleDef module = {PyModuleDef_HEAD_INIT, "c_ext_check", NULL, 0, methods};\n'
            'PyMODINIT_FUNC PyInit_c_ext_check(void) { return PyModuleDef_Init(&module); }\n'
            'EOF\n'
            'include=$("$python" -c \'import sysconfig; print(sysconfig.get_path("include"))\')\n'
            'ext=$("$python" -c \'import sysconfig; print(sysconfig.get_config_var("EXT_SUFFIX"))\')\n'
            'cc -shared -fPIC -I"$include" "$d/c_ext_check.c" -o "$d/c_ext_check$ext"\n'
            '"$python" -c "import sys; sys.path.insert(0, sys.argv[1]); import c_ext_check; '
            'assert c_ext_check.answer() == 42; print(\'c_ext_check: built and imported\')" "$d"\n'
            'rm -rf "$d"\n',
            'needs': {'packages': ['build-base']}}

def ours_for_c_ext(subpackages):
    """subpackages with each test of Wolfi's C extension step replaced by ours."""
    out = []
    for sub in subpackages:
        test = sub.get('test')
        if test and any(step.get('uses') == C_EXT for step in test.get('pipeline', [])):
            steps = []
            for step in test['pipeline']:
                if step.get('uses') == C_EXT:
                    if set(step) != {'uses', 'with'} or set(step['with']) != {'python'}:
                        raise ValueError('Review changed upstream C extension test before deriving')
                    step = c_ext(step['with']['python'])
                steps.append(step)
            sub = {**sub, 'test': {**test, 'pipeline': steps}}
        out.append(sub)
    return out

def runtime_of(subpackages):
    """The kept -runtime subpackage, which builds the interpreter."""
    return next(s for s in subpackages if s['name'] == RUNTIME)

def probe(pkg):
    py = (pkg.dir / 'tests/probe.py').read_text()
    sh = (pkg.dir / 'tests/probe.sh').read_text()
    for text in (py, sh):
        if '${{' in text or PROBE_PY in text or PROBE_SH in text:
            raise ValueError('tests/probe.* cannot be embedded in a Melange recipe')
    return (runtime.root_probe('python', ['python3', '-V']) + "mkdir -p /tmp/probe\n"
            "cat > /tmp/probe/probe.py <<'" + PROBE_PY + "'\n" + py + PROBE_PY + '\n' +
            "cat > /tmp/probe/probe.sh <<'" + PROBE_SH + "'\n" + sh + PROBE_SH + '\n' +
            'sh /tmp/probe/probe.sh /tmp/probe/probe.py\n')

def derive(pkg, config, lock):
    original = config['package']['name']
    if original != Path(lock['recipe']).stem or str(config['package']['version']) != lock['version']:
        raise ValueError('Review changed upstream package identity before deriving')
    pipeline = config['pipeline']
    at = checkout(config)
    source = pipeline[at].get('with', {})
    assert source.get('expected-commit') == lock['source_commit'], 'source pin differs from the lock'
    if source.get('repository') != UPSTREAM or set(source) - {'branch', 'tag', 'expected-commit', 'repository'}:
        raise ValueError('Review changed upstream source checkout before deriving')
    pipeline[at] = fetch(lock['source_commit'], pkg.sources(lock))
    # Wolfi's patches first, then ours, before configuring ordinary Python.
    # Recognize the variant clone step before removing it with the variants.
    patched = [i for i, step in enumerate(pipeline) if step.get('uses') == 'patch']
    clones = [i for i, step in enumerate(pipeline) if step.get('name') == CLONE]
    configures = [i for i, step in enumerate(pipeline) if step.get('uses') == 'autoconf/configure']
    if not patched or len(clones) != 1 or configures or not patched[-1] < clones[0]:
        raise ValueError('Review changed upstream patch and clone steps before deriving')
    if config.get('data') != VARIANTS:
        raise ValueError('Review changed upstream interpreter variants before deriving')
    pipeline.pop(clones[0])
    # The build runs the new python on build-tree scripts (writable-source),
    # with -c (inline), importing every extension it built (native), and its
    # PGO suite unpickles (deserialize) and sets PYTHON* variables
    # (environment). So the build guest, and only it, grants every group.
    # Packages hold nothing under /etc/runtime-hardening; werewolf's forms
    # grant only the groups lib/allow.zig defines, never `environment`.
    pipeline[patched[-1] + 1:patched[-1] + 1] = [
        patching.step(PATCHES),
        {'name': ALLOW, 'runs': grant_all()}]
    options = config['vars'][OPTIONS]
    line = re.compile(r'^[ \t]*' + re.escape(REMOVED_OPTION) + r'[ \t]*\\\n', re.M)
    config['vars'][OPTIONS], count = line.subn('', options)
    if count != 1:
        raise ValueError('Review changed upstream configure option ' + REMOVED_OPTION + ' before deriving')
    config['subpackages'] = ours_for_c_ext(retained_subpackages(config))
    build = runtime_of(config['subpackages'])['pipeline']
    if not [s for s in build if s.get('uses') == 'autoconf/configure']:
        raise ValueError('Review changed upstream runtime build before deriving')
    build.append(prune())
    wolfi = str(config['package']['version']) + '-r' + str(config['package']['epoch'])
    config['package']['epoch'] = epoch(config['package']['epoch'], lock['revision'])
    config['package']['description'] += ' (Werewolf fixed hardening policy)'
    # Each package names the Wolfi build it rebuilds: werewolf matches Wolfi's
    # CVE fixes against that (lib/prebuilt.zig), not against our epoch.
    for section in [config['package']] + config['subpackages']:
        section.setdefault('dependencies', {}).setdefault('provides', []).append(
            'wolfi-origin:' + original + ':' + wolfi)
    config['test']['pipeline'].append({'name': 'Werewolf protections hold, and allowances take each back',
                                       'needs': {'packages': ['build-base']},
                                       'runs': probe(pkg)})
    config['update'] = {'enabled': False}
    return config

def test_recipe(config, repository):
    # Wolfi's tests, and each subpackage's, run `python -c`, scripts and pickle,
    # so grant every group in every test environment; the probe (in the main
    # test) clears them and proves deny-by-default on its own. Done only when
    # tests run, so the derived recipe stays a faithful copy of Wolfi's.
    grant = {'name': 'Grant every group for the suite (the probe clears them)', 'runs': grant_all()}
    for section in [config] + config.get('subpackages', []):
        test = section.get('test')
        if test and test.get('pipeline'):
            test['pipeline'].insert(0, dict(grant))
    return config

def check(pkg, upstream, derived):
    """Keep Wolfi's ordinary build and tests, apart from the documented omissions."""
    options = lambda c: c['vars'][OPTIONS].splitlines()
    kept = [o for o in options(upstream) if REMOVED_OPTION not in o]
    assert options(derived) == kept, 'upstream configure options changed beyond the removed one'
    assert not [o for o in options(derived) if REMOVED_OPTION in o], 'loadable sqlite extensions still configured'
    assert derived['environment'] == upstream['environment'], 'upstream build environment changed'
    patches = lambda c: [x for x in c['pipeline'] if x.get('uses') == 'patch']
    assert patches(upstream) == patches(derived), "Wolfi's patches changed"
    names = [x.get('name') for x in derived['pipeline']]
    ours = names.index('Apply Werewolf policy patches')
    assert derived['pipeline'][ours - 1] == patches(upstream)[-1], "our patches must follow Wolfi's"
    assert CLONE not in names, 'the omitted variants must not clone the source tree'
    assert derived.get('data') == VARIANTS == upstream.get('data'), 'interpreter variants changed'
    runtime = runtime_of(derived['subpackages'])['pipeline']
    assert runtime[-1] == prune(), 'GUI and documentation files must be removed before splitting'
    assert PRUNE not in names and sum(x.get('name') == PRUNE for x in runtime) == 1, \
        'one GUI and documentation cleanup step, in -runtime'
    ours_only = ('Apply Werewolf policy patches', ALLOW, 'Fetch the locked source commit')
    steps = lambda c, omitted: [x for x in c['pipeline']
                               if x.get('name') not in omitted and x.get('uses') != 'git-checkout']
    assert steps(upstream, (CLONE,)) == steps(derived, ours_only), 'an upstream build step changed'
    # Ours, apart from the wolfi-origin provides and the cleanup in -runtime.
    strip = lambda subs: [{**s, 'dependencies': {**s.get('dependencies', {}), 'provides': [
        p for p in s.get('dependencies', {}).get('provides', []) if not p.startswith('wolfi-origin:')]},
        'pipeline': [x for x in s.get('pipeline', []) if x != prune()]}
        for s in subs]
    assert strip(ours_for_c_ext(retained_subpackages(upstream))) == strip(derived['subpackages']), \
        'an upstream subpackage changed'
    for test in upstream['test']['pipeline']:
        assert test in derived['test']['pipeline'], "Wolfi's package test removed"
