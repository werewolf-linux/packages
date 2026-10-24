# Publishing Wolfi package updates

## Summary

Every ten minutes, rebuild the newest stable Wolfi recipe of each package under
`packages/` with our policy patches, and publish signed APK repositories for both native
architectures to R2 and GitHub. Consumers use one combined feed at `apk/hardened`.
One package never holds back another.

## Background

A maintained version pin delays security updates and duplicates Wolfi's release work. We
need immutable captured inputs for each build, no version kept in this tree, and a
person told when a Wolfi change stops a package.

## Goals

- Follow only each package's newest stable stream, chosen numerically.
- Skip inputs already published; recover staged releases without rebuilding.
- Require native x86_64/aarch64 builds and package tests before publication.
- File an issue for each package a Wolfi change stops; close it when it passes.

## Non-Goals

- Automatic patch rebasing, older streams kept for compatibility, or deployment.
- Altering Werewolf's existing APK repository index.

## Detailed design

`refresh.yml` runs at minutes 7-57/10, on main pushes and by hand. Builds and
publication remain independent per package; only index commits serialize through
R2 conditional writes. `check.yml` runs discovery on pull requests without building
or filing issues.

**Discovery** (`discover.py`) clones Wolfi once, then runs refresh, verify, derive and
prepare for each package as separate commands. The first to fail stops that package: its
issue is filed and discovery goes on. Passing packages are bundled with their inputs and
listed to build or publish, by the state of their release.

**Capture** (`refresh.py`) picks the highest stream named `FAMILY` plus digits and dots
that the `stable` hook accepts; `policy.json` `"stream"` may pin one. It requires the
recipe's exact source pin and captures the unchanged recipe, its directory, pipelines
and Wolfi's key. The lock hashes them with their modes, plus our scripts, workflows,
tool pins and package directory less docs: the build identity.

**Version.** `policy.json` keeps a revision that `full_version` folds into the APK
epoch, so ours outranks the Wolfi build it rebuilds. Raise it when behaviour changes.

**Source.** Preparation and Melange both fetch only Wolfi's exact commit, depth one,
from the package's sources in order: 120 seconds each, three rounds, 15- and 45-second
pauses. A mismatched commit stops at once; branch tips never replace the pin.

**Patches** apply with GNU patch, `-p1 --forward --fuzz=2`, as Wolfi applies its own: on
the host by `prepare.py`, then in the build by one shared step `verify.py` requires
unchanged. Fuzz still builds and files an advisory issue; a failure stops the package.

**Issues** (`drift.py`) are labelled `drift`, `package/NAME` and their kind: `refresh`,
`derivation`, `patches` or `fuzz`. Each names the Wolfi version and quotes the output. A
hidden marker finds the open issue, so a new failure comments on it.

**Builds** (`build.yml`) run Melange in bubblewrap on `ubuntu-26.04` and
`ubuntu-26.04-arm`. Ubuntu's AppArmor child profile denies the `sys_chroot` APK's
bootstrap test needs; a private bubblewrap copy gets a profile allowing `sys_chroot`,
`setuid` and `setgid`. The latter two let runtime probes verify real/effective UID
mismatches. The system binary is checked to still refuse chroot. Package tests, Wolfi's and our probes,
run as a separate Melange command against the signed packages.

**Publication** (`publish.py`) checks build records, APK signatures, payload hashes,
index coverage and matching versions on both architectures. `NAME-ARCH.tar.gz` archives
go to a GitHub draft tagged `NAME/build-<input hash>`, its manifest last, so a later run
resumes with the original bytes. Archives are normalized and assets never overwritten.

R2 holds each snapshot under `apk/NAME/builds/<input hash>/<arch>/`, APKs before the
signed index, both architectures before the completion manifest. Machines follow
`https://dist.werewolf-linux.org/apk/hardened`, whose `<arch>/` keeps every published
APK and one index naming all package families and retained versions. Per-package
stable indexes remain available for compatibility and as authenticated backfill inputs.

`feed.py` authenticates each source index and each added APK, including its payload,
control checksum, architecture, identity and size. Immutable APKs arrive before the
combined index. Index reads use the R2 API rather than a CDN. A signed union replaces
the index with `If-Match` on the ETag read, or `If-None-Match: *` for initial creation.
A racing publisher re-reads, merges and retries; other errors stop publication. APK
creation is conditional too and never overwrites a version's first bytes.

Normal publication includes its package in the combined feed before completing the
GitHub release. A separate feed job backfills already published releases even when
discovery requires no new build, and repairs partial publication. A missing or failed
family never removes its last good versions or prevents other families from merging.
`current.json` advances conditionally after both architectures are included, never
backwards, and the GitHub release becomes public last.

Release notes are made once, at staging: Wolfi's and upstream's changes, ours,
advisories fixed, test counts, probe results and provenance. Every package provides
`wolfi-origin:<origin>:<wolfi version>`, for Werewolf's CVE matching.

The `release` environment holds `WEREWOLF_PACKAGES_KEY` and the R2 keys. Signing has its
own step, so the key never reaches a build; OpenSSL converts it to PKCS#1 through
standard input, mode 0600. Its public key must equal the committed trust root.

## Drawbacks

Native suites are expensive, and removed features fail some upstream tests; glibc's 5.5%
raw failure ceiling can admit new failures. Because packages keep Wolfi's names, a newer
Wolfi build outranks ours until we publish; Werewolf must take them only from here.

## Alternatives Considered

- One repository per package: duplicates the pipeline; fixes land in one only.
- Daily version-update PRs: a separate pinned stream and slower releases.
- Sharing Werewolf's APK index: couples two release pipelines.
- Renamed packages: every form and dependency would need our names.
- Rebuilding after a failed publication: changes bytes and makes retries unsafe.

## Security Considerations

Publication reads artifacts from its own run, never a pull request's, which use
throwaway keys. Bucket reads are authenticated; an authentication error never means
absent. Concurrent CI writers commit indexes conditionally; signature failures never
reset an index. Mirrors give availability, not trust:
Wolfi's recipe pins the commit, checked before Git runs, with hooks disabled.

Fuzz can misplace a hunk. Each package's probes test every protection on the installed
package, so a misplaced hunk fails the build. A published package passes these gates; it
is not evidence a form is ready for it. Form, IPE and performance checks govern that.

## Reliability Considerations

A complete draft preserves validated bytes across R2 failures. Uploads are idempotent
and reject conflicting bytes. A failing package turns the run red at the end without
holding back others.

Sources: [GitHub schedules](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule),
[R2 S3 compatibility](https://developers.cloudflare.com/r2/api/s3/api/),
[artifact permissions](https://github.com/actions/upload-artifact#permission-loss).
