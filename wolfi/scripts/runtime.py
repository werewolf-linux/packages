"""Build-only setup and identity probes for the hardened language runtimes."""

BUILD_ROOT = '/etc/runtime-hardening/build-root'


def check_apk(pkg, name, parts):
    """A package may never grant its own exceptions, including build-root."""
    import io
    import posixpath
    import tarfile
    with tarfile.open(fileobj=io.BytesIO(parts[2][1])) as archive:
        for entry in archive:
            path = posixpath.normpath(entry.name).lstrip('/')
            if path == 'etc/runtime-hardening' or path.startswith('etc/runtime-hardening/'):
                raise ValueError('Runtime package grants its own hardening exception: ' + name)


def build_root():
    # This is a build/test rootfs file, never an installed package file or a
    # supported form allowance. No environment variable or command line opens it.
    return ('mkdir -p /etc/runtime-hardening\n: > ' + BUILD_ROOT + '\n'
            'chmod 600 ' + BUILD_ROOT + '\n')


def root_probe(runtime, command):
    """Exercise real/effective UID mismatches before restoring the build marker."""
    import shlex
    argv = ' '.join(shlex.quote(arg) for arg in command)
    # Melange's bubblewrap guest starts as a 0700 temporary directory mounted
    # at /. Dropped-privilege probes must be able to traverse that guest root.
    return r'''chmod 755 /
root_work=$(mktemp -d)
chmod 755 "$root_work"
cat > "$root_work/identity.c" <<'WEREWOLF_IDENTITY_C'
#define _GNU_SOURCE
#include <grp.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
int main(int argc, char **argv) {
    if (argc < 4) return 125;
    uid_t real = (uid_t)strtoul(argv[1], NULL, 10);
    uid_t effective = (uid_t)strtoul(argv[2], NULL, 10);
    if (setgroups(0, NULL) || setresgid(65534, 65534, 65534) ||
        setresuid(real, effective, effective)) {
        perror("identity probe");
        return 125;
    }
    execvp(argv[3], argv + 3);
    perror("identity exec");
    return 125;
}
WEREWOLF_IDENTITY_C
cc -Wall -Werror -o "$root_work/identity" "$root_work/identity.c"
rm -f /etc/runtime-hardening/build-root
root_refused() {
    if "$@" >"$root_work/out" 2>"$root_work/err"; then
        echo 'probe: FAIL: effective root ran' >&2; exit 1
    fi
    grep -F 'effective UID 0' "$root_work/err"
    grep -F '"event":"runtime-denial"' "$root_work/err" | grep -F '"group":"root"'
}
''' + 'root_refused ' + argv + '\n' + r'''
# A real root UID does not prevent execution after effective privileges drop.
"$root_work/identity" 0 65534 ''' + argv + r'''
# A non-root real UID must not disguise effective root.
root_refused "$root_work/identity" 65534 0 ''' + argv + r'''
: > "$root_work/target"
ln -s "$root_work/target" /etc/runtime-hardening/build-root
root_refused ''' + argv + r'''
rm /etc/runtime-hardening/build-root
mkdir /etc/runtime-hardening/build-root
root_refused ''' + argv + r'''
rmdir /etc/runtime-hardening/build-root
''' + build_root() + argv + '\n' + r'''rm -rf "$root_work"
''' + 'echo "probe: ok: ' + runtime + ' checks effective UID; only the build marker permits root"\n'
