# Package definitions and maintenance

Every `CATEGORY/NAME/package.yaml` is the maintained entry point for one package.
It declares how to build, test and update the package, why we maintain it, and the
files supporting any hardening claims. Commands run from the repository root.

The directory supplies the stable context name. Melange supplies APK names; they
may differ from the context name and must remain compatible with their consumers.
Moving a context between categories does not rename its public releases.

## Files and sections

```text
www/example/
  package.yaml
  patches/          # when patches are needed
  tests/            # declared probes and fixtures
  package-lock.json # ecosystem manifests and locks keep their standard names
  README.md         # optional installation and usage guidance
```

Exactly one of `melange` and `wolfi` is required. Their presence selects the engine.
There are no package lists, engine flags or smoke-test registries to update.

| Section | Meaning |
| --- | --- |
| `update` | The single release-discovery policy. |
| `melange` | A native Melange recipe, including environment, pipelines, subpackages and tests. |
| `wolfi` | A Wolfi baseline selector, downstream revision and guarded derivation. Used instead of `melange`. |
| `changes` | Records with `what`, `why`, `evidence` and optional positive `claims`. |
| `replaces` | Optional Wolfi recipe names replaced by a directly maintained recipe. |
| `drop-when` | The condition for retiring our separate packaging. |
| `actions` | Optional reviewed helper operations, invoked explicitly by a pipeline. |
| `checks` | Explicit offline contributor checks, including package-local Go tests. |
| `test` | Optional installed-test harness controls: `confined`, `allow`, `preserve-mtimes`. |

Use kebab-case for wrapper fields. The loader rejects unknown fields, duplicate
keys, aliases, multiple documents and conflicting build sections. Native Melange
fields are checked by pinned Melange rather than a parallel implementation.
[package.schema.json](../package.schema.json) is generated from the wrapper loader.
All definitions include an editor directive pointing to it. Regenerate it with
`Werewolf/.work/tools/package schema --dest Werewolf/package.schema.json`; `make lint` checks it.

## A native recipe

```yaml
update:
  github:
    identifier: example/project
    strip-prefix: v
melange:
  package:
    name: example
    version: 1.0.0
    epoch: 0
    description: Example application
    copyright:
      - license: MIT
  environment:
    contents:
      packages: [busybox, make]
  pipeline:
    - uses: git-checkout
      with:
        repository: https://github.com/example/project
        tag: v${{package.version}}
        expected-commit: REPLACE_WITH_REVIEWED_40_CHARACTER_COMMIT
    - runs: make install DESTDIR=${{targets.destdir}}
  test:
    pipeline:
      - runs: example --version
changes:
  - what: Build example for Werewolf.
    why: Provide the application required by its consumers.
    evidence: [package.yaml]
```

The `melange` mapping uses Melange's own configuration language. Use native
`uses`, `with`, `runs`, variables, resources, dependencies and tests wherever they
fit. Shared operations can be reusable pipelines under `Werewolf/pipelines/`. Ordinary
supporting files remain relative to the package context; fixtures for installed
application tests are copied from its `tests/` directory.

`update` belongs only at the document root. The renderer exports native build
configuration without a competing release-discovery policy.

## Release discovery

GitHub releases use `github.identifier`, `strip-prefix`, and `strip-suffix`.
`use-tag: true` selects tags instead of GitHub releases. Keep release discovery
separate from source checkout: forks and upstream release repositories may differ.

Other adapters use `source`: `pypi`, `npm`, `minecraft`, `git`, `content`, `plex`,
`openjdk` and `git-tags` where supported by their engine. Registry adapters name
`project`; git snapshots name `repo`, `branch` and a reason. `git-tags` uses an
HTTPS `url`. Version normalization fields are `separator`, `version-pattern` and
`version-replace`; replacements use Go's `${1}` capture syntax. `pattern` and
`series` retain their existing application compatibility semantics.

`components` declares separately updated bundled components. Each component
names its source and optional `commit-var`. `lock` selects an existing dependency
resolver (`npm`, `bugsink` or `langflow`). A complete update prepares source pins,
checksums and dependency locks in isolation, validates the result, and installs
successful work. Failures are reported per package and retain the last publication.

