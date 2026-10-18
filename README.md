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

## Automatic publication

`publish.yml` runs on main pushes, hourly retries and manual dispatch. It builds
unpublished recipe inputs natively on x86_64 and ARM64. A failed context leaves
other contexts free to publish. Pull requests run the publisher tests and parse
every recipe without access to release credentials.

The signed application feed is `https://dist.werewolf-linux.org/apk/packages`,
with `x86_64/APKINDEX.tar.gz` and `aarch64/APKINDEX.tar.gz`. Its trust root is
`release/packages.pub`. Intentionally vulnerable packages use distinct APK names:
`solr-vuln-demo`, `n8n-vuln-demo` and `langflow-vuln-demo`; production packages keep
`solr`, `n8n` and `langflow`. Application paths and ports are unchanged.

Builders receive no release secrets. The publisher validates both native build
records, commit/run identities, APK metadata, payload hashes and index checksums
before signing. APKs and immutable snapshots upload before the signed feed
indexes. Conditional R2 writes preserve other publishers' additions. A signed
snapshot allows the next run to finish an interrupted publication without rebuilding.
Large APK payloads are streamed during validation and signing.

Bump `package.epoch` when changing a published recipe or its build context without
changing the upstream version. Published versions keep their first bytes;
conflicting rebuilds fail. Upstream releases and the deliberately vulnerable pins
are changed through reviewed recipe commits, not automatically replaced.

The `release` GitHub environment needs these secrets:

- `WEREWOLF_PACKAGES_KEY`: private key matching `release/packages.pub`.
- `R2_ACCESS_KEY_ID` and `R2_SECRET_ACCESS_KEY`: access to the `werewolf` bucket.

Allow deployments only from `main`. Optional repository variables
`WEREWOLF_X86_64_BUILD_RUNNER` and `WEREWOLF_ARM64_BUILD_RUNNER` are JSON arrays of
runner labels; defaults are `ubuntu-26.04` and `ubuntu-26.04-arm`. Self-hosted
builders run as dedicated non-root accounts with subordinate UID/GID mappings,
using the same `scripts/ci-root` and private bubblewrap profile as hardened-packages.
No signing or R2 keys are needed by the builder accounts.

```sh
make test check
python3 scripts/plan.py --offline
# Rebuild/publish only one context, or retry its staged snapshot:
gh workflow run publish.yml -f package=onnx-minilm
```

Published APKs can be installed from this feed by consumers using its public key.
Werewolf's current `recipes` forms still build their overlays locally; switching
those forms to published package dependencies is a separate migration.
