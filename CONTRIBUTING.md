# Contributing packages

Each package lives under `CATEGORY/NAME/`, with one `package.yaml` and any patches,
tests, scripts and ecosystem locks it needs. Names are globally unique. Use a broad
category describing what the software does. Adding a package requires no registry
or workflow edit.

Read [Package definitions and maintenance](docs/package.md), the authoritative
reference for the format, defaults, rendering, updates and CI behavior. Start with
`build/tools/package new --repo OWNER/PROJECT CATEGORY/NAME`, then review its source,
license, build steps and installed tests. Prefer native Melange capabilities and
existing release/lock adapters before adding framework code.

Run `make test lint check`. Use `package render --package NAME` to inspect the complete
native recipe. Explicitly capture Wolfi inputs with `package discover --package NAME`
before building. Build and test both architectures before publication. Service
confinement remains declared in Werewolf forms.

Bump `melange.package.epoch` for changed direct-recipe build inputs at the same
upstream version, or `wolfi.revision` for a derivation. Published APK identities are
immutable. Preserve source pins, original licenses and signing checks.

Keep functions small and direct. Avoid duplicate configuration, speculative
frameworks and silent fallbacks. A failed update retains the previous publication
and reports its package and failing operation. Keep native runtime/API tests in the
language they exercise. Go code follows the [Go review comments](https://go.dev/wiki/CodeReviewComments)
and [Google's Go guide](https://google.github.io/styleguide/go/). Pass contexts from
`main`, avoid new dependencies, and keep each Go file below 2000 lines.
