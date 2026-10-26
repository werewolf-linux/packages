# glibc

Wolfi's newest glibc with fixed restrictions on environment configuration and dynamic loading.

## Use

Requires Linux **6.18 or later**, including the host kernel for containers.
Werewolf's minimal form installs `glibc-neutered`. On Wolfi, use the same alias
after [feed setup](../../Werewolf/docs/usage.md#install):

```sh
apk add glibc-neutered@hardened
```

Test applications in a rebuilt image before deployment; these changes affect every
glibc-linked program. Packages are named `glibc-neutered-VERSION` and
`ld-linux-neutered-VERSION`, provide/replace Wolfi's names, and include matching NSS
configuration. Use the combined hardened feed; the older glibc feed is compatibility-only.

## Patches

| Patch | One-line change | Allowance / policy |
|---|---|---|
| [0001](patches/0001-deny-internal-environment.patch) | Ignore libc/loader environment configuration, force eager binding/strict path behavior and reject executable stacks. | Always on; environment overrides always denied |
| [0002](patches/0002-disable-loader-injection.patch) | Disable preload/audit injection and loader overrides; make `ldd` use `--list`. | Always denied |
| [0003](patches/0003-builtin-nss.patch) | Load only built-in `files` and `dns` NSS providers. | External providers always denied |
| [0004](patches/0004-builtin-converters.patch) | Use built-in iconv converters; ignore external gconv configuration, caches and DSOs. | External converters always denied |
| [0005](patches/0005-remove-audit-dispatch.patch) | Remove audit callback dispatch, including forged audit registration state. | Always denied |

There are **no runtime allowances**. Form names `glibc:dlopen`/`glibc:ifunc` do not
change this build: ordinary `dlopen`, dependencies and IFUNC CPU dispatch already
work. Native libraries retain the process's authority; audit removal is not isolation.

## Debug and migration

glibc uses ordinary errors, not runtime-denial JSON. Check the loaded library with
plain `ldd PROGRAM`; use `readelf -l -d PROGRAM` to inspect interpreter/dependencies.
`LD_DEBUG` is ignored. Inspect the runtime image's files and configuration directly.

| Symptom / changed behavior | Fix |
|---|---|
| `FATAL: kernel too old` | Boot Linux 6.18 or newer; changing the container image cannot upgrade the host kernel. |
| Bare-name `execvp`/`execvpe`/`posix_spawnp` returns `ENOENT` | Use an explicit executable path such as `/usr/bin/tool`; libc no longer searches `PATH`. |
| Options after positional arguments stop working | Put options before operands; `getopt` no longer permutes argv. |
| `LANG`, `LC_*`, `LOCPATH` ignored; `setlocale(..., "")` selects `C` | Select an installed locale explicitly, such as `setlocale(LC_ALL, "C.UTF-8")`. |
| `TZ` ignored | Install the intended `/etc/localtime`, or use the application's explicit timezone API. |
| `LOCALDOMAIN`, `RES_OPTIONS`, `HOSTALIASES` ineffective | Configure `/etc/resolv.conf` and `/etc/hosts`; resolver environment settings are ignored. |
| Temporary-name creation fails despite `TMPDIR` | Supply an explicit existing directory/template, such as `/run/app/tmp.XXXXXX`. |
| Tilde expansion ignores `HOME` | Set the account database home or pass an explicit path. |
| Pattern matching differs | Use POSIX `fnmatch` semantics; environment cannot switch them. |
| `GLIBC_TUNABLES`, malloc tuning or profiling environment has no effect | Use compiled defaults or explicit supported application APIs; implicit profiling output is disabled. |
| “executable stack is not allowed” | Rebuild the requesting executable/library without executable-stack requirements; `glibc.rtld.execstack` is fixed at 0. This does not disable ordinary JIT mappings. |
| `LD_LIBRARY_PATH` or `--library-path` cannot find a dependency | Install libraries in configured loader paths or link with the intended RUNPATH. |
| Preload/audit instrumentation missing | Link instrumentation explicitly or use application hooks; `LD_PRELOAD`, `LD_AUDIT`, `/etc/ld.so.preload`, `DT_AUDIT`/`DT_DEPAUDIT` cannot restore it. |
| “loader overrides are disabled” | Remove `--preload`, `--audit`, `--library-path`, `--inhibit-rpath`, `--inhibit-cache`, `--glibc-hwcaps-prepend` or `--glibc-hwcaps-mask`. |
| `ldd -d/-r/-u/-v` fails | Use plain `ldd`/loader `--list`; optional loader controls are disabled. |
| LDAP/mDNS/Hesiod/NSS plugin lookups fail | Use local account files and DNS; changing `nsswitch.conf` cannot enable external providers. |
| `iconv_open` fails for UTF-16 or a legacy encoding | Convert upstream or use an application library; built-ins support UTF-8, ASCII and UCS only. |

Public `getenv`, `setenv` and `secure_getenv` still work for applications. All `LD_*`
configuration and libc's internal environment reads are suppressed. Dependencies
bind eagerly; missing symbols can therefore fail at startup rather than first use.
NSS configuration uses `files`, plus `files dns` for hosts ([full file](profile/nsswitch.conf)).

## Build differences

Keep Wolfi's commit, patches, toolchain, OpenSSF flags, bind-now, CET, fortify,
strong stack protector and stack-guard randomization. Add `-fstrict-flex-arrays=3`
and `-z separate-code`; raise minimum kernel from 4.9 to 6.18.
Disable nscd build/use. Remove nscd, nss-db, nss-hesiod, sotruss, pcprofiledump,
mtrace/xtrace/sprof, profiling/debug allocator DSOs, external gconv modules and iconvconfig.
Keep the iconv CLI and remaining development/runtime subpackages.
Provider priority is `10MAJORMINOR` (10244 for 2.44); epoch adds the policy revision;
`wolfi-origin` preserves CVE matching. Tests use explicit loader paths/locales,
strict-option-compatible tooling, the full Wolfi suite, injection probes and ABI checks;
raw failure ceiling is 5.5% rather than Wolfi's 2%.
[Build instructions](../../Werewolf/docs/wolfi.md#building-locally), [audit](docs/glibc-vuln-audit.md),
[native validation](docs/audit/native-r16.md). Patches: LGPL-2.1-or-later.
