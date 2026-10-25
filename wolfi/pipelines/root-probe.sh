chmod 755 /
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
root_refused @COMMAND@

# A real root UID does not prevent execution after effective privileges drop.
"$root_work/identity" 0 65534 @COMMAND@
# A non-root real UID must not disguise effective root.
root_refused "$root_work/identity" 65534 0 @COMMAND@
: > "$root_work/target"
ln -s "$root_work/target" /etc/runtime-hardening/build-root
root_refused @COMMAND@
rm /etc/runtime-hardening/build-root
mkdir /etc/runtime-hardening/build-root
root_refused @COMMAND@
rmdir /etc/runtime-hardening/build-root
mkdir -p /etc/runtime-hardening
: > /etc/runtime-hardening/build-root
chmod 600 /etc/runtime-hardening/build-root
@COMMAND@
rm -rf "$root_work"
echo "probe: ok: @RUNTIME@ checks effective UID; only the build marker permits root"
