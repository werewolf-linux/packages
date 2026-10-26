# Werewolf packages

Applications, libraries and hardened runtimes in one categorized ports tree.
Every `CATEGORY/NAME/` is a complete build context with one maintained
[`package.yaml`](docs/package.md). It contains a native Melange recipe or guarded
Wolfi derivation, one automatic update policy, tests and maintenance evidence.

```text
devel/glibc/       lang/python/      net/curl/
devel/libffi/      lang/php/         www/caddy/
                  lang/node/        www/n8n/
                  lang/openjdk/     multimedia/plex/
```

Names are globally unique and stable across category moves. APK names remain
compatible with Wolfi where we replace its packages. All commands run from the
repository root; shared mechanics live in `internal/`, `pipelines/` and `tools/`.

```sh
make tools
build/tools/package catalog list
build/tools/package new --repo OWNER/PROJECT www/example
build/tools/package check --package caddy
build/tools/package render --package caddy
build/tools/package updates --package caddy
build/tools/package updates --package caddy --write
build/tools/package discover --package python
make PACKAGE=python build
make test lint check
```

[Package definitions and maintenance](docs/package.md) is the format and workflow
reference, including defaults, helper actions, guarded edits, generated paths and
CI planning. [Application packaging](docs/applications.md) covers dependency locks
and service consumers. [Wolfi packages](docs/wolfi.md) covers runtime policy,
publication and native/guest build requirements. [Feed usage](docs/usage.md) covers
installation, capability grants and denial logs.

Tooling requires Go 1.27+, git, pinned Melange and package-specific test tools.
Install the builder with `build/tools/package install-melange`. Native builds use
Linux bubblewrap; supported Wolfi builds can use QEMU guests. Local application
builds may use a sibling Werewolf checkout by setting `WEREWOLF=/path/to/werewolf`.
Plex provenance checks also require GnuPG.

Both engines build x86_64 and aarch64 independently. CI discovers packages from
their definitions and builds changed contexts without a maintained package list.
Scheduled discovery checks all upstream sources. Captured Wolfi inputs are shared
by both architecture jobs. Failed packages retain their previous publication and
do not prevent other packages from publishing.

The application feed is `https://dist.werewolf-linux.org/apk/packages`; hardened
Wolfi derivations use `https://dist.werewolf-linux.org/apk/hardened`. Both retain the
trust root in `release/packages.pub`. Signing credentials stay in the `release`
environment; application builders receive no signing credentials. Signed Wolfi
packages retain their installed-test and recovery gates.

All generated work lives under ignored `build/`. `package paths` reports exact
locations. Dependency manifests and locks keep their native ecosystem formats;
JSON reports are machine output, while maintained package definitions are YAML.

Original application recipes and tools retain [Apache-2.0](LICENSE). Imported
Wolfi engine tooling retains [GPL-3.0](LICENSES/wolfi/LICENSE); patches retain their
upstream licenses. [Import provenance](LICENSES/wolfi/IMPORT.json) and associated
license notices remain under `LICENSES/wolfi/`.
