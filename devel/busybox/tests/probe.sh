# Each tool that runs a command runs an applet named bare from busybox itself:
# with PATH naming nothing, the PATH search a plain execvp would need finds no
# program, as Werewolf's glibc searches none.
set -eu
for tool in env 'timeout 5' nohup nice setsid flock\ /tmp/busybox-probe.lock; do
	# shellcheck disable=SC2086 # the tool's words split on purpose
	PATH=/nonexistent /usr/bin/busybox $tool true
done
echo x | PATH=/nonexistent /usr/bin/busybox xargs true
PATH=/nonexistent /usr/bin/busybox time true 2> /dev/null
rm -f /tmp/busybox-probe.lock

# All variants leave /bin/sh to the standalone shell package.
for manifest in /etc/busybox-paths.d/*; do
    [ -f "$manifest" ] || continue
    if /usr/bin/busybox grep -Eq '^(usr/)?bin/sh$' "$manifest"; then
        echo "BusyBox claims /bin/sh: $manifest" >&2
        exit 1
    fi
done

# No variant builds wget, telnet or telnetd, so no manifest links them.
for applet in wget telnet telnetd; do
    if /usr/bin/busybox --list | /usr/bin/busybox grep -qx "$applet"; then
        echo "BusyBox builds $applet" >&2
        exit 1
    fi
done
