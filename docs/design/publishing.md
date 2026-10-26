# Publishing package updates

Package definitions, defaults, version arithmetic and contributor commands are
specified in [the package reference](../package.md). Publication uses the common
CLI from the repository root. Application and Wolfi artifacts retain their distinct
signing requirements and publication feeds.

## Source selection and builds

The application updater prepares each source and dependency-lock update in isolation,
validates it, and refuses to replace a context edited during discovery. Successful
updates are committed per package; an ordinary push must succeed before dispatching
the publisher. Upstream failures do not discard other successful updates.

`package release-plan` checks application publication state and selects new native
builds or staged publication retries. `package plan` independently supplies the common
changed-package validation matrix for both engines. Both use the catalog discovered
from `CATEGORY/NAME/package.yaml`; workflows have no package registry.

Wolfi discovery explicitly selects a Wolfi baseline and independent upstream stable
release, validates derivation and patches, and captures unchanged upstream files,
pipelines, source identities and file modes. Local policy and relevant builder code
are bound to the build ID. Explanatory prose is retained in source and provenance
without changing that build ID.

The same captured archive is restored by both architecture jobs. Builds fetch exact
source commits with the declared mirrors and retries, apply the reviewed patches,
and run retained and additional installed-package tests. PR validation uses a
separate explicit capture stage before native builds; restoring that archive replays
the selected source without repeating discovery.

The common builder setup installs pinned tools and checks sandbox support. Application
builders produce unsigned APKs without release credentials. Production Wolfi builds
use the release environment and sign packages before their installed-package tests.
PR and local Wolfi builds use ephemeral signing keys.

## Independent publication

Both architectures must pass their native build, installed tests and artifact checks.
A package cannot publish a mixed source, input ID, architecture set or workflow run.
One package's failure retains its last publication and does not hold back other
packages. No partial architecture set replaces a published package.

Application publication validates native provenance, stages both architectures,
signs their APKs with the checked production key, and publishes a signed manifest.
Wolfi publication stages already signed artifacts and their provenance in a
recoverable GitHub draft. Production signing keys must match `release/packages.pub`.
Credentials remain in the release environment.

Release identities and APK versions are immutable. A changed native recipe or shared
builder requires an application epoch or Wolfi downstream revision bump at the same
upstream version. The migration to unified definitions bumps each exactly once.
The two feeds remain `apk/packages` and `apk/hardened`.

Index publication merges independently published packages using conditional object
writes. A concurrent writer causes a re-read and merge. A completed release is marked
only after its signed packages and index are available. `package feed` can backfill
existing signed Wolfi releases without rebuilding them.

## Recovery and diagnostics

Recovery verifies the original source workflow, repository, commit and both successful
native jobs before accepting historical artifacts. It restores the original input
archive and checks the release tag. A repaired publisher can stage and publish those
validated artifacts without rebuilding or substituting new source inputs.

Wolfi drift issues identify the package, selected source, failed operation and log.
Repeated failures update their existing issue; successful discovery resolves it.
Workflows report per-package failures after retaining other successful work.
Application updates use the same structured report for available, current, updated,
held and failed results, including the underlying failing operation in their errors.

Generated work, source captures, architecture workspaces and logs remain under
`build/`. Workflow paths come from `package paths`, and captured bundles travel as
artifacts. License and import provenance remain under `LICENSES/wolfi/`.