Only `*-vuln-demo` packages may set a nonempty `hold` explanation. They keep their
real source policy and skip network discovery. Production packages cannot opt out.
Plex retains signed vendor indexes and checked codec ABI inputs beside its definition.

## A Wolfi derivation

```yaml
update:
  github:
    identifier: libffi/libffi
    strip-prefix: v
wolfi:
  recipe: libffi
  revision: 6
  sources:
    - https://github.com/libffi/libffi.git
  edits:
    - path: [pipeline, uses=autoconf/configure, with, opts]
      replace:
        - old: --enable-pax_emutramp
          new: --disable-pax_emutramp
        - old: --disable-exec-static-tramp
          new: --enable-exec-static-tramp
changes:
  - what: Enable static executable trampolines.
    why: Allow callbacks without writable executable memory.
    evidence: [package.yaml, tests/probe.c]
```

This example shows the shape; the actual libffi definition also checks the retained
pipeline, package set and installed behavior. Declare exactly one baseline `recipe`
or `family`. A family follows the newest stable recipe. `update` independently
selects the latest upstream stable release, including new major releases.

Captured Wolfi recipes, upstream files and pipelines remain unchanged. The lock
records their repository commit, version, epoch and source pin separately from the
selected upstream release. Derivation checks expectations and produces a complete
native recipe. An overlay describes a delta; use `render` to inspect all inherited
steps in the effective build.

`revision` is the downstream packaging revision. The default APK epoch is
`revision * 1000 + upstream_epoch`; the existing glibc exception explicitly uses
`epoch-multiplier: 1`. Preserve that arithmetic and bump the revision for changes
to patches, packaging, tests or builder behavior. Direct recipes instead bump
`melange.package.epoch`. New upstream application versions reset their epoch to zero.
Published APK identities are immutable.

Additional options retain their explicit meaning: `sources` supplies ordered source
mirrors; `stream-parts` controls versioned family names; `resources` sizes the runner;
`min-kernel` is the build-runner kernel floor. Test exceptions such as
`max-fail-per-mille`, `test-unpinned`, `test-packages` and `origin-main-only` are
explicit policy, never inferred from a package name.

## Changes and claims

Write explanations once in `changes`. Evidence paths must be existing regular
files inside the context. Optional `claims: [minimized]` or `claims: [pledge]`
attaches an explicit claim to its explanation and evidence. Missing claims mean
no claim has been established; they do not assert the absence of a property.

`minimized` means identified unused features or entrypoints are deliberately
omitted. Compiler flags alone do not establish minimization. `pledge` means the
package implements explicit, default-deny capability grants. A service's Werewolf
confinement configuration does not establish a package-level pledge claim.
Installed-package tests remain responsible for checking the claimed behavior.

## Contributor commands

```sh
make tools
Werewolf/.work/tools/package new --repo OWNER/PROJECT www/example
# Or start from a reviewed local Wolfi recipe:
Werewolf/.work/tools/package new --repo OWNER/PROJECT --from-wolfi /path/to/example.yaml devel/example
Werewolf/.work/tools/package check --package example
Werewolf/.work/tools/package render --package example
Werewolf/.work/tools/package updates --package example
Werewolf/.work/tools/package updates --package example --write
Werewolf/.work/tools/package build --package example --arch aarch64
Werewolf/.work/tools/package test --package example --arch aarch64
```

Scaffolding creates a deliberately incomplete native recipe that stops before
building until its source, license and build steps are reviewed. It never overwrites
an existing context or duplicates an existing package name. Wolfi scaffolding copies
the reviewed recipe into `tests/fixtures` and generates source/pipeline assertions.

`check` is offline: it checks all maintenance contracts and validates native recipes
or reviewed Wolfi fixtures. Downloading pinned build sources remains part of a build.
`refresh` explicitly selects and captures new Wolfi inputs. `discover` additionally
checks derivation and patches and produces a transferable input archive. `build`,
`render`, `derive`, `verify` and `prepare` do not silently discover new releases;
restore or refresh inputs first. Rendered output uses native Melange syntax; builds
also need the appropriate source context and any declared helper operations.

