# Contributing packages

Put each context under `CATEGORY/NAME/`, with one maintained recipe or a Wolfi
[`overlay.yaml`](docs/overlays.md) derivation. Keep sources, patches, locks and tests together. Pick a
broad category describing what the software does; names are globally unique.

Add `metadata.json` with a supported automatic update source, explicit hardening
claims and an explanation of the Wolfi divergence. See [the metadata contract](docs/metadata.md).
Use existing sources and lock handlers before adding package-specific code.

Run `make metadata`, then `make test lint check`. For Wolfi derivations also run
`make PACKAGE=NAME discover`; it captures the newest recipe and applies the patches.
Build both native architectures and exercise installed-package probes before
publication. Application service and confinement tests live in Werewolf forms.

Bump the recipe epoch when changing an existing upstream release's build inputs;
bump `policy.json` revision for a Wolfi derivation. Published APK identities are
immutable. Preserve original licenses, source pins and package signing checks.

Keep functions small and direct. Avoid duplicate configuration, speculative
frameworks and silent fallbacks. A failed update must leave the previous context
and publication usable, and must report the package and the failed step.

Describe package-specific file edits in `packaging/changes.yaml` and reuse
`cmd/package-helper`. Add common operations in Go when needed. Keep application
launchers and direct runtime/API tests in the language they exercise. Go code
follows [the Go review comments](https://go.dev/wiki/CodeReviewComments) and
[Google's Go guide](https://google.github.io/styleguide/go/). Pass contexts from
`main`, avoid new dependencies, and keep each Go file below 2000 lines.
