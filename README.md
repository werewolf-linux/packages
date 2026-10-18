# packages

Third-party application packaging for Werewolf. Each directory contains a Melange
recipe and its complete source context: patches, dependency locks and bootstrap
code. Production and intentionally vulnerable releases have separate directories
(for example, `solr` and `solr-vuln-demo`).

Forms, service restrictions, guest configuration and appliance tests remain in
`../werewolf/forms`. Patched language runtimes and libc remain in
`../hardened-packages`; these recipes use that project's signed hardened feed.
Do not replace hardened runtimes with stock ones when porting dependencies.

Keep these repositories side by side:

```text
packages/
werewolf/
hardened-packages/
```

A form selects recipes with paths relative to its directory:

```yaml
recipes:
  - ../../../packages/solr/solr.yaml
```

Build and inspect a package using Werewolf's existing QEMU or bubblewrap runner:

```sh
make PACKAGE=solr build
# Or from ../werewolf:
howl build-apk ../packages/solr/solr.yaml
```

`make check` parses every recipe with Melange. `make PACKAGE=NAME check` checks
one context. `ARCH=aarch64` or `ARCH=x86_64` selects an architecture for a build.
Package outputs and caches stay in `../werewolf/build/vendor`. Form builds discover
these declared recipes, build them, check their shared-library dependencies and
include the resulting files. Changes to a context invalidate the image cache.

Distro-specific packages remain in Werewolf alongside their source, including
`forms/vuln-demo/melange` (the Go console and SSH server) and
`boot/tpm-stub.melange.yaml` (the measured EFI stub builder). The static-trampoline
libffi hardening port is in `../hardened-packages/ports/libffi`.

CI checkout and reviewed source pins are described in
[Werewolf's contribution guide](../werewolf/CONTRIBUTING.md#package-sources).
The recipes use Werewolf's Apache-2.0 license; packaged upstream code retains
the license declared by each recipe.
