# packages

Third-party application packaging for Werewolf. Each directory contains a Melange
recipe and its complete source context: patches, dependency locks and bootstrap
code. Production and intentionally vulnerable releases have separate directories
(for example, `solr` and `solr-vuln-demo`).

Forms, service restrictions, guest configuration and appliance tests remain in
`../werewolf/forms`. Patched language runtimes and libc live beside applications in this repository;
these recipes use the signed hardened feed.
Do not replace hardened runtimes with stock ones when porting dependencies.

## Optional native dependencies

The production Bugsink package removes Brotli, symbolic and milksnake. Its source
patch rejects `Content-Encoding: br` before reading the body and disables native
minidumps; gzip/deflate and ordinary Sentry events remain supported. Brotli file
event storage is unavailable, so installations using it need a data migration
before switching packages. The Werewolf form uses database event storage. CFFI
and pycparser remain for its compiled inotify adapter. Hash-locked build inputs
still include the removed distributions; they are uninstalled from the shipped
venv, whose Bugsink dependency metadata and patched-file hashes are updated.

n8n removes native Sentry profiling/stacktrace packages and Parcel's development
watcher, including platform variants. Upstream catches the missing optional
integrations. SQLite, isolated-vm, canvas/PDF backends and workflow integrations
remain. The pruning step refuses newly introduced consumers in the dependency
lock. `N8N_DEV_RELOAD` and native Sentry profiling are unavailable in this build.

Mastodon excludes Bundler's optional `opentelemetry` group. Configurations enabling
OTel exporters need a different package. The API gem required by ordinary runtime
dependencies remains. The reduced libvips profile retains JPEG, PNG, WebP, GIF,
EXIF and ICC support; optional document and specialist image loaders are omitted.
FFmpeg retains the selected codecs and local media processing, while omitting
network protocols, physical devices, display and font rendering integrations.
Installed-package format conversion and Rails/streaming startup checks pass.

Hardened Node omits Brotli and Zstd implementations, APIs and HTTP negotiation.
Gzip/deflate and HTTPS remain. Its installed-package tests cover unavailable
codecs and negotiation; n8n's native SQLite/isolate probes and Mastodon's
streaming server are exercised on this runtime. Removing `--shared-brotli` alone
would bundle Brotli; the maintained source patch instead removes the codec.

The HTTP(S)-only curl profile removes Brotli/Zstd, unused protocols and optional
authentication backends. Pi-hole uses a separate musl build inside its OCI tree;
its original curl and now-unused libraries are pruned by the form. Blocklist,
login and DNS checks cover first boot and restart.

Continuwuity omits Brotli/Zstd HTTP compression, its terminal admin console and
URL previews. Its database codecs remain for existing RocksDB data, and
memory-serve retains pure-Rust Brotli for embedded static assets. Build-time
dependency checks prevent the HTTP decoder and console/preview libraries from
returning. An installed-package check initializes RocksDB and serves the Matrix API.

