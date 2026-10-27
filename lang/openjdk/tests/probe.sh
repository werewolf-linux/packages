#!/bin/sh
# probe.sh WORK holds the installed JDK to the protections patches/series
# compiles in, then takes each back with an allowance and sees it return.
# WORK holds Probe.java. The package test runs it as root, with
# /etc/runtime-hardening/allow free to use (the YAML overlay includes it).
# WORK is writable, so every run grants writable-source to load the probe,
# but the ones that show its refusal.
set -eu
work=$1
allow=/etc/runtime-hardening/allow/all/java
trap 'rm -rf "$allow"' EXIT
cd "$work"
# An agent, which -javaagent loads through the class path as any other code.
printf 'public class Agent {\n    public static void premain(String a) {\n        System.setProperty("werewolf.probe", "agent");\n    }\n}\n' >Agent.java
# Probe uses sun.misc.Unsafe on purpose: javac warns, and that is all.
javac -nowarn Probe.java Agent.java
printf 'Premain-Class: Agent\n' >"$work/agent-manifest"
jar --create --file "$work/agent.jar" --manifest "$work/agent-manifest" -C "$work" Agent.class

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

# refuse NAME GROUP CMD...: the command fails, and a denial names GROUP.
refuse() {
	name=$1
	group=$2
	shift 2
	if "$@" >"$work/stdout" 2>"$work/stderr"; then
		echo "probe: FAIL: $name: it ran" >&2
		cat "$work/stdout" "$work/stderr" >&2
		exit 1
	fi
	if ! grep -F '"event":"runtime-denial"' "$work/stderr" | grep -qF "\"group\":\"$group\""; then
		echo "probe: FAIL: $name: no $group denial" >&2
		cat "$work/stderr" >&2
		exit 1
	fi
	echo "probe: ok: $name"
}

# grant NAME...: exactly these allowances, and writable-source for the probe.
grant() {
	rm -rf "$allow"
	mkdir -p "$allow"
	: >"$allow/writable-source"
	for name in "$@"; do
		: >"$allow/$name"
	done
}

# state OPTIONS DESERIALIZE UNSAFE UNMAP NATIVE EXEC: what Probe prints. Attach
# and perf data are off, and an XML external entity is refused, whatever is
# granted.
state() {
	echo "{\"options\":\"$1\",\"attach\":\"disabled\",\"perf_data\":\"off\",\"deserialize\":\"$2\",\"primitive_array\":\"read\",\"unsafe\":\"$3\",\"unmap\":\"$4\",\"native\":\"$5\",\"exec\":\"$6\",\"xml_external\":\"refused\"}"
}
closed=$(state none refused refused refused refused refused)

grant
expect 'closed without allowances' "$closed" java Probe
grep -F '"group":"deserialize"' "$work/stderr" | grep -F 'java.lang.Integer'
grep -F '"group":"native"' "$work/stderr" | grep -F 'sun.misc.Unsafe'
grep -F '"group":"native"' "$work/stderr" | grep -F 'reinterpret'
grep -F '"group":"native"' "$work/stderr" | grep -F 'invokeCleaner'
grep -F '"group":"exec"' "$work/stderr" | grep -F 'ProcessBuilder.start'

expect 'replacing System.err cannot hide caught denials' "$closed" java Probe hide-stderr
grep -F '"event":"runtime-denial"' "$work/stderr" | grep -F '"group":"deserialize"'

# The environment configures the VM as argv does, and lifts no protection
# either.
open='-Dwerewolf.probe=leaked -Djdk.serialFilter=* -Dsun.misc.unsafe.memory.access=allow'
leaked=$(state leaked refused refused refused refused refused)
expect 'JAVA_TOOL_OPTIONS lifts no protection' "$leaked" env JAVA_TOOL_OPTIONS="$open" java Probe
expect '_JAVA_OPTIONS lifts no protection' "$leaked" env _JAVA_OPTIONS="$open" java Probe
expect 'JDK_JAVA_OPTIONS lifts no protection' "$leaked" env \
	JDK_JAVA_OPTIONS="-Dwerewolf.probe=leaked --illegal-native-access=allow --sun-misc-unsafe-memory-access=allow" \
	java Probe
# Stock java fails here: it would load /nonexistent/libjvm.so.
expect 'JDK_ALTERNATE_VM is ignored' "$closed" env JDK_ALTERNATE_VM=/nonexistent java Probe

# Neither does argv; its own system properties still reach the program.
expect 'argv cannot lift a protection' "$(state argv refused refused refused refused refused)" \
	java -Dwerewolf.probe=argv -Djdk.serialFilter='*' --sun-misc-unsafe-memory-access=allow \
	--illegal-native-access=allow --enable-native-access=ALL-UNNAMED Probe

# A JAR's manifest still grants native access, as the image's own jars may.
printf 'Main-Class: Probe\nEnable-Native-Access: ALL-UNNAMED\n' >"$work/manifest"
jar --create --file "$work/probe.jar" --manifest "$work/manifest" -C "$work" Probe.class
expect "a JAR manifest grants native access" "$(state none refused refused refused allowed refused)" java -jar "$work/probe.jar"

# Code loads from read-only mounts: WORK's classes, a JAR there and an agent
# there need writable-source, however they are named.
rm -f "$allow/writable-source"
refuse 'a class path on a writable mount needs writable-source' writable-source java -cp "$work" Probe
refuse 'a JAR on a writable mount needs writable-source' writable-source java -jar "$work/probe.jar"
refuse 'an agent on a writable mount needs writable-source' writable-source \
	java -javaagent:"$work/agent.jar" -version
refuse 'an agent from the environment needs it too' writable-source \
	env JAVA_TOOL_OPTIONS=-javaagent:"$work/agent.jar" java -version
grant
expect 'writable-source takes back the agent' "$(state agent refused refused refused refused refused)" \
	java -javaagent:"$work/agent.jar" Probe

# The VM's command hooks run a shell: they need exec, however they are set.
grant
refuse 'OnOutOfMemoryError needs exec' exec java -XX:OnOutOfMemoryError=true -version
refuse 'OnError from the environment needs exec' exec env JAVA_TOOL_OPTIONS=-XX:OnError=true java -version
grant exec
if ! java -XX:OnOutOfMemoryError=true -XX:OnError=true -version 2>"$work/stderr"; then
	echo "probe: FAIL: exec takes back the command hooks" >&2
	cat "$work/stderr" >&2
	exit 1
fi
echo "probe: ok: exec takes back the command hooks"

# Each allowance takes back what it names, and nothing else.
for name in deserialize unmap native exec; do
	grant "$name"
	case $name in
	deserialize) want=$(state none read refused refused refused refused) ;;
	unmap) want=$(state none refused refused allowed refused refused) ;;
	native) want=$(state none refused allowed allowed allowed refused) ;;
	exec) want=$(state none refused refused refused refused allowed) ;;
	esac
	expect "the $name group takes back its own" "$want" java Probe
done

# A link is not an allowance, even to a file: lock times will be links.
grant
: >"$work/target"
for name in deserialize unmap native exec; do
	ln -s "$work/target" "$allow/$name"
done
expect 'a link is not an allowance' "$closed" java Probe
rm -f "$allow/writable-source"
ln -s "$work/target" "$allow/writable-source"
refuse 'a link grants no writable-source' writable-source java -cp "$work" Probe

# What follows (the YAML appends it) runs from WORK too.
grant
