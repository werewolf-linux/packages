# OpenJDK

Wolfi's newest stable OpenJDK recipe with runtime restrictions; currently `openjdk-27`.

## Use and allowances

Werewolf: add `openjdk-27-jre` and `openjdk-27-default-jvm` to `packages` for services;
use `openjdk-27`/`openjdk-27-default-jdk` for compilation. Wolfi, after [feed setup](../../docs/usage.md#install):

```sh
apk add openjdk-27-jre@hardened openjdk-27-default-jvm@hardened
```

Run `java -jar /usr/lib/app/app.jar` as a non-root user. Werewolf's normal JVM JIT
needs top-level `allow: [jit]`. Add `allow: [java:native]` to the existing service
for native libraries, or create `/etc/runtime-hardening/allow/SERVICE/java/native`
on Wolfi. The namespace is `java`, not `openjdk`.
See [grant scope and restart instructions](../../docs/usage.md#grant-an-allowance).

## Patches

| Patch | One-line change | Allowance / policy |
|---|---|---|
| [0001](patches/0001-ignore-environment-options.patch) | Ignore `_JAVA_OPTIONS`, `JAVA_TOOL_OPTIONS`, `JDK_JAVA_OPTIONS` and `JDK_ALTERNATE_VM`. | Always denied |
| [0002](patches/0002-disable-attach-by-default.patch) | Default `DisableAttachMechanism` to true. | Target JVM flag `-XX:-DisableAttachMechanism` |
| [0003](patches/0003-werewolf-allowances.patch) | Read and cache regular allowance files from the service or `all` namespace. | Always on |
| [0004](patches/0004-deserialization-refused.patch) | Reject nonprimitive classes checked by `ObjectInputStream`, regardless of serialization filters. | `deserialize` |
| [0005](patches/0005-unsafe-memory-access-denied.patch) | Deny `sun.misc.Unsafe` memory access regardless of the requested Unsafe mode. | `native` |
| [0006](patches/0006-native-access-denied.patch) | Deny illegal JNI/FFM native access and ignore command-line native grants. | `native`; JAR manifest native grants still work |
| [0007](patches/0007-refuse-effective-root.patch) | Refuse effective UID 0 at VM creation, including `JNI_CreateJavaVM`. | Always denied |
| [0008](patches/0008-log-denials-to-stderr.patch) | Emit JSON denial records directly to stderr. | Always on |

`native` restores upstream native-access controls; `--enable-native-access`,
`--illegal-native-access` and `--sun-misc-unsafe-memory-access` then apply normally.
A JAR's `Enable-Native-Access` manifest grants native access without the marker;
it does not restore Unsafe memory access. Primitive arrays remain deserializable.
Allowance results are cached: restart the JVM after every allowance change.

## Debug

Use [stderr denial records](../../docs/usage.md#debug-a-refusal) to identify the group.

| Symptom | Fix |
|---|---|
| `InvalidClassException` while reading Java serialization | Grant `java:deserialize`, then retain application serialization filters. RMI/JMX remoting may need this too. |
| Unsafe memory operation denied | Grant `java:native`; an argv option or native-access manifest alone does not unlock Unsafe. |
| JNI/FFM restricted method denied despite `--enable-native-access` | Grant `java:native`, then set the required JVM native-access options; inspect the log's target module/method. |
| Netty/Kryo/Chronicle/Kafka/zstd-jni native path fails | Confirm the denied operation; grant `java:native` if that path is required. |
| `jcmd`, `jstack`, `jmap` or attach agent cannot attach | Restart the target with `-XX:-DisableAttachMechanism`; check UID and OS attach permissions. |
| JVM options in environment have no effect | Put options on argv or in an explicit `@argfile`. There is no `java:environment` grant. |
| Alternate VM environment setting ignored | Use explicit `-XXaltjvm=PATH` if required; the argv mechanism remains. |
| Startup refuses root | Drop effective UID before VM creation; no production allowance exists. |
| Executable-memory failure on Werewolf | Check top-level `jit` and kernel policy; `java:native` does not enable JIT mappings. |

Some exception text names the obsolete `/etc/werewolf/allow/java` path; use
`/etc/runtime-hardening/allow/SERVICE/java/GROUP` as above.

## Remaining behavior and build differences

`CLASSPATH` still applies without `-cp`/`-jar`; set the class path explicitly.
`JDK_AOT_VM_OPTIONS` still applies with `-XX:AOTMode=create`; `_JAVA_LAUNCHER_DEBUG`
still traces the launcher. The headless build omits splash-screen support.
`inline`/`writable-source` names accepted by forms have no Java enforcement yet:
jshell, writable classes/JARs, generated classes and custom loaders remain available.
Process execution remains subject to OS/Werewolf policy, without a Java `exec` gate.

Keep Wolfi's commit, patches, toolchain, JDK/JRE builds and JIT. Add
`--enable-headless-only` to omit desktop windows and X11 integration; retain
fontconfig, fonts and image codecs for server-side text and image rendering.
Fetch the locked commit from `jdk27u`, with mainline `jdk` as fallback.
Ship the JDK, JRE and default-jdk/default-jvm selectors. Omit demos, the separate
jmods package/build target and OCI-entrypoint compatibility/download; remove demo files.
Linkable-runtime builds and jlink tests remain. Attach, RMI, management-RMI and
JDWP modules still ship; the package is not a reduced jlink runtime.
Epoch is `1000 × policy revision + Wolfi epoch`; `wolfi-origin` preserves CVE matching.
Retained Wolfi tests and [probes](tests/probe.sh) check grants, manifests and refusals.
[Build instructions](../../docs/wolfi.md#building-locally); patches: GPL-2.0-only WITH Classpath-exception-2.0.