This does not remove every Brotli implementation from every application. Font
and PDF engines (including n8n's native canvas backends) can retain independent
codecs, and imported OCI images need separate audits. FreeType's WOFF2 support
is retained because font consumers accept those formats. Werewolf needs Zstd
for boot images; databases may also need codecs to read existing data.
Vulnerable demonstration packages remain separate from production reductions.

Keep these repositories side by side:

```text
packages/
werewolf/
```

A form names a package from the signed feed below, as it names Wolfi's; Werewolf
images list the feed and refuse a name it covers from anywhere else:

```yaml
packages:
  - solr
```

Add a package only for what Wolfi does not ship, or ships unfit for a form (old,
bringing a shell, built for one CPU, or at a fixed address); keep Wolfi's APK name when replacing its package. Its header says why, and when it can go.

Build and inspect a package using Werewolf's existing QEMU or bubblewrap runner:

```sh
make PACKAGE=solr build
# Or use the sibling Werewolf builder, still from this repository:
WEREWOLF=/path/to/werewolf make PACKAGE=solr build
```

`ARCH=aarch64` or `ARCH=x86_64` selects an architecture. The shared command prepares
an ordinary Melange recipe and its complete source context before invoking the
sibling Werewolf builder. Its native build interface still uses the sibling's
vendor output/cache paths. The package wrapper itself is not a native recipe.

For form-specific local recipe handling, see
[Werewolf's contribution guide](../../../werewolf/CONTRIBUTING.md#package-sources).
The [package reference](package.md) covers direct builds, rendering and workspace paths.

## Automatic publication

`publish.yml` runs on main pushes, hourly retries and manual dispatch. It builds
unpublished recipe inputs natively on x86_64 and ARM64. A failed context leaves
other contexts free to publish. Pull requests run the publisher tests and parse
every recipe without access to release credentials. The catalog-driven PR matrix builds changed packages on both native architectures.

The signed application feed is `https://dist.werewolf-linux.org/apk/packages`,
with `x86_64/APKINDEX.tar.gz` and `aarch64/APKINDEX.tar.gz`. Its trust root is
`Werewolf/keys/packages.pub`. Intentionally vulnerable packages use distinct APK names:
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

- `WEREWOLF_PACKAGES_KEY`: private key matching `Werewolf/keys/packages.pub`.
- `R2_ACCESS_KEY_ID` and `R2_SECRET_ACCESS_KEY`: access to the `werewolf` bucket.

Allow deployments only from `main`. Optional repository variables
`WEREWOLF_X86_64_BUILD_RUNNER` and `WEREWOLF_ARM64_BUILD_RUNNER` are JSON arrays of
runner labels; defaults are `ubuntu-26.04` and `ubuntu-26.04-arm`. Self-hosted
builders are used for main, manually dispatched checks and pull requests from
this repository. Fork pull requests use hosted runners; external fork workflows
require maintainer approval before they can run. Unit checks and publication
planning use the configured amd64 runner too. Signing and R2 publication remain
on a separate hosted job in the protected release environment. Self-hosted
builders run as dedicated non-root accounts with subordinate UID/GID mappings,
using the same `Werewolf/runner/ci-root` and private bubblewrap profile as the Wolfi engine.
No signing or R2 keys are needed by the builder accounts.

```sh
make test check
go -C Werewolf run ./cmd/package plan --offline
# Rebuild/publish only one context, or retry its staged snapshot:
gh workflow run publish.yml -f package=onnx-minilm
# Exercise a context on both hosted architectures without publication:
gh workflow run check.yml --ref BRANCH -f package=CONTEXT
```

Published APKs can be installed from this feed by consumers using its public key.
Werewolf's current `recipes` forms still build their overlays locally; switching
those forms to published package dependencies is a separate migration.

## Tracking upstream releases

Each context's `package.yaml` update record declares a policy for every recipe context. `make test` fails if a
new context lacks tracking or a production package is marked pinned. Metadata is excluded from APK build identities, so editing a discovery source
does not invalidate an existing publication. The older Melange `update:` blocks remain portable upstream hints;
this repository's automation uses each context's `package.yaml` update record as its complete policy.

```sh
make updates                       # read-only discovery for every context
GH_TOKEN="$(gh auth token)" make updates  # use the existing GitHub login
make PACKAGE=solr updates           # discover one upstream release
Werewolf/.work/tools/package updates --engine upstream --write --package solr
Werewolf/.work/tools/package catalog validate  # offline coverage check
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

Each direct recipe declares installed checks in its native `melange.test` section.
The Go smoke runner installs the built APK and its runtime dependencies in a
disposable Melange runner. It installs mksh explicitly because hardened BusyBox
omits `/bin/sh`. Explicit test repositories are retained, so the Pi-hole curl
check uses Alpine's musl and TLS libraries. Checks include application version commands, Python
imports, n8n's native SQLite and isolate modules, model parsing and ONNX CPU
inference with invalid-shape and unknown-input rejection. The ONNX fixture comes
from Werewolf's form tests. APK installation uses unsigned local test indexes;
release artifact validation and signing still use the normal authenticated path.
Runtime checks run offline as UID 1000 with the installed root and test fixtures
mounted read-only, and writable scratch in `/tmp`. Python checks also prove inline
code and scripts on writable mounts remain refused. Langflow also checks SQLite
full-text search and offline MiniLM embeddings using the packaged model, with its
form's `python:native` allowance.
Other contexts retain native build and APK validation. These are package smoke
checks; full service, database migration and appliance security checks remain in
Werewolf's form suites.

Langflow omits the optional native Db2 driver, which IBM does not provide for
Linux ARM64. Its other IBM provider dependencies remain installed.

Ordinary Melange recipes remain compatible with external tools. For a packaging
change that needs only an epoch bump, use:

```sh
wolfictl bump databases/solr/solr.yaml --dry-run
wolfictl bump databases/solr/solr.yaml
```

The [current wolfictl bump](https://github.com/wolfi-dev/wolfictl/blob/main/docs/cmd/wolfictl_bump.md)
only increments epochs. Its exported recipe section helpers depend on Melange and
apko and do not implement this repository's release discovery or coordinated npm
and Python updates. The updater therefore uses Go's standard library plus the
same YAML parser as Melange, keeping wolfictl optional for manual maintenance.
For an upstream version change, use the Go updater so source and lock pins move
together, then commit and push through the regular publisher.

Bundled upstream components are listed under `components` in each context's `package.yaml` update record,
keyed by the recipe variable holding their version. They use the same stable
GitHub release resolver. For example, Vaultwarden's web vault is tracked
independently of its server. A component-only update refreshes its archive hash
and increments the parent's APK epoch; it does not regenerate the parent's
unchanged dependency lock. Subpackage pipelines are included. A `commit-var`
can additionally pin a component's release to its dereferenced commit (n8n's
launcher). Vulnerability demonstrations remain pinned, including components.

`wordpress-sqlite` packages WordPress's official SQLite integration release and
builds its database drop-in from the matching template. Werewolf keeps its
site configuration, installer and integration tests.

Werewolf's hourly `package-pins.yml` job verifies the signed `contexts/*/current.json`
records in this feed against a clean source snapshot. Every locked context and
subpackage must be published for both native architectures before it proposes
an immutable source commit and digest. Its regular check workflow tests the PR
and merges it only after the tests and form boots pass. Failed publication or
consumer tests leave the previous pins in place. No new publication mechanism
or R2 credentials are needed by the consumer.
