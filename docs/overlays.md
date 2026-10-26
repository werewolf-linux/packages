# Wolfi YAML overlays

A Wolfi package keeps its policy in `overlay.yaml`. The common Go engine captures
an unchanged Wolfi recipe, checks the overlay's expectations, and applies its
edits. Start with a nearby package; ImageMagick and libffi are small examples.

`metadata.json` selects the Wolfi recipe or family and the independent upstream
release source. `policy.json` holds the policy revision. Raise that revision when
packaging, patches, or probes change behavior.

## Preview a change

Run an overlay against its offline recipe fixture:

```sh
go run ./cmd/package fixture --package imagemagick \
  --fixture graphics/imagemagick/tests/fixtures/imagemagick-7.yaml
```

The command prints the derived recipe. An unexpected upstream value stops the
command and identifies the edit and path that need review.

```sh
go test ./...
make lint
```

The regression snapshots in `internal/overlay/testdata` capture the previous
engine's outputs and reviewed expectations for new packages. Tests compare complete derived recipes, check that the captured
baseline stays unchanged, and reject changed source pins and new build steps.

## Describe the package changes

The engine handles exact source fetching, patch ordering, epoch calculation,
`upstream-origin` metadata, and disabling Wolfi's update automation. An overlay
supplies source mirrors and the package's changes:

```yaml
title: Example
sources:
  - https://github.com/example/example.git
edits:
  - path: [environment, contents, packages]
    remove: [unused-library-dev]
  - path: [pipeline, uses=autoconf/configure, with, opts]
    expect: --enable-optional-loader
    set: --disable-optional-loader
```

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

`context` maps package directories to destinations in the build context.
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
`packaging/changes.yaml`, with overlay version substitutions available. Captured
upstream files remain unchanged.

## Reuse probes and test setup

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

## Change application files

When a build needs reviewed edits to downloaded files, put actions in
`packaging/changes.yaml`:

```yaml
actions:
  source:
    files:
      - path: module/settings.py
        require-contains: import json
        replace:
          - old: ENABLE_OPTIONAL_PROVIDER = True
            new: ENABLE_OPTIONAL_PROVIDER = False
```

Invoke `werewolf-tools/package-helper --action source --root SOURCE` in the
recipe. The builder compiles one static Linux helper for the target architecture
and copies it into the build context. It stays outside installed package paths.
The helper checks every expectation before writing any changed files, and
rejects paths outside the action root. Unknown YAML fields fail validation.

Actions also support `requirements` for omitting complete hashed requirement
blocks, and `wheels` for removing optional dependencies while updating wheel
`RECORD` hashes. See Langflow, Open WebUI and Bugsink for examples. Use `required`
omissions to make an upstream dependency change require review.

The shared curl probe runs local HTTP/TLS fixtures; its YAML options describe
allowed features and optional musl linkage checks. Glibc's actions generate its
explicit locale fixtures and run its compiled C/C++ policy probes. Direct Python
interpreter/API probes and Python application launchers remain in Python.

For native libc analysis, commands run from the repository root:

```sh
go run ./cmd/package probe glibc --build BUILD --report build/analysis/policy.json
go run ./cmd/package probe glibc-compare --output build/analysis/results.json \
  BASELINE-TESTRESULTS.apk CANDIDATE-TESTRESULTS.apk
go run ./cmd/package probe glibc-benchmark --rounds 9 \
  --baseline-libc BASELINE-LIBC.apk --baseline-loader BASELINE-LOADER.apk \
  --candidate-libc CANDIDATE-LIBC.apk --candidate-loader CANDIDATE-LOADER.apk \
  --output build/analysis/benchmark.json
```

Comparison and benchmarking inspect explicitly selected APKs without establishing
signature trust. They require matching source commits and architectures; benchmarks
also require native Linux. Publication separately verifies signed artifacts.

Input captures made by the former Python engine must be refreshed and rebuilt.
Their old policy hashes do not describe the Go engine or YAML overlays, so release
recovery refuses to treat them as validated Go builds.
