# Contributing packages

Put each context under `CATEGORY/NAME/`, with one maintained recipe or a Wolfi
`package.py` derivation. Keep sources, patches, locks and tests together. Pick a
broad category describing what the software does; names are globally unique.

Add `metadata.json` with a supported automatic update source, explicit hardening
claims and an explanation of the Wolfi divergence. See [the metadata contract](docs/metadata.md).
Use existing sources and lock handlers before adding package-specific code.

Run `make metadata`, then `make test check`. For Wolfi derivations also run
`make PACKAGE=NAME discover`; it captures the newest recipe and applies the patches.
Build both native architectures and exercise installed-package probes before
publication. Application service and confinement tests live in Werewolf forms.

Bump the recipe epoch when changing an existing upstream release's build inputs;
bump `policy.json` revision for a Wolfi derivation. Published APK identities are
immutable. Preserve original licenses, source pins and package signing checks.

Keep functions small and direct. Avoid duplicate configuration, speculative
frameworks and silent fallbacks. A failed update must leave the previous context
and publication usable, and must report the package and the failed step.
