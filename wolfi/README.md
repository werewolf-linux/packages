# Wolfi derivation engine

Commands in this guide run from `wolfi/`; package contexts live in the top-level
categories. The repository root provides the common `make PACKAGE=NAME` interface.

The `wolfi/` engine captures Wolfi recipes unchanged as packaging baselines and
independently selects the latest stable upstream releases. Its checked derivations
apply the source/version update and our hardening changes. Discovery runs every ten
minutes; successful native builds publish with Werewolf's package key.

Consumers use one feed: **https://dist.werewolf-linux.org/apk/hardened**, with
`x86_64/APKINDEX.tar.gz` and `aarch64/APKINDEX.tar.gz` signed by
[release/packages.pub](release/packages.pub).

| Package | Follows | Policy |
|---|---|---|
| [glibc](../devel/glibc/README.md) | newest `glibc-2.*` | libc ignores its environment, no loader injection |
| [php](../lang/php/README.md) | newest `php-8.*` | no PEAR/Phar; execution, uploads and optional OS/diagnostic features require allowances |
| [openjdk](../lang/openjdk/README.md) | newest `openjdk-*` | object deserialization and native access restricted; see manifest exception |
| [python](../lang/python/README.md) | newest `python-3.*` | no user site; environment, ctypes, pickle globals, inline and writable main scripts gated |
| [node](../lang/node/README.md) | newest `nodejs-*` | no npm, inspector or SEA; exec, native, inline and writable modules require allowances |
| [curl](../net/curl/README.md) | `curl` | HTTP(S) only; no Brotli, Zstd, HTTP/2/3, proxy, SSH, LDAP or Kerberos libraries |
| [openssh](../net/openssh/README.md) | `openssh` | no PAM, SELinux, zlib or PKCS#11; OpenSSL stays for P-256 security keys |
| [vips](../graphics/vips/README.md) | `libvips` | Mastodon image formats only; no HEIF, SVG, PDF, TIFF or external loader modules |
| [ffmpeg](../multimedia/ffmpeg/README.md) | newest `ffmpeg-*` | local media only; no network protocols, display, devices or font rendering |
| [libffi](../devel/libffi/README.md) | `libffi` | callbacks from read-only library code (static trampolines), so they work under MDWE |
| [busybox](../devel/busybox/README.md) | `busybox` | applets a tool runs by name come from busybox itself, as glibc searches no PATH |
| PostgreSQL | newest `postgresql-*` | unversioned latest stable packages; no PAM/LDAP/GSSAPI, PL/Perl, PL/Python or OCI entrypoints; retain JIT, ICU, TLS, compression and contrib |
| PostgreSQL 17 | `postgresql-17` | versioned compatibility stream for existing Werewolf data clusters until an explicit major upgrade path exists |

Each package guide covers installation on Werewolf/Wolfi, every patch and allowance,
build changes and troubleshooting. Start with [feed setup, grants and denial logs](docs/usage.md).

Package snapshots and release notes remain under `apk/NAME/builds/BUILD-ID/`.
Legacy `apk/NAME` feeds are maintained for compatibility. Runtimes keep Wolfi's names;
glibc uses `glibc-neutered-*` names and provides/replaces Wolfi's names.
Our version outranks the Wolfi build it rebuilds, and each feed package provides
`upstream-origin:NAME:VERSION` for the actual source identity used in CVE comparisons.
The captured Wolfi recipe version is recorded separately; a newer upstream source
does not claim to be a Wolfi build. Werewolf accepts the historical `wolfi-origin`
marker for existing packages too. Allowances
are grouped by capability, pledge-style: an empty file at
`/etc/runtime-hardening/allow/<ns>/LANG/GROUP`, or `.../all/LANG/GROUP` for every
service, read off the read-only root, never from argv or the environment. `<ns>` is the
service, from `/proc/self/cgroup`. Any distro shipping these packages gets the same
default-deny and relaxes a knob by creating one file.

Python, PHP, Java and Node refuse initialization when their **effective UID is 0**,
including embedded runtime initialization. A real UID of 0 with a non-root effective
UID is allowed. Builds and package tests create a private
`/etc/runtime-hardening/build-root` marker (root-owned, regular, not writable by group
or other); it is not a form allowance. Publication rejects any runtime APK containing
that marker or anything else under `/etc/runtime-hardening`. Production services must
drop privileges before starting a runtime, including PHP-FPM's master process.

Hardening denials emit one JSON record directly to file descriptor 2 (stderr),
which Werewolf routes to the console. This also happens when an application
catches the exception or replaces its language's stderr object. Records identify
the runtime, capability group, operation and target, with a timestamp, PID,
effective UID and the process arguments from `/proc/self/cmdline`. No syslog
service is required. Normal error messages and exit statuses still apply.

