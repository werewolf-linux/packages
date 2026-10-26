# Using hardened packages

Package guides list every patch, allowance and packaging change. “Always denied”
means no allowance restores the feature; “always on” means mandatory behavior.

## Install

Werewolf's minimal form includes the signed hardened feed. Add the package names
from its guide to your form's `packages`; rebuild and deploy the image. Explicit
older streams and runtimes inside OCI images require separate migration.

On Wolfi, configure the feed while building the image. From a trusted checkout:

```sh
install -m 0644 release/packages.pub /etc/apk/keys/werewolf-packages.rsa.pub
printf '%s\n' '@hardened https://dist.werewolf-linux.org/apk/hardened' >> /etc/apk/repositories
apk update
```

Then run the guide's `apk add` command. Keep Wolfi's repository for dependencies.
Both `x86_64` and `aarch64` are published. Select a stream present in the signed
index; examples reflect the recipes in this checkout, not a release guarantee.
Pin covered packages to versions from that index and verify their control checksums
in image validation. Repository order, tags and higher epochs alone cannot prevent
fallback to a newer Wolfi build. Werewolf's builder performs that validation.

Run PHP, Python, Java and Node with a nonzero **effective UID**, including embedded
runtimes and PHP-FPM's master. There is no production root allowance. The private
`/etc/runtime-hardening/build-root` build marker must never enter a deployed image.
Mount application code read-only; `chmod 0444` on a writable mount is insufficient.

## Grant an allowance

In Werewolf, add a documented `RUNTIME:GROUP` to the existing service:

```yaml
services:
  worker:
    allow: [python:native]
```

This is a fragment: retain the service's other settings. Top-level `allow` grants
image-wide access. `--services.worker.allow python:native` is the CLI equivalent.
Service grants apply to the machine's root, not a service's separate OCI/root tree.

On Wolfi, create an empty regular file in the image before starting the runtime:

```sh
install -d -m 0755 /etc/runtime-hardening/allow/worker/python
install -m 0644 /dev/null /etc/runtime-hardening/allow/worker/python/native
```

Replace `worker`, `python` and `native` with the service, runtime and group.
The service is the last component of the process's `/proc/PID/cgroup`, not its UID.
Use `all` instead of the service for an image-wide grant, including containers whose
cgroup namespace exposes only `/`. Symlink allowance files do not grant access.
Keep the entire allowance tree administrator-owned and read-only to the service;
the checks rely on the image and supervisor to protect it and the cgroup identity.

Werewolf validates names, while each package defines which grants have an effect.
Alternatively, put the marker under your form's `rootfs/etc/runtime-hardening/allow/`.
Rebuild, deploy and restart after changing grants. Remove the file or declaration
and redeploy to revoke; restart also clears cached decisions and loaded native code.
An allowance does not override PHP settings, JVM/Node permissions, filesystem
permissions, Werewolf leash rules, seccomp, network policy or missing extensions.

## Debug a refusal

Capture the process's stderr; Werewolf routes it to the console. Runtime denials
write JSON directly to descriptor 2, even when exceptions are caught. Filter a
captured stream with:

```sh
jq -R 'fromjson? | select(.event == "runtime-denial")' service.stderr
```

Read `runtime`, `group`, `operation` and `detail`; match the package's table before
granting anything. `root`, PHP's `source`, and fixed-policy log groups are diagnostic
labels, not necessarily allowance names. Records also include time, PID, effective
UID and argv. Details are limited to 192 bytes (characters in Java), argv to 256;
`truncated` marks clipping. Underlying stderr redirection redirects these records.

If a grant fails, check the affected process's cgroup, marker path/type, deployed
image and restart. For source failures, inspect the actual source mount, including
symlink targets (`findmnt -T /path/to/script` on a debugging host). If no runtime
denial appears, inspect the ordinary error and OS/service policy. glibc and libffi
use ordinary loader/library errors rather than these runtime JSON records.

Build and package probes: [repository instructions](../README.md#building-locally).
