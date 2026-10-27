# OpenJDK

Wolfi's newest stable OpenJDK recipe with runtime restrictions; currently `openjdk-27`.

## Use and allowances

Werewolf: add `openjdk-27-jre` and `openjdk-27-default-jvm` to `packages` for services;
use `openjdk-27`/`openjdk-27-default-jdk` for compilation. Wolfi, after [feed setup](../../Werewolf/docs/usage.md#install):

```sh
apk add openjdk-27-jre@hardened openjdk-27-default-jvm@hardened
```

Run `java -jar /usr/lib/app/app.jar` as a non-root user. Werewolf's normal JVM JIT
needs top-level `allow: [jit]`. Add `allow: [java:native]` to the existing service
for native libraries, or create `/etc/runtime-hardening/allow/SERVICE/java/native`
on Wolfi. Code loads only from read-only mounts unless the service has `java:writable-source`. The namespace is `java`, not `openjdk`.
See [grant scope and restart instructions](../../Werewolf/docs/usage.md#grant-an-allowance).

## Patches

| Patch | One-line change | Allowance / policy |
|---|---|---|
| [0001](patches/0001-ignore-environment-options.patch) | Ignore `JDK_ALTERNATE_VM`: the environment never picks the VM. | Always denied |
| [0002](patches/0002-disable-attach-by-default.patch) | Default `DisableAttachMechanism` to true and `UsePerfData` to false. | Target JVM flags `-XX:-DisableAttachMechanism`, `-XX:+UsePerfData` |
| [0003](patches/0003-werewolf-allowances.patch) | Read and cache regular allowance files from the service or `all` namespace. | Always on |
| [0004](patches/0004-deserialization-refused.patch) | Reject nonprimitive classes checked by `ObjectInputStream`, regardless of serialization filters. | `deserialize` |
| [0005](patches/0005-unsafe-memory-access-denied.patch) | Deny `sun.misc.Unsafe` memory access regardless of the requested Unsafe mode. | `native`; `unmap` takes back `invokeCleaner` alone |
| [0006](patches/0006-native-access-denied.patch) | Deny illegal JNI/FFM native access and ignore command-line native grants. | `native`; JAR manifest native grants still work |
| [0007](patches/0007-refuse-effective-root.patch) | Refuse effective UID 0 at VM creation, including `JNI_CreateJavaVM`. | Always denied |
| [0008](patches/0008-log-denials-to-stderr.patch) | Emit JSON denial records directly to stderr. | Always on |
| [0009](patches/0009-writable-source.patch) | Load classes, JARs and agents only from read-only mounts; refuse class path sources that are not local files. | `writable-source` |
| [0010](patches/0010-command-hooks-exec.patch) | Refuse `-XX:OnError` and `-XX:OnOutOfMemoryError`, which run shell commands. | `exec` |
| [0011](patches/0011-exec-refused.patch) | Refuse running programs (`ProcessBuilder`, `Runtime.exec`). | `exec` |
| [0012](patches/0012-xml-external-references.patch) | Refuse XML external references (DTDs, entities, schemas) that neither the application nor the JDK catalog resolves. | Application resolvers still apply |
| [0013](patches/0013-jre-without-remote-agents.patch) | Build the JRE without `jdk.jdwp.agent`, `jdk.management.agent` and `jdk.naming.rmi`; the JRE also drops `rmiregistry`. | The JDK keeps them |

`native` restores upstream native-access controls; `--enable-native-access`,
`--illegal-native-access` and `--sun-misc-unsafe-memory-access` then apply normally.
A JAR's `Enable-Native-Access` manifest grants native access without the marker;
it does not restore Unsafe memory access. Primitive arrays remain deserializable.
Allowance results are cached: restart the JVM after every allowance change.

## Debug

Use [stderr denial records](../../Werewolf/docs/usage.md#debug-a-refusal) to identify the group.

| Symptom | Fix |
|---|---|
| `InvalidClassException` while reading Java serialization | Grant `java:deserialize`, then retain application serialization filters. RMI/JMX remoting may need this too. |
| Unsafe memory operation denied | Grant `java:native`; an argv option or native-access manifest alone does not unlock Unsafe. For `invokeCleaner` alone, which releases mapped files (Kafka's indexes), grant the narrower `java:unmap`. |
| JNI/FFM restricted method denied despite `--enable-native-access` | Grant `java:native`, then set the required JVM native-access options; inspect the log's target module/method. |
| Netty/Kryo/Chronicle/Kafka/zstd-jni native path fails | Confirm the denied operation; grant `java:native` if that path is required. |
| `jcmd`, `jstack`, `jmap` or attach agent cannot attach | Restart the target with `-XX:-DisableAttachMechanism`; check UID and OS attach permissions. |
| Class or JAR not found, with a `writable-source` denial | Keep code on the read-only image. Grant `java:writable-source` only to a service that unpacks its own code onto a writable mount. |
| VM refuses `-XX:OnError` or `-XX:OnOutOfMemoryError` | Prefer `-XX:+ExitOnOutOfMemoryError`; grant `java:exec` if a hook is required. |
| `Cannot run program ...: refused` | The service runs another program; grant `java:exec` only if it must, and name the program in its `run` list. |
| XML parse fails with a `CatalogException` | The document names an external DTD or entity. Supply it from the application's own resolver; nothing is fetched. |
| `jps`, `jstat` see no JVM | Perf data is off; start the target with `-XX:+UsePerfData`. |
| `-agentlib:jdwp` or JMX remote options fail on the JRE | Remote debugging and JMX remote are the JDK's alone. |
| Alternate VM environment setting ignored | Use explicit `-XXaltjvm=PATH` if required; the argv mechanism remains. |
| Startup refuses root | Drop effective UID before VM creation; no production allowance exists. |
| Executable-memory failure on Werewolf | Check top-level `jit` and kernel policy; `java:native` does not enable JIT mappings. |

Some exception text names the obsolete `/etc/werewolf/allow/java` path; use
`/etc/runtime-hardening/allow/SERVICE/java/GROUP` as above.

## Remaining behavior and build differences

`CLASSPATH` still applies without `-cp`/`-jar`; set the class path explicitly.
`JDK_AOT_VM_OPTIONS` still applies with `-XX:AOTMode=create`; `_JAVA_LAUNCHER_DEBUG`
still traces the launcher. The headless build omits splash-screen support.
The JRE has no `inline` paths: jshell and the source launcher (`java File.java`) are
the JDK's, a build tool's. Classes a program defines from bytes (generated classes,
custom loaders) remain available; `writable-source` covers class path entries, JARs
and agents. A program a Java program would run, and the VM's own command hooks,
need `exec`; Werewolf's `run` lists still apply on top.

Keep Wolfi's commit, patches, toolchain, JDK/JRE builds and JIT. Add
`--enable-headless-only` to omit desktop windows and X11 integration; retain
fontconfig, fonts and image codecs for server-side text and image rendering.
Fetch the locked commit from `jdk27u`, with mainline `jdk` as fallback.
Ship the JDK, JRE and default-jdk/default-jvm selectors. Omit demos, the separate
jmods package/build target and OCI-entrypoint compatibility/download; remove demo files.
Linkable-runtime builds and jlink tests remain. The JRE drops the remote debugger
(JDWP), the JMX remote agent, RMI naming and `rmiregistry`; java.rmi and
management-RMI remain for programs that link against them. The JDK keeps all.
Epoch is `1000 × policy revision + Wolfi epoch`; `wolfi-origin` preserves CVE matching.
Retained Wolfi tests and [probes](tests/probe.sh) check grants, manifests and refusals.
[Build instructions](../../Werewolf/docs/wolfi.md#building-locally); patches: GPL-2.0-only WITH Classpath-exception-2.0.