Details are limited to 192 bytes (192 characters in Java), and process arguments
to 256 bytes in total; `truncated` reports a limit being reached. Control characters
are escaped to keep each record on one line. Targets include module/script paths,
pickle classes, PHP function names and Java restricted methods; serialized payloads
and the environment are not dumped. PHP's disabled functions fail during lookup,
before their arguments are evaluated, so those records identify the function and
script location. Redirecting the underlying stderr descriptor also redirects these
records.

The vocabulary and allowance files are shared; coverage follows each runtime's
actual execution paths. Current differences remain explicit:

| Capability | Python | PHP | Java | Node |
|---|---|---|---|---|
| Effective-root startup refusal | yes | yes | yes | yes |
| `native` | ctypes | `dl`, SQLite loaders, optional FFI | Unsafe, JNI, FFM | addons, FFI, WASI, native loaders |
| `exec` runtime floor | leash only | yes | leash only | yes, including package scripts |
| `inline` | argv/stdin | argv/stdin, eval, generated streams | deferred | argv/stdin, worker eval, data-URL code |
| `writable-source` | main script | compiled files | deferred | ordinary main/dependency code, disk cache |
| `deserialize` | pickle globals | `unserialize()` objects | ObjectInputStream | no equivalent object-gadget API |
| `upload` | no runtime upload API | multipart parsing and upload moves | no runtime upload API | no runtime upload API |
| `upload-progress` | no equivalent | session upload-progress callbacks | no equivalent | no equivalent |
| `namespace` | not gated | `pcntl_unshare`, `pcntl_setns` | not gated | not gated |
| `process` | not gated | fork, session/process group changes | not gated | not gated |
| `listen` | not gated | stream socket servers and accepts | not gated | not gated |
| `special-files` | not gated | FIFOs and special nodes | not gated | not gated |
| `debug` | not gated | fatal backtraces, variable dumps, printed backtraces, PHP information | not gated | not gated |

PHP gates string compilation with `inline` and verifies every compiled file by
its opened descriptor, including OPcache hits. `php:upload` permits multipart parsing
(including text-only forms) and upload moves; ordinary URL-encoded forms need no grant.
It does not grant execution of uploaded code or restrict applications that save raw
request bodies themselves. Session upload progress also needs `php:upload-progress`,
checked at the start of each multipart body. In-process code provenance in the
other runtimes (`eval`, `vm`, generated classes and custom loaders) remains separate
work. These defaults complement the service's kernel restrictions.

## How it works

Every ten minutes `wolfi-refresh.yml` takes one Wolfi snapshot and, for each package:

1. **Capture** (`scripts/refresh.py`, `releases.py`): resolve the latest upstream
   stable release using the shared Go updater, then capture a suitable Wolfi recipe,
   its directory, pipelines and key unchanged. Lock the baseline separately from
   the selected release. Same-release maintenance commits are retained; newer
   upstream releases advance without waiting for Wolfi. Older-stream pins are rejected.
2. **Verify and derive** (`verify.py`, `derive.py`): the package's `derive` hook edits
   the recipe; `check` refuses an upstream change nobody has reviewed. Its source is
   fetched at the effective source's exact commit by one shared step, with mirrors and retries.
3. **Prepare** (`prepare.py`): every patch applies at that commit as the build will
   apply it, GNU patch with fuzz 2.
4. **Build** (`wolfi-build.yml`): Melange on native x86_64 and aarch64 runners, then Wolfi's
   package tests and our own probes against the signed packages.
5. **Publish** (`publish.py`, `feed.py`): a GitHub draft, immutable APKs, signed indexes,
   then the release. Each index update is conditional on the version read; a concurrent
   publisher re-reads and merges. Existing versions stay available, and a failed
   package leaves its previous release available while others publish. `feed.py`
   backfills existing signed releases even when no rebuild is needed. See
   [publishing](docs/design/publishing.md).

Repository order and higher epochs alone do not prevent fallback to a newer Wolfi
package. Consumers must verify covered package names against the signed hardened
index, including their versions and control checksums, and fail if the feed is
unavailable. Explicit older runtime streams and runtimes inside OCI images need their
own migration; adding this feed does not replace them.

A step that stops a package opens an issue labelled `drift` and `package/NAME`, with the
version and what failed: a patch that no longer applies quotes patch's output. A patch
that needed fuzz still ships, and opens an advisory `fuzz` issue to refresh it. Issues
close when the step passes again. One package's failure holds back no other.

## Building locally

On macOS Melange boots a QEMU guest, built once into `build/guest/ARCH` with apko; on
Linux it uses bubblewrap. You need Python 3.12+ with PyYAML, git, GNU or BSD patch, and:

```sh
brew install apko qemu libarchive          # Linux: apko, bubblewrap, bsdtar
python3 scripts/install-melange.py --dest build/tools/melange
make test                                  # every unit test, no network
make discover                              # capture, verify, derive and patch all
make PACKAGE=php build                     # one package, this machine's arch
```

`make PACKAGE=NAME` also takes `refresh`, `verify`, `derive` and `prepare`; `WOLFI=DIR`
uses a local Wolfi checkout. Local builds sign with a throwaway key in `build/keys/`.
`MELANGE_CPU` and `MELANGE_MEMORY` size the guest. Packages land in
`build/NAME/packages/ARCH/`.

Native Linux CI builds can use dedicated self-hosted runners. Register one Linux
runner per build machine as a dedicated non-root account, then set these repository
variables to JSON arrays of its labels. Building glibc also requires a Linux kernel
of at least 6.18.

```text
WEREWOLF_X86_64_BUILD_RUNNER=["self-hosted","Linux","X64"]
WEREWOLF_ARM64_BUILD_RUNNER=["self-hosted","Linux","ARM64"]
```

An unset variable uses the GitHub-hosted runner for that architecture. Linux builds
use bubblewrap and the host's available CPUs and memory; the Node recipe builds
with `make -j$(nproc)`. `MELANGE_CPU` and `MELANGE_MEMORY` size QEMU guests, not
native bubblewrap builds. Runner setup adjusts Ubuntu's restrictive bubblewrap
AppArmor child profile on GitHub-hosted runners, and checks chroot before every build.

Self-hosted jobs use preinstalled tools and never invoke sudo. Provision them once
as an administrator (keep Arch's system packages up to date before installing):

```sh
# Fedora
sudo dnf install bubblewrap python3-pip git make openssl patch util-linux shadow-utils
# CachyOS / Arch
sudo pacman -S --needed bubblewrap python python-pip git make openssl patch util-linux shadow
```

The runner account needs subordinate UID and GID ranges covering at least 65536
IDs in `/etc/subuid` and `/etc/subgid`, with `newuidmap` and `newgidmap` available.
`scripts/ci-root` uses `unshare --user --map-auto --map-root-user` so the account
appears as UID 0 inside the namespace. Melange and the real/effective UID probes
keep their required capabilities there without host root. The helper refuses a
self-hosted agent running as host root. This is a user namespace, not a disposable
machine: builds still have the runner account's access to files and the network.

Deployment environments allow only the `main` branch. Production builds, feed
updates and publication use the `release` environment, which holds their signing
and storage credentials. It has no required reviewer: successful native builds
and package validation gate automatic publication. Manual test builds use `ci`.
Keep branch restrictions and credentials on these existing environments; no
duplicate owner environments or copies of their secrets are needed.

## Adding a package

From the repository root, make `CATEGORY/NAME/` with `metadata.json`, `package.py`, `policy.json` (`{"revision": 1}`),
`patches/series` and `tests/`. Copy the closest package. `package.py` defines:

| Hook | Required | Purpose |
|---|---|---|
| `FAMILY` or `RECIPE` | yes | recipe prefix, such as `php-`, whose newest stable stream wins; or one recipe Wolfi versions within, such as `libffi` |
| `SOURCES` or `sources(lock)` | yes | git URLs for the locked commit, first choice first |
| `PATCHES` | yes | patch directory inside the build context; `None` with an empty `patches/series`, for a policy of build options alone |
| `source_commit(config)` | yes | the commit Wolfi's recipe pins |
| `full_version(lock)` | yes | the version we publish |
| `context(pkg, dir)` | yes | copy patches and probes into the build context |
| `derive(pkg, config, lock)` | yes | Wolfi's recipe to ours |
| `check(pkg, upstream, derived)` | yes | refuse a derivation that lost a protection |
| `TITLE`, `UPSTREAM` | no | names for issues and release notes |
| `stable`, `check_policy`, `prepared`, `upstream_patches` | no | baseline selection and source preparation checks; retained Wolfi patch list |
| `test_recipe`, `TEST_PACKAGES`, `TEST_UNPINNED`, `MIN_KERNEL` | no | package tests |
| `check_apk`, `check_apks`, `gather`, `notes` | no | publication checks and notes |

Raise `policy.json`'s revision whenever patches, packaging or tests change behaviour:
it moves the version above our previous build of the same Wolfi release.

## Writing patches

Patches must survive upgrades, so keep them small and fuzzy-tolerant:

- Change a default or add one early check rather than rewrite a function.
- Anchor hunks on code upstream rarely touches, with short context.
- Fail closed: if the code a patch expects is gone, the build or a probe must fail.
- Prove each protection in the package's probe, run against the installed package.

Each patch starts with `Subject:`, `Upstream-Status:` and `Reference:` headers and is
numbered in `series`. Patches carry the license of what they patch (`LICENSES/`); the
tooling is under `LICENSE`.