For local application builds through a sibling Werewolf checkout, set
`WEREWOLF=/path/to/werewolf make PACKAGE=example build`. Otherwise the common builder
uses native Linux bubblewrap, or the supported Wolfi guest runner.

## Installed tests

Ordinary application tests live in `melange.test` and subpackage test sections, including their extra packages.
`package test` installs the built APKs in a disposable test root and runs that native
configuration. It uses package-local fixtures; genuinely shared runtime checks live
under `Werewolf/pipelines/tests` and are referenced as `shared-tests/FILE`.

Select one context, a category, or the whole tree with the same Go command:

```sh
Werewolf/.work/tools/package test --package caddy
Werewolf/.work/tools/package test --category www
Werewolf/.work/tools/package test
```

Add `--build` to build missing APK sets first. Complete existing sets are checked
for the selected names, versions and architecture before being tested again.
`--build --fresh` rebuilds the selected architecture's generated work. Wolfi
inputs must already be captured; testing never selects newer sources implicitly.
The equivalent Make targets build missing packages and save a JSON report:

```sh
make test-packages PACKAGE=caddy
make test-packages CATEGORY=www
make test-packages
```

The batch continues after individual failures and exits unsuccessfully if any
suite fails. `--report FILE` saves every selected context, including pending or
cancelled work, and updates it after each package. Each context has a `test.log`
under the logs directory reported by `package paths`. `--json` emits the final
report to standard output. Build and test subprocesses receive no publication
credentials. Successful batch runner roots are removed; failed roots remain
available for diagnosis. `make test` runs the framework and contributor checks.

`test.confined: true` runs shell probes as UID 1000 with no capabilities, read-only
runtime and fixtures, and writable `/tmp`. Build/test allowances belong only to this
disposable environment. `allow` lists explicit supported runtime grants, while
`preserve-mtimes` restores reviewed archive timestamps where runtime validation needs
exact package bytes and timestamps. These settings are never installed into APKs.

Wolfi retains the baseline tests and the checked extra test steps in its derivation.
`test-setup` affects retained recipe tests; `suite-setup` and `test-edits` affect the
installed-package test recipe only.

## Generated paths and CI

All generated work lives under `Werewolf/.work/`:

```text
Werewolf/.work/tools/                         # pinned tools
Werewolf/.work/cache/                         # reusable downloads and compiler caches
Werewolf/.work/bundles/NAME.tar.gz            # captured inputs transferred between CI jobs
Werewolf/.work/packages/NAME/INPUT-ID/inputs/ # shared verified Wolfi capture
Werewolf/.work/packages/NAME/INPUT-ID/ARCH/   # isolated mutable workspace and artifacts
```

For Wolfi, `Werewolf/.work/packages/NAME/current` records the selected captured input ID.
Each architecture verifies a private input copy before deriving its own context.
Direct recipe IDs bind their recipe, supporting files, modes and relevant builder
implementation. Commentary and `changes` text do not rebuild APKs; full definitions
remain in reviewed source and Wolfi provenance. JSON reports and ecosystem locks
remain machine formats; maintainers edit YAML definitions.

`package paths --package NAME --arch ARCH` reports actual build, input, artifact and
log locations as JSON. `--github` also emits Actions outputs. Workflows use this
contract instead of constructing paths independently.

`package plan --json --base BASE --head HEAD` plans changed-package validation for
both engines and both architectures. Without a base it selects all requested packages.
Changes to shared framework inputs conservatively select all packages. Documentation
changes still receive definition checks but do not select native builds. Scheduled
updates check all upstream sources. Consumer dependency selection can later narrow
shared-runtime validation without adding a manually maintained package registry.

PR checks use reviewed fixtures for offline validation. A separate capture job
explicitly resolves each selected Wolfi package and archives one immutable snapshot
before either architecture builds. Native jobs restore that snapshot; they do not
resolve upstream independently. Replaying an input archive reproduces that selection;
a fresh capture may select newer upstream input. Both engines retain independent
package failures, signing gates, immutable publication and release recovery.

