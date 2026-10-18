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
every recipe without access to release credentials. Both hosted architectures also
build the small ONNX model package as a real native runner smoke test.

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
conflicting rebuilds fail. Stable upstream releases are tracked and committed by
`update.yml`; deliberately vulnerable pins stay unchanged.

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

## Tracking upstream releases

`updates.json` declares a policy for every recipe context. `make test` fails if a
new context lacks tracking or a production package is marked pinned. Policies live
outside the build contexts so changing a discovery source does not invalidate a
published APK. The older Melange `update:` blocks remain portable upstream hints;
this repository's automation uses `updates.json` as its complete policy.

```sh
make updates                       # read-only discovery for every context
make PACKAGE=solr updates           # discover one upstream release
go run ./cmd/package-update --write --package solr
go run ./cmd/package-update --validate  # offline coverage check
```

The Go updater uses GitHub's stable releases or numeric release tags, non-yanked
PyPI releases, npm's `latest` channel and Mojang's stable server manifest. Numeric
comparison handles versions such as `26.3` and `1.10.0`; previews, drafts and
downgrades are rejected. Tag sources paginate instead of assuming tag order is
version order. Continuwuity and Nextcloud use tags because their GitHub latest
release metadata can lag their stable tags. GitHub requests accept `GH_TOKEN` to
avoid anonymous rate limits; the token is never sent to download hosts or child
processes. endoflife.date is useful for lifecycle information, but direct release
sources also cover the applications it does not track.

The explicitly versioned `neo4j-4.4` package follows stable patches in the 4.4
stream. BloodHound requires APIs removed in newer Neo4j majors; migrating that
form is separate from maintaining its named compatibility package. A stream
policy requires an explicit reason and still checks upstream for new releases.

New versions reset the APK epoch to zero. Every version-dependent archive hash,
architecture-specific fetch and Git checkout commit is refreshed. npm locks are
regenerated without installing packages or running lifecycle scripts. Bugsink's
hashed dependency lock is resolved with uv. Langflow's lock and three wheel pins
are resolved together, with the existing Python overrides and separate fastuuid
build retained. Resolver and recipe validation failures leave that context's files
untouched. Other contexts can still update and the run reports each failure.

The ONNX model has no upstream release number: its archive is checked for changed
content and a change increments the epoch. Postdoc currently has no stable release
tags, so its explicit snapshot policy follows `main`, pinning the commit and date;
multiple commits on one date increment the epoch. All three `*-vuln-demo` contexts
remain pinned and are skipped before any network access or filesystem write.

Hourly and manual `update.yml` runs test the updater, prepare complete contexts,
commit each successful update and push to main without force. They explicitly
dispatch `publish.yml`, since pushes made with `GITHUB_TOKEN` do not trigger push
workflows. Both native builds and their smoke tests must pass before the existing
publisher signs and uploads APKs to R2. Failed updates are recorded in the
`upstream-update-report` artifact and make the update run fail after other updates
have been committed. Release credentials remain confined to the publisher.

`tests/smoke.json` describes runtime checks for sixteen contexts. The Go smoke
runner installs the built APK and its runtime dependencies in a disposable
Melange bubblewrap runner. Checks include application version commands, Python
imports, n8n's native SQLite and isolate modules, model parsing and ONNX CPU
inference with invalid-shape and unknown-input rejection. The ONNX fixture comes
from Werewolf's form tests. APK installation uses unsigned local test indexes;
release artifact validation and signing still use the normal authenticated path.
Other contexts retain native build and APK validation. These are package smoke
checks; full service, database migration and appliance security checks remain in
Werewolf's form suites.

Ordinary Melange recipes remain compatible with external tools. For a packaging
change that needs only an epoch bump, use:

```sh
wolfictl bump solr/solr.yaml --dry-run
wolfictl bump solr/solr.yaml
```

The [current wolfictl bump](https://github.com/wolfi-dev/wolfictl/blob/main/docs/cmd/wolfictl_bump.md)
only increments epochs. Its exported recipe section helpers depend on Melange and
apko and do not implement this repository's release discovery or coordinated npm
and Python updates. The updater therefore uses Go's standard library plus the
same YAML parser as Melange, keeping wolfictl optional for manual maintenance.
For an upstream version change, use the Go updater so source and lock pins move
together, then commit and push through the regular publisher.
