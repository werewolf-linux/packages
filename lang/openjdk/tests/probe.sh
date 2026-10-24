#!/bin/sh
# probe.sh WORK holds the installed JDK to the protections patches/series
# compiles in, then takes each back with an allowance and sees it return.
# WORK holds Probe.java. The package test runs it as root, with
# /etc/runtime-hardening/allow free to use (package.py embeds it).
set -eu
work=$1
allow=/etc/runtime-hardening/allow/all/java
trap 'rm -rf "$allow"' EXIT
cd "$work"
# Probe uses sun.misc.Unsafe on purpose: javac warns, and that is all.
javac -nowarn Probe.java

expect() {
	name=$1
	want=$2
	shift 2
	got=$("$@" 2>"$work/stderr")
	if [ "$got" != "$want" ]; then
		echo "probe: FAIL: $name" >&2
		echo "  want $want" >&2
		echo "  got  $got" >&2
		cat "$work/stderr" >&2
		exit 1
	fi
	echo "probe: ok: $name"
}

state() {
	echo "{\"options\":\"$1\",\"attach\":\"disabled\",\"deserialize\":\"$2\",\"primitive_array\":\"read\",\"unsafe\":\"$3\",\"native\":\"$4\"}"
}
closed=$(state none refused refused refused)

rm -rf "$allow"
expect 'closed without allowances' "$closed" java Probe
grep -F '"group":"deserialize"' "$work/stderr" | grep -F 'java.lang.Integer'
grep -F '"group":"native"' "$work/stderr" | grep -F 'sun.misc.Unsafe'
grep -F '"group":"native"' "$work/stderr" | grep -F 'reinterpret'

expect 'replacing System.err cannot hide caught denials' "$closed" java Probe hide-stderr
grep -F '"event":"runtime-denial"' "$work/stderr" | grep -F '"group":"deserialize"'

# The environment opens nothing: the JVM and the launcher read none of it.
open='-Dwerewolf.probe=leaked -Djdk.serialFilter=* -Dsun.misc.unsafe.memory.access=allow -XX:-DisableAttachMechanism'
expect 'JAVA_TOOL_OPTIONS is ignored' "$closed" env JAVA_TOOL_OPTIONS="$open" java Probe
expect '_JAVA_OPTIONS is ignored' "$closed" env _JAVA_OPTIONS="$open" java Probe
expect 'JDK_JAVA_OPTIONS is ignored' "$closed" env \
	JDK_JAVA_OPTIONS="-Dwerewolf.probe=leaked --illegal-native-access=allow --sun-misc-unsafe-memory-access=allow" \
	java Probe
# Stock java fails here: it would load /nonexistent/libjvm.so.
expect 'JDK_ALTERNATE_VM is ignored' "$closed" env JDK_ALTERNATE_VM=/nonexistent java Probe

# Neither does argv; its own system properties still reach the program.
expect 'argv cannot lift a protection' "$(state argv refused refused refused)" \
	java -Dwerewolf.probe=argv -Djdk.serialFilter='*' --sun-misc-unsafe-memory-access=allow \
	--illegal-native-access=allow --enable-native-access=ALL-UNNAMED Probe

# A JAR's manifest still grants native access, as the image's own jars may.
printf 'Main-Class: Probe\nEnable-Native-Access: ALL-UNNAMED\n' >"$work/manifest"
jar --create --file "$work/probe.jar" --manifest "$work/manifest" -C "$work" Probe.class
expect "a JAR manifest grants native access" "$(state none refused refused allowed)" java -jar "$work/probe.jar"

# Each allowance takes back what it names, and nothing else.
mkdir -p "$allow"
for name in deserialize native; do
	rm -f "$allow"/*
	: >"$allow/$name"
	case $name in
	deserialize) want=$(state none read refused refused) ;;
	native) want=$(state none refused allowed allowed) ;;
	esac
	expect "the $name group takes back its own" "$want" java Probe
done

# A link is not an allowance, even to a file: lock times will be links.
rm -f "$allow"/*
: >"$work/target"
for name in deserialize native; do
	ln -s "$work/target" "$allow/$name"
done
expect 'a link is not an allowance' "$closed" java Probe
