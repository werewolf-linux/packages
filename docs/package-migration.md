# Package definition and automation migration

Adopt one `CATEGORY/NAME/package.yaml` as the maintained entry point for each
package. Use native Melange configuration for builds and tests, with a small
wrapper for automatic updates, guarded Wolfi derivation, and maintenance evidence.

## Agreed design

- Exactly one of `melange` (a native recipe) and `wolfi` (a captured baseline and
  guarded edits) selects the build engine. The directory supplies the context name;
  native recipes retain their APK names.
- `update` owns release discovery. Use native GitHub update vocabulary where it
  fits; retain explicit adapters for other sources, dependency locks and components.
- `changes` records explanations, evidence, positive hardening claims and removal
  conditions once. Omitted claims mean no claim. Preserve substantive migration
  notes even when old boilerplate can be eliminated.
- `actions` holds exceptional structured helper operations. Prefer native Melange
  pipelines, test sections and resources for ordinary build work.
- Remove package-local `metadata.json`, `policy.json`, `overlay.yaml`, separate
  native recipes and `packaging/changes.yaml` after migrating their consumers.
- Keep patches, tests, scripts and ecosystem manifests beside the definition.
  Move package-specific smoke tests out of the central test registry. Execute only
  declared work; preserve one explicit patch order.
- Retire `wolfi/` as a second project root. Preserve imported licenses and provenance,
  move shared pipelines and runner assets into the common framework, and run all
  commands from the repository root.
- Put generated inputs, recipes, architecture workspaces, artifacts and logs under
  `build/`. Key package work by reviewed inputs and isolate architectures. Keep
  caches and tools shared; captures used by both architectures are immutable.
- Extend the common planner to both engines. Derive CI matrices from definitions
  and changed inputs; adding a package must not require editing a registry or CI.
- Separate release discovery from validation of captured inputs. Preserve independent
  package failure handling, immutable publications, signing boundaries and recovery.
- Provide check, render, updates, build and test commands shared by contributors and
  CI, an editor schema checked against the loader, and minimal package scaffolding.
- Keep prose out of build identities while retaining complete source provenance.
  Preserve APK names, version ordering and existing epoch arithmetic.

## Implementation sequence

1. Define and document the loader and renderer; migrate all definitions and callers.
2. Move package tests and shared assets; unify generated paths and root commands.
3. Add the common CI planner, reproducible validation inputs, schema and scaffolding.
4. Update workflows and documentation; remove obsolete entry points and registries.

## Validation

- Compare every rendered application recipe and transformation plan with the old
  definition, allowing only intentional path and test-layout changes.
- Run complete Wolfi derivation snapshots and upstream-drift regression tests.
- Exercise update transactions, source pins, dependency locks, smoke environments,
  provenance, artifact recovery and build identity behavior.
- Check strict YAML errors, duplicate fields, incompatible sections, evidence paths,
  unknown actions, and schema/loader agreement.
- Run `go test ./...`, formatting/vet checks, catalog checks, pinned Melange recipe
  validation and workflow lint. Native package builds remain subject to Linux and
  architecture-specific runner availability.

Dependency-aware consumer selection can follow this migration. Until then shared
runtime/framework changes must select a conservative validation set. This avoids
requiring a second manually maintained dependency graph.

## Progress

- Implementation completed; final validation is recorded below.

### Implemented

- Migrated all 54 contexts to one YAML definition and preserved all 54 release
  policies. Compared 39 native build recipes, 15 Wolfi overlay specifications and
  eight helper action plans against their original values.
- Bumped each native epoch and Wolfi revision exactly once for the builder migration.
  Kept existing APK names and epoch arithmetic.
- Moved package-specific installed tests and n8n pruning regressions into their
  contexts. Native main-package and subpackage tests retain their configuration.
- Consolidated the CLI, documentation, shared runner setup, input captures and
  architecture workspaces. Added rendering, path discovery, scaffolding and an
  editor schema generated from the strict wrapper loader.
- Added catalog-driven CI matrices for both engines. Separate capture jobs supply
  one source snapshot to both native architectures. Package discovery also accepts
  descriptive new categories without changing a category registry.
- Added complete staged updates with stale-input checks and rollback; preserved
  independent failures, publication signing boundaries and artifact recovery.
- Kept Wolfi context-copy mappings explicit: automatically mixing local files into
  captured upstream directories could introduce unreviewed collisions. This is a
  deliberate exception to removing identity mappings.

Full local validation includes the Go regression suite, formatting, vet, schema
agreement, pinned Melange validation for every definition, complete Wolfi derivation
snapshots, and workflow lint. Native Linux APK builds and GitHub workflow execution
remain for the updated CI; they were not executed in this macOS workspace.