Contributor checks outside the maintenance module are declared explicitly:

```yaml
checks:
  - name: Package helper regression tests
    working-directory: tests
    runs: go test helper_test.go
```

These commands run from the package directory, or the declared package-local working
directory, with publication credentials removed. `make test` runs framework tests
with `go -C Werewolf test ./...`, then runs all declared `checks`. Use
`Werewolf/.work/tools/package checks --package NAME` to run one package's checks.
Installed-package tests remain in native Melange test sections.

Run `make test lint check` before submitting. Architecture-specific package builds
and installed tests require their native Linux runner or supported guest setup.

## Guarded edit reference

Paths walk mapping keys and sequence items. `name=Configure` and
`uses=autoconf/configure` select exactly one item. Ambiguous selectors fail.
Use a sequence index only when a step has no unique name or pipeline identifier.

| Field | Effect |
| --- | --- |
| `expect` | Require an exact YAML value before editing it. |
| `absent: true` | Require a missing field before adding it. |
| `exists: true` | Require a field whose contents are intentionally discarded. |
| `set` | Replace the selected value. |
| `delete: true` | Delete a field or an item selected by its unique identity. |
| `remove` | Remove each listed sequence value exactly once. |
| `append` or `prepend` | Add sequence items. |
| `before` or `after` | Place appended items beside a sequence selector. |
| `replace` | Change literal text; each replacement expects one occurrence by default. |
| `shape` | Require the sequence's ordered names and pipeline identifiers. |
| `join` | Combine shell steps from a `from: name=...` selector to the end into one named step, preserving their package needs and adding an optional shell `prefix`. |

Put baseline assertions in `checks` and changes in `edits`. Keep a literal edit
close to its expectation so a reviewer can see both sides of the change.
Replacement entries have `old`, `new`, and an optional positive `count`.

`context` explicitly maps package directories to destinations in the captured build context. Identity mappings remain explicit to avoid unintended collisions with upstream files.
`patches` names the copied patch directory. Its `series` must list every numbered
patch once, with the required provenance headers.

To refresh the context of a Wolfi patch, keep the refreshed file under the package's
`upstream-patches/` directory and declare the captured file's original SHA-256:

```yaml
upstream-patches:
  example.patch:
    sha256: ORIGINAL_WOLFI_PATCH_SHA256
    source: upstream-patches/example.patch
```

Capture stops for review if Wolfi changes the original patch. Captured inputs stay
unchanged; derivation copies the refresh into the build context, and preparation
uses the same file. Preserve the patch's code changes when refreshing context.

`context-edits` and `prepared-edits` apply guarded file replacements after copying
the build context or preparing source. They use the same file fields as
the `actions` section, with overlay version substitutions available. Captured
upstream files remain unchanged.

### File includes and test setup

`${file:tests/probe.sh}` includes an existing package file without maintaining a
second copy. Ordinary file includes reject Melange interpolation; use the
explicit `${template:PATH}` form for a reviewed recipe fragment that needs it.
Includes must stay inside the package directory.

`test-setup` prepends a step to each retained package test. `suite-setup` does the
same only in the recipe used to run tests. `test-edits` makes other suite changes.
Build and test permissions must never be installed into an APK.

Explicit substitutions include `${lock.source_commit}`, `${lock.version}`,
`${lock.feature}`, `${upstream.name}`, `${upstream.tag}`,
`${policy.max_fail_ratio}`, and `${test.repository}`. Melange's `${{...}}`
expressions and ordinary shell variables keep their existing meaning.

Add a shared operation in Go when several packages need the same behavior.
Keep package choices, commands, and expected upstream values in YAML.


## Helper actions

Declare exceptional reviewed file transformations under `actions` in the same
package definition. Invoke `werewolf-tools/package-helper --action NAME --root PATH`
from a declared pipeline. The helper reads `package.yaml` by default. It checks an
entire action before writing files and rejects paths outside its action root.
Actions support guarded `files`, filtered hashed `requirements`, wheel dependency
edits with updated `RECORD` hashes, and the existing shared curl/glibc probes.
The builder installs the helper only into the build context, outside APK paths.

