# Werewolf packages

Applications, libraries and hardened runtimes in one categorized ports tree.
Each `CATEGORY/NAME/` is a complete build context. Names are unique across
categories and remain stable when directories move. APK names remain compatible
with Wolfi where we replace its packages; Caddy is `caddy`.

```text
devel/glibc/       lang/python/      net/curl/
devel/libffi/      lang/php/         www/caddy/
                  lang/node/        www/n8n/
                  lang/openjdk/     multimedia/plex/
```

## Maintain a package

Every context has one [metadata.json](docs/metadata.md) describing its automatic
update source, hardening evidence and divergence from Wolfi. CI rejects missing
sources, unsupported handlers, unexplained divergences and invalid evidence.
Applications track upstream releases; Wolfi overlays capture its exact recipes
and apply small checked changes. Deliberately vulnerable demonstrations retain
real source metadata and an explicit update hold.

```sh
python3 scripts/catalog.py list
python3 scripts/catalog.py show caddy
make metadata                     # offline maintenance assertions
make test check                   # tests and Melange recipe validation
make PACKAGE=caddy updates        # discover an upstream release
make PACKAGE=php updates          # capture a Wolfi snapshot
make PACKAGE=caddy build          # build through ../werewolf
make PACKAGE=php build            # build the Wolfi derivation
```

Python 3.12+ with PyYAML, Go 1.27+, pinned Melange and package-specific test tools
are required. Plex provenance checks also require GnuPG (`gpg`). Create a local
environment with a supported interpreter before running the tests:

```sh
python3.12 -m venv build/venv
build/venv/bin/python -m pip install -r wolfi/requirements.txt
make PYTHON=build/venv/bin/python test check
```

Local application builds also need the sibling Werewolf checkout.
[Application packaging](docs/applications.md) covers dependency locks and native
smoke tests. [Wolfi builds](wolfi/README.md) covers capture, derivation, patching,
installed-package probes and local build tools.

## Publication

Both engines build and validate x86_64 and aarch64 independently for each context.
Failed contexts retain their previous publication and do not stop other packages.
The application feed remains `https://dist.werewolf-linux.org/apk/packages`; Wolfi
overlays use `https://dist.werewolf-linux.org/apk/hardened`. Both use the trust root
in `release/packages.pub`. Signing credentials stay in the `release` environment.
Application builders receive no signing credentials. Wolfi package tests require
signed artifacts and retain their existing signing gates.

Hourly `update.yml` updates application source pins and dispatches `publish.yml`.
Every ten minutes `wolfi-refresh.yml` captures Wolfi changes and builds unpublished
inputs through `wolfi-build.yml`. Pull requests run metadata checks, both engines'
unit tests, recipe validation and Wolfi patch discovery. A directory move does
not change a package's public release identity.

## Layout and licensing

Top-level categories describe what software does. Update origin and hardening
status are metadata, so adding hardening never requires moving a package.
`scripts/` holds shared tools and the application publisher; `wolfi/` holds the
Wolfi derivation engine. Tool pins, runner helpers and the signing trust root each
have one maintained copy. Build outputs stay under ignored `build/` directories.

The application recipes and original tools retain [Apache-2.0](LICENSE).
Imported Wolfi engine tooling retains [GPL-3.0](wolfi/LICENSE); patches retain the
licenses of the code they modify, recorded under `wolfi/LICENSES`. Packaged
upstream code retains its declared license. [Import provenance](wolfi/IMPORT.json)
records the source repository and commit.
