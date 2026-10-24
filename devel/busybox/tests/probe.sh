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
