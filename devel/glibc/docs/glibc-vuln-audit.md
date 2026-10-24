# glibc vulnerability audit and hardening inventory

Audit date: **2026-10-13**. Intended target: Werewolf's x86_64 and aarch64 application runtime, derived from Wolfi's glibc packages. This is a research inventory and policy rationale. Candidate patches now exist; production application compatibility and release readiness have not been established.

## Findings that should drive the build

The selected design has **five protections**, with environment configuration treated as one mechanism and preload/audit activation as another:

1. **Deny glibc's implicit environment configuration.** Cover loader/tunable startup scans, legacy aliases, and later internal lookups. Ignore `LANG`, `LANGUAGE`, and all `LC_*`; `setlocale(category, "")` selects `C`. Preserve application `getenv` and `secure_getenv` and real kernel secure-execution sanitation.
2. **Disable loader injection and search overrides from every source.** Explicitly cover `LD_PRELOAD`, `LD_LIBRARY_PATH`, `LD_AUDIT`, other loader environment controls, `/etc/ld.so.preload`, loader CLI equivalents (including `--library-path`), and ELF audit tags. Preserve normal dependencies and explicit loading under Werewolf's native-code integrity enforcement.
3. **Permit only built-in NSS backends, `files` and `dns`.** Reject external module loading at the load point. Immutable `/etc/nsswitch.conf` alone is insufficient when a privileged program changes its filesystem root or corruption changes a module name.
4. **Permit only built-in character converters.** Skip external registry/cache discovery and prevent converter DSO loading. This addresses ordinary encoding arguments as well as PwnKit's restored `GCONV_PATH`, without creating another allowlist parser.
5. **Disable nscd daemon and client support using existing build options.** Keep direct resolver and built-in NSS paths; measure lookup costs.

Upstream fixes and regression tests remain the baseline for retained APIs, rather than a sixth protection or a reason to rewrite libc. Keep IFUNC/CPU dispatch, vector routines, tcache, arenas, threading, math, and ordinary DNS. Integer overflow, output sizing, allocation-failure cleanup, and signal-handler misuse cannot be solved by removing environment knobs.

The evidence below supports these priorities. Performance effects are qualitative expectations, not benchmark results. Compatibility must be validated; adapt applications before weakening a protection.

## Engineering policy and selected scope

Apply the copied [CONTRIBUTING.md](../../../CONTRIBUTING.md): simple, reliable, efficient, secure code; deny by default; concise documentation. The [Werewolf design](design/glibc-neutered.md) follows the project's template and its 120-line design limit. This audit is a detailed evidence inventory, not a design document; retaining per-CVE evidence is intentional.

Use the smallest reliable patch at existing input/activation points, not wholesale deletion or new runtime controls. No permissive profile, generic environment allowlist, or PostgreSQL preload exception is proposed. Resolve current application requirements with explicit application APIs and setup changes first. CONTRIBUTING's Werewolf build/test commands apply to integration in Werewolf; glibc package validation also requires its own upstream and architecture checks. Candidate code and CI exist; passing release and integration gates remain required.

**Code-execution boundary:** deny the identified implicit injection and search-redirection mechanisms throughout process lifetime. Public getenv still returning an `LD_PRELOAD` string must not make the loader act on it. Normal `DT_NEEDED`, RPATH/RUNPATH, explicit `dlopen`/`dlmopen`, constructors, and alternate loaders are additional native-code routes; environment denial alone does not constrain them. The production claim requires Werewolf's existing [IPE verified-root policy](https://github.com/tstromberg/werewolf/blob/main/docs/design/ipe.md) to reject untrusted executable mappings, plus image dependency/search-path inspection and adversarial loading probes. Non-IPE builds do not meet that claim. Current strict IPE also rejects anonymous executable memory, excluding JIT-dependent forms absent separate design work. These are integration requirements, not a sixth libc patch. Neither glibc reductions nor IPE prevent every RCE: trusted-code reuse, interpreted commands, and application memory corruption remain separate risks.

## Candidate implementation status

Four ordered source patches implement the first four mechanisms; nscd uses existing configure switches. Against Wolfi's pinned 2.44 source the revised policy changes 18 files, adding 80 and removing 71 lines. Public environment APIs, C locale behavior, loader environment/CLI/ELF injection, built-in conversion/NSS, static startup, threads, backtrace and C++ exceptions pass focused probes on aarch64 and emulated x86_64. These local builds use Ubuntu/GCC 13 and do not establish parity with Wolfi's complete hardened toolchain.

The first native Alpine/Melange build passed Wolfi's compiler check, special-test gate, and reported 100% public ABI compatibility, but its 902 runtime FAIL results do not meet the 2% package threshold. A [patch security review](design/patch-security-review.md) subsequently corrected permissive PATH, POSIX, temporary-directory, lazy-binding and executable-stack defaults; revised native package validation remains required. Policy-incompatible upstream tests and host-specific failures require individual review; no test suite or gate has been waived. There are no validated production packages. Initial native microbenchmarks show no median slowdown; [measurements](design/performance.md) and the [raw test inventory](audit/native-r12.md) retain evidence and limits. [Patch maintenance](design/patch-maintenance.md), [compiler evaluation](design/compiler-hardening.md), [Chainguard hardening investigation](https://www.chainguard.dev/unchained/this-shit-is-hard-hardening-glibc), [current OpenSSF guide](https://best.openssf.org/Compiler-Hardening-Guides/Compiler-Options-Hardening-Guide-for-C-and-C++.html).

## Scope, evidence, and interpretation

The exact ten-year window is **2016-10-13 through 2026-10-13, inclusive**. The inventory also includes January–October 2016 context, notably the major `getaddrinfo` DNS overflow, and older CVEs indexed much later. CVE identifier years are not disclosure dates: `CVE-2025-15281` was published in January 2026, while `CVE-2023-6246` was disclosed in January 2024. Tables use **NVD publication dates**, with first-public dates called out in the case studies where verified.

The expanded collection contains **120 CVE identifiers**: 119 retrieved NVD records plus the Fedora-specific `CVE-2024-12455`, which has vendor and distribution records but was absent from the queried NVD/CVE API results. **107** have record publication dates within the exact ten-year window, including historical backfills and disputed reports. The collection is an inventory of records, not a count of newly discovered, accepted, independently exploitable glibc flaws. In particular, `CVE-1999-0199`, `CVE-2005-3590`, and `CVE-2006-7254` are historical issues indexed in 2019–2020; do not count them as contemporary discoveries.

Collection method:

- Downloaded the complete CISA KEV JSON catalog and searched both product text and CVE-ID intersections. Snapshot: **2026.10.13**, released **2026-10-13T14:34:17.9076Z**, **1,740 entries**.
- Queried NVD's CVE 2.0 API for both `glibc` and `GNU C Library`, retrieving **189** and **290** records respectively, then deduplicated. A single keyword query missed important records.
- Cross-checked against Debian's glibc source-package tracker, used individual-ID queries to fill gaps, and manually excluded unrelated programs merely mentioning libc.
- Reviewed upstream glibc advisories, vendor/CNA records, original researcher advisories, sudo's maintainer disclosure, and OpenSSH's release notes. glibc's formal advisory directory covers the recent era; it is not a complete source for older history. [glibc security process](https://sourceware.org/glibc/security.html), [upstream advisory format](https://github.com/bminor/glibc/blob/master/advisories/README), [Debian tracker](https://security-tracker.debian.org/tracker/source-package/glibc), [NVD API documentation](https://nvd.nist.gov/developers/vulnerabilities).

**Severity:** “Critical” means a CVSS v3/v4 score of at least 9.0 from an identified source. Do not infer it from an advisory's prose. The inventory prefers NVD's v3.1/v3.0 score when present, otherwise an available CNA/ADP score (or v4). Divergent scores are recorded where material. A high base score does not establish a remote path in our forms. A low score does not make an exploitation primitive harmless.

**Exploitation:** KEV membership, a demonstrated research exploit, and a speculative attack path are different claims. “Not in KEV” below means absent from this catalog snapshot, not proof of no exploitation. We did not attempt to exhaust every commercial exploit-intelligence feed. No claim is made about undisclosed flaws or the completeness of keyword/CPE indexing.

**Fix status:** the inventory describes the vulnerable behavior and reduction strategy. It does not establish whether the currently selected Wolfi source commit contains every fix. Some recent records affect 2.44 or earlier; a recipe named `glibc-2.44` is not sufficient evidence either way because branch backports matter. Release gating must check the actual source and backport history.

## All glibc KEVs and related application chains

There is **one CVE assigned to glibc itself** in the downloaded catalog, across all years, and it falls inside this audit window. Three additional KEV-listed vulnerabilities have especially direct glibc exploitation paths. These are separate application CVEs, not extra glibc CVEs. [CISA catalog](https://www.cisa.gov/known-exploited-vulnerabilities-catalog), [machine-readable catalog](https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json).

| CVE | Owner; score | KEV addition; ransomware field | Relevant mechanism | What our glibc can change |
| --- | --- | --- | --- | --- |
| [CVE-2023-4911](https://nvd.nist.gov/vuln/detail/CVE-2023-4911) | glibc; 7.8 v3.1 | 2023-11-21; Unknown | Looney Tunables: early-loader heap overflow while sanitizing `GLIBC_TUNABLES` for privileged execution | Remove environment tunables parsing, including legacy aliases, before values are interpreted. |
| [CVE-2021-3156](https://nvd.nist.gov/vuln/detail/CVE-2021-3156) | sudo; 7.8 v3.1 | 2022-04-06; Unknown | Baron Samedit: sudo heap corruption; one research technique overwrites glibc NSS state and loads a constructed attacker-controlled module path | Built-in-only NSS eliminates that module-loading route, but does not fix sudo's overflow or other exploit techniques. |
| [CVE-2021-4034](https://nvd.nist.gov/vuln/detail/CVE-2021-4034) | polkit/pkexec; 7.8 v3.1 | 2022-06-27; **Known** | PwnKit: an out-of-bounds argv access rewrites the environment, restoring `GCONV_PATH`; error printing invokes `iconv_open` and loads code | Permanently ignore the path override and restrict converter loading. Startup sanitation alone fails against this pattern. |
| [CVE-2025-32463](https://nvd.nist.gov/vuln/detail/CVE-2025-32463) | sudo; **9.3 CNA / 7.8 NVD**, v3.1 | 2025-09-29; Unknown | User-selected chroot changes which `/etc/nsswitch.conf` sudo consults while still privileged; glibc loads an external NSS library | Reject external NSS providers at runtime. Patch sudo separately; it is fixed in sudo 1.9.17p1. |

For clarity, **CVE-2024-2961 (iconv), CVE-2023-6246 (syslog), CVE-2024-6387 (OpenSSH), CVE-2018-1000001 (realpath), CVE-2017-1000366 (glibc Stack Clash), CVE-2017-1000367 (sudo), CVE-2015-7547 (DNS), and CVE-2015-0235 (GHOST) are not in this CISA snapshot**. Several have compelling published exploitation research. Do not substitute an exploit repository or EPSS score for KEV membership.

## Cases that change the threat model

### Direct loader invocation bypasses environment-only library search restrictions

This is an abuse of an intentional feature, not an additional CVE. glibc supports launching an executable through the dynamic loader directly; its `--library-path` option supplies a library search path independently of `LD_LIBRARY_PATH`. [Upstream invocation documentation](https://sourceware.org/glibc/manual/latest/html_node/Dynamic-Linker-Invocation.html), [pinned loader source](https://sourceware.org/git/?p=glibc.git;a=blob;f=elf/rtld.c;hb=34106fee698c83eb0af71d4d0381e66b399be746).

```sh
/lib/ld-linux-aarch64.so.1 --library-path /writable/lib /usr/bin/trusted-app
```

**Attack prerequisites:** an attacker can place a compatible shared library in the supplied directory, control invocation of the trusted loader and its arguments, and cause an eligible dependency to resolve there. For example, a command allowlist could authorize the loader while accepting arbitrary arguments. The substituted library's constructors can execute before the application's entry point, even with a sanitized environment. Execution uses the loader process's credentials; direct invocation does not itself grant a target executable's setuid privileges.

**Proposed reduction:** reject `--library-path` alongside `--preload` and `--audit`, closing command-line alternatives to disabled environment controls. This provides little additional protection when loader invocation is unavailable to the attacker, or when the attacker can already execute arbitrary native code. It is a fixed appliance policy, not a claim that upstream's option is insecure in every deployment. Adapt build/test utilities rather than add a runtime exception.

**Remaining boundary:** Werewolf's production IPE policy must reject executable mappings of untrusted libraries regardless of search path. With comprehensive enforcement, CLI denial provides defense in depth. Normal dependencies, RPATH/RUNPATH, explicit `dlopen`/`dlmopen`, and alternate loaders still require that enforcement. Regression probes must prove a compatible substitute executes in a positive control, then verify denial before its constructor runs through the restricted CLI route.

### Looney Tunables: the sanitizer itself was the attack surface

`CVE-2023-4911` was introduced in glibc 2.34 and publicly disclosed on 2023-10-03. The loader attempted to rewrite a privileged process's tuning string into a safe form. Malformed name/value nesting caused it to copy more than its allocation could hold, before application startup. Qualys demonstrated root compromise on multiple distributions. The allocation came from the loader's minimal allocator, so hardening only the normal malloc implementation would miss the vulnerable phase. [Original Qualys advisory](https://www.qualys.com/2023/10/03/cve-2023-4911/looney-tunables-local-privilege-escalation-glibc-ld-so.txt).

The proposed defense is to stop interpreting these values entirely. Setting a supposedly secure tunable through `GLIBC_TUNABLES` still requires parsing that variable; setting a process environment once also does not bind descendants to a policy. Maintain real kernel `AT_SECURE` semantics rather than pretending every process is setuid.

The fixed upstream implementation first appears in the 2.39 release line; supported older branches received backports. Upstream's fix is `1056e5b4c3f2d90ed2b4a55f96add28da2f4c8fa`. [Upstream advisory](https://github.com/bminor/glibc/blob/master/advisories/GLIBC-SA-2023-0004).

### NSS can turn another program's bug into a library load

Baron Samedit's initial memory corruption belongs to sudo. One Qualys technique altered glibc's heap-resident `service_user` fields so its NSS loader constructed a library name containing a path separator and loaded it. This works without an `LD_PRELOAD` override. Modern glibc has reorganized these structures; the historical structure should not be assumed to exist unchanged. The general dynamic-module primitive remains relevant. [Original Baron Samedit advisory](https://www.openwall.com/lists/oss-security/2021/01/26/3).

The 2025 sudo chroot bug is even more direct: attacker-selected filesystem configuration was enough to select an NSS library while privileged. Sudo's own maintainer identifies the untrusted `nsswitch.conf` and affected versions 1.9.14–1.9.17. [Maintainer disclosure](https://www.openwall.com/lists/oss-security/2025/06/30/3).

**Proposed reduction:** use glibc's built-in `files` and `dns` implementations and return an unavailable/failed lookup for any external provider. Apply the check on every module-load path, including configuration reload and explicit NSS reconfiguration. Preserve legitimate DNS and `/etc/passwd`/`/etc/group` lookup. Enterprise LDAP/SSSD, winbind, mDNS, NIS, and custom identity requirements need application-side integration or provisioned files; do not restore generic external NSS through another profile.

This removes a convenient exploitation route, not arbitrary memory corruption. An attacker may target another function pointer or an already loaded library. Built-in-only NSS also does not make attacker-controlled passwd, group, or hosts data safe; applications must preserve the trust of their configuration and filesystem root.

### PwnKit: a removed environment variable came back

The pkexec flaw let an attacker rewrite an environment entry after loader sanitation. GLib's error-output conversion subsequently used glibc's iconv machinery, which accepted the restored `GCONV_PATH` and loaded the attacker's converter. GLib and glibc are distinct projects. [Original PwnKit advisory](https://www.qualys.com/2022/01/25/cve-2021-4034/pwnkit.txt).

**Proposed reduction:** glibc's internal converter-path lookup should always ignore `GCONV_PATH`, even after `setenv`, `putenv`, or another library changes `environ`. Public application `getenv("GCONV_PATH")` need not be changed. The trust restriction belongs where glibc consumes the setting.

### iconv: a few bytes in a rarely used encoding can compromise PHP

`CVE-2024-2961` is a real glibc bug in ISO-2022-CN-EXT output conversion, publicly disclosed on 2024-04-17. Missing output-space checks permitted a small bounded overwrite. The upstream advisory places the correction in glibc 2.40 and older-branch backports. [Upstream disclosure](https://www.openwall.com/lists/oss-security/2024/04/17/9).

The researcher showed that application features selecting encodings or PHP stream filters could turn it into remote code execution; a later case study reaches the converter through Roundcube's character-set input. Neither needs `GCONV_PATH`. [PHP case study](https://blog.lexfo.fr/iconv-cve-2024-2961-p1.html), [Roundcube case study](https://blog.lexfo.fr/iconv-cve-2024-2961-p2.html).

**Proposed reduction:** keep the existing built-in conversion graph and aliases, skip external registry/cache discovery, reject converter DSO loading, and omit external modules from runtime packages. Built-ins in the inspected source include UTF-8, ASCII, UCS-2/UCS-4 and internal wide-character conversions; **UTF-16 is not built-in**. Inventory application requirements; mail, imports, Samba, and databases needing other encodings must use an explicit application conversion library rather than reopen glibc's module path. The 2026 IBM1390/IBM1399 crash and JIS X 0213 infinite loops reinforce this choice. Wolfi already removes IBM1390 and IBM1399, but package file removal alone is weaker than disabling discovery/loading. [Built-in converters](https://github.com/bminor/glibc/blob/master/iconv/gconv_builtin.h), [CVE-2026-4046](https://nvd.nist.gov/vuln/detail/CVE-2026-4046), [CVE-2026-77117](https://nvd.nist.gov/vuln/detail/CVE-2026-77117), [CVE-2026-80489](https://nvd.nist.gov/vuln/detail/CVE-2026-80489), [Wolfi recipe](https://github.com/wolfi-dev/os/blob/main/glibc-2.44.yaml).

### syslog: argv and failed-authentication messages are hostile input

`CVE-2023-6246`, disclosed on 2024-01-30, overflowed a heap buffer when a long program name made header formatting exceed the short-buffer path. Qualys demonstrated local root compromise through `su` and PAM's failed-authentication logging. Companion `CVE-2023-6779` and `CVE-2023-6780` involve formatting-error/length arithmetic. The vulnerable rewrite had itself been backported as a fix for `CVE-2022-39046`, a log disclosure. [Original syslog advisory](https://www.openwall.com/lists/oss-security/2024/01/30/6), [upstream syslog advisory](https://github.com/bminor/glibc/blob/master/advisories/GLIBC-SA-2024-0001).

**Required defense:** keep syslog but test long `argv[0]`, explicit long `openlog` identities, negative formatter results, arithmetic overflow, and allocation failure. Treat header and message lengths independently. An environment denylist does not fix this.

### OpenSSH regreSSHion: correct libc calls used at the wrong time

`CVE-2024-6387` is an OpenSSH signal-handler regression, not a glibc CVE. The handler could invoke functions such as syslog that are not async-signal-safe, allowing heap corruption through interruption and reentrancy. OpenSSH's 9.8 release notes report lab exploitation on 32-bit Linux/glibc and carefully distinguish that from the then-unproven 64-bit case. [OpenSSH 9.8 release notes](https://www.openssh.org/txt/release-9.8), [Qualys technical advisory](https://www.qualys.com/2024/07/01/cve-2024-6387/regresshion.txt).

**Required defense:** fix the application's signal handling. Do not advertise an environment-free libc as preventing this class, or attempt to make every complex libc function signal-safe by adding locks. Locks can introduce deadlocks, and such a rewrite would be much larger than the intended patch set.

### realpath: kernel semantics, namespaces, locale data, and format strings combined

`CVE-2018-1000001` combined Linux's unreachable-current-directory representation with glibc's assumption that `getcwd` returned an absolute path. `realpath` could write before its buffer. The original researcher exploited setuid **umount**, not sudo, and used locale/translation state and formatted error output in the chain. [Original RationalLove report](https://www.halfdog.net/Security/2017/LibcRealpathBufferUnderflow/).

**Required defense:** keep canonicalization patched; test unreachable directories, namespace changes, and short buffers. Restrict locale/catalog paths and preserve Werewolf's namespace and privilege controls. Do not assume that chroot or a mount namespace makes name resolution safer.

### Stack Clash and the 2026 $ORIGIN bugs survive a narrow env policy

The 2017 glibc Stack Clash and follow-on loader reports used path expansion and large environment inputs to influence stack/heap layout. Sudo's `CVE-2017-1000367` was a separate component flaw; grouping the disclosure does not make them one CVE. [Original Stack Clash research](https://www.qualys.com/2017/06/19/stack-clash/stack-clash.txt), [loader follow-on disclosure](https://www.openwall.com/lists/oss-security/2017/12/11/4).

Recent `CVE-2026-86805` and `CVE-2026-95818` affect `$ORIGIN` expansion in ELF **RPATH/RUNPATH**, not merely `LD_LIBRARY_PATH`. One is a normalized-path-versus-opened-path race with specific hardlink/privileged-binary prerequisites; the other corrupts a loader stack buffer. [Race record](https://nvd.nist.gov/vuln/detail/CVE-2026-86805), [overflow record](https://nvd.nist.gov/vuln/detail/CVE-2026-95818).

**Baseline defense outside the five reductions:** retain upstream `$ORIGIN` fixes, stack-clash compiler protection, protected-hardlink controls, and trusted application RPATHs. Broader path-syntax restrictions need a demonstrated use case before adding patch surface. Removing all `$ORIGIN` support could break relocatable application bundles without improving their hot paths.

### No-CVE qsort hardening still belongs in the regression corpus

In January 2024, Qualys described out-of-bounds accesses when an application's qsort comparator was nontransitive and allocation pressure selected an old fallback algorithm. Upstream attributed the invalid comparator contract to the application and did not treat it as a glibc vulnerability. Newer code used a safer fallback. [Original qsort report](https://www.qualys.com/2024/01/30/qsort.txt).

**Required defense:** retain sorting and its optimized normal path, but test allocation failure and adversarial comparators for containment. Record this as robustness hardening, not an invented CVE or proof that arbitrary comparator code can be sandboxed by libc.

## Critical-score records: explicit review list

All CVSS v3/v4 Critical records found in the expanded glibc collection are listed here, including disputed and historical cases. The full inventory gives publication dates and per-record links. Scores are from NVD unless identified otherwise.

| Records | Score | Actual weakness and applicability |
| --- | --- | --- |
| CVE-2014-9761 | 9.8 | `nan` family stack overflow; early-2016 context outside exact window. |
| CVE-2015-8776 | 9.1 | `strftime` out-of-range time handling; early-2016 context. |
| CVE-2015-8778, CVE-2015-8779 | 9.8 each | Hash-table allocation arithmetic and message-catalog stack sizing; early-2016 context. |
| CVE-2014-9984 | 9.8 | nscd netgroup buffer sizing; published in 2017, vulnerable implementation predates window. |
| CVE-2017-15670, CVE-2017-15804 | 9.8 each | `glob` tilde/user-name expansion overflows. |
| CVE-2018-6485, CVE-2018-6551 | 9.8 each | Allocation integer overflows; latter specifically old powerpc/i386 paths. |
| CVE-2017-18269 | 9.8 | i386 SSE2 overlap handling; not the native x86_64/aarch64 ABI. |
| CVE-2018-11236 | 9.8 | Long-path `realpath` arithmetic on 32-bit targets. |
| CVE-2019-9169 | 9.8 | Regex out-of-bounds read; input control and application use determine impact. |
| CVE-2019-1010022 | 9.8 | **Disputed** stack-guard bypass claim requiring another flaw; not standalone remote RCE evidence. |
| CVE-2021-33574 | 9.8 | `mq_notify` thread-attribute lifetime error. |
| CVE-2021-35942 | 9.1 | `wordexp` parameter-index arithmetic and arbitrary read. |
| CVE-2022-23218, CVE-2022-23219 | 9.8 each | Legacy SunRPC local-path/hostname stack copies. |
| CVE-2023-25139 | 9.8 | Locale grouping/field-width output overflow in `sprintf`. |
| CVE-2023-0687 | 9.8 NVD / 4.6 CNA | **Disputed** profiling startup bounds; database “Critical” conflicts with the stated trust prerequisites and CNA rating. |
| CVE-2026-5450 | 9.8 CNA/ADP | `%mc` scanf heap off-by-one with explicit width greater than 1024. |
| CVE-1999-0199, CVE-2005-3590 | 9.8 each | **Historical backfills:** documentation/dangling-pointer misuse and zero-size group-list buffer; not discoveries from this decade. |

Outside glibc's own list, sudo's chroot CVE has a **9.3 CNA / 7.8 NVD** split. Conversely, Looney Tunables is **7.8** and iconv's PHP-exploitable bug is **7.3**. They still deserve top engineering priority. Raw severity alone is a poor feature-selection rule.

## Weakness families and proposed restrictions

The matrix preserves the wider research inventory. The first five mechanisms above are selected for initial implementation; other suggestions are compatibility research or upstream/configuration maintenance, not authorization to add more patches or exported-API stubs. Preserve symbol versions and public environment APIs.

| Surface | Proposed policy | History or exploitation mechanism | Performance and compatibility consequences |
| --- | --- | --- | --- |
| Loader `LD_*` environment | Ignore all loader configuration input; remove its value parsing. Cover static `dl-support` as well as dynamic `rtld`. | 2017 path overflows; 2019 ASLR override; 2025 static-setuid path injection; 2023 tunables. | No reason to lose vector dispatch or steady-state throughput. Breaks preload shims and env-based library layouts; PostgreSQL setup is known to depend on one. |
| Environment tunables and `MALLOC_*` aliases | No external environment tuning; retain compiled defaults and CPU-detected defaults. | Looney Tunables; tuning can also weaken protection or change memory layout. | Keep tcache, arenas, thresholds, and CPU selection. A legitimate performance workaround must become a reviewed build setting, not an arbitrary environment override. |
| Loader injection/search overrides beyond environment | Disable `/etc/ld.so.preload`, `--preload`, `--audit`, `--library-path`, ELF `DT_AUDIT`/`DT_DEPAUDIT`, and CLI search-policy overrides including inhibit/cache and hwcaps directories. | Code-injection facilities and library substitution do not require an env override. | Normal dependencies and explicit loading remain under verified-root enforcement. Breaks instrumentation, injected sanitizers, and preload-based initialization. |
| External NSS providers | Built-in `files`/`dns` only; reject external load requests and unexpected module names at the loader. | sudo 2021/2025; 2023 resolver plugin lifetime bugs. | Adapt identity requirements at application/provisioning layer; no permissive profile. This is not the Mozilla NSS TLS library. |
| nscd daemon and client paths | Use `--disable-build-nscd` and `--disable-nscd`; the inspected configure script has no `--disable-nscd-cache` option. | Netgroup overflow/double free/ownership errors, 2024 cache failures, 2026 client race and daemon stack exhaustion. | May increase direct lookup cost; measure DNS and account lookup workload. Application or trusted resolver caching can replace caching where needed. |
| Character conversion | Built-in graph/aliases only; skip external registry/cache discovery and reject converter DSOs. | PwnKit; ISO-2022-CN-EXT; recurring IBM, JIS, EUC bugs. | Preserve built-in conversion paths; UTF-16 and legacy encodings require application-side conversion. |
| Locale and message catalogs | Ignore `LANG`, `LANGUAGE`, all `LC_*`, `LOCPATH`/`NLSPATH`, and output overrides. Empty-name locale requests select `C`. | realpath exploit chain; `strxfrm`, `sprintf`, catalog parsing; locale names can affect allocation/layout. | C is not C.UTF-8; database collation and multibyte conversion need application review. Explicit programmatic locale requests are separate API inputs. |
| Timezone selection | Ignore `TZ` and `TZDIR`; use the existing system default from `/etc/localtime`. | File/parser reachability; early `strftime` and format bugs illustrate length/range hazards. | Zone handling remains cached. Applications needing another zone use explicit application logic; no new libc selector/parser. |
| Resolver env overrides | Ignore `LOCALDOMAIN`, `RES_OPTIONS`, `HOSTALIASES`, `RESOLV_HOST_CONF`, `RESOLV_*`, and unused legacy NIS/Hesiod settings. | DNS override/input validation; 2026 long search-domain assertion. | Keep `/etc/resolv.conf`, hosts and ordinary DNS. Network-provided config still needs validation; env removal does not make DNS packets trusted. |
| Network database legacy APIs | Consider disabling `networks: dns`; keep `getaddrinfo`, normal host lookups, TCP fallback and bounded DNS parsing. | getnetbyname overflow; getnetbyaddr leak; reverse-response section/name validation. | `networks` DNS is distinct from hosts DNS. Avoid breaking the common resolver to remove rarely used APIs. |
| Retained dependency/search paths | Audit the built image's cache, RPATH/RUNPATH, `$ORIGIN`, dependencies and symlinks; require verified-root executable mapping enforcement for the production code-integrity claim. Keep upstream path fixes. | 2017 and 2026 `$ORIGIN` vulnerabilities; normal dependency search and explicit loading can substitute untrusted libraries without env input. | Keep supported packaged bundles and plugins from the selected root. Avoid a new libc path-policy parser; arbitrary explicit dlopen is not disabled by the five patches. |
| Executable stack requests | Refuse executable-stack upgrades by the loader; choose and verify safe compiled execstack policy; test `PT_GNU_STACK` and subsequent loads. | A built-in exploit enabler; not itself a CVE in this inventory. | GCC trampolines and some old native components can break. JIT executable mappings are a separate mechanism; this is not a general JIT ban. |
| Legacy SunRPC/NIS/Hesiod | Do not ship unused helpers/modules; assess compatibility symbols before stubbing APIs. Removing an NSS provider does not automatically remove SunRPC code. | XDR failure leaks; UDP RPC UAF/stack overflow; `clnt_create`/`svcunix_create` overflows. | Modern software may use libtirpc independently, which this build does not harden. Legacy applications need explicit verification. |
| `wordexp` shell expansion | Candidate: export a predictable refusal for all expansion if applications do not need it; otherwise at least block command substitution and bound stack allocations. | 2021 arbitrary read; 2025/2026 invalid ownership; 2026 stack clash. | No cost to normal programs that do not call it. Breaks consumers relying on shell expansion. `WRDE_NOCMD` alone does not remove the other parsing bugs. |
| Profiling/tracing/debug DSOs | Omit `libc_malloc_debug`, memusage/pcprofile/sotruss and unused profiling tools from runtime; disable automatic output paths. | Extra allocation hooks, file writes, loader audit paths; disputed gmon record. | External profilers may be affected. Keep unwind/backtrace support when applications need it; performance profiling and call tracing are different features. |
| General formatting, regex, sorting, allocator, random APIs | Keep, patch, and test limits/error/lifetime semantics. Review `%n` separately; never assume it is a drop-in removal. | Most non-loader CVEs; qsort robustness; random/fork errors; library ABI faults. | Broad removal would break applications or reduce speed. `%n` is used internally (including syslog formatting), so a global ban needs redesign and compatibility work. |
| New system-wide tunables/cache extensions | Inventory and choose a compile-time or immutable reviewed policy; do not enable arbitrary runtime policy through untrusted caches. | Latest upstream documentation permits cache-stored tuning, including effects in privileged programs. | Can retain carefully selected tuning, but adds parser/policy input outside the environment. Verify whether the selected release actually implements it. |

Source anchors for implementation research: [rtld](https://github.com/bminor/glibc/blob/master/elf/rtld.c), [static loader support](https://github.com/bminor/glibc/blob/master/elf/dl-support.c), [tunables](https://github.com/bminor/glibc/blob/master/elf/dl-tunables.c), [NSS module loading](https://github.com/bminor/glibc/blob/master/nss/nss_module.c), [gconv cache](https://github.com/bminor/glibc/blob/master/iconv/gconv_cache.c), [locale lookup](https://github.com/bminor/glibc/blob/master/locale/findlocale.c), [timezone file lookup](https://github.com/bminor/glibc/blob/master/time/tzfile.c), [system-wide tunables](https://sourceware.org/glibc/manual/latest/html_node/System_002dwide-Tunables.html). Source layout and knobs must be checked at Wolfi's pinned commit, not assumed from these moving branch links.

## Minimal environment policy

The policy applies to **glibc interpreting configuration**, not applications receiving environment variables. An application may still need database URLs, credentials, feature settings, proxy configuration, or arbitrary variables through `getenv`; filtering that public API would be a different and much more disruptive product.

| Class | Initial decision |
| --- | --- |
| `HOME`, `PATH`, `PWD` | Application getenv remains normal. Internal blanket denial changes tilde expansion, exec path search, and current-directory shortcuts. Prefer explicit application paths; preserve an internal API lookup only for a demonstrated requirement after reviewing a workaround. This is not a generic variable allowlist. |
| `LANG`, individual `LC_*`, `LC_ALL`, `LANGUAGE` | Ignore all for glibc configuration. `setlocale(category, "")` selects `C`; initially the locale is already C. No C.UTF-8 fallback or environment locale opt-out. |
| `TZ`, `TZDIR` | Ignore both; use existing `/etc/localtime` default. |
| `TMPDIR` | Ignore for internal configuration; use existing fixed fallback. Application getenv and explicit temp paths remain available. |
| Loader/tunables/converter/locale-path/resolver-path variables | Ignored by internal readers, including values introduced after startup. |
| `DATEMSK`, `GETCONF_DIR`, `GETTEXT_LOG_UNTRANSLATED`, `GMON_OUT_PREFIX`, malloc tracing and other file/output overrides | Default deny inside glibc; identify callers requiring them before any exception. |
| `POSIXLY_CORRECT`, `ARGP_HELP_FMT`, `MSGVERB`, `SEV_LEVEL`, `OUTPUT_CHARSET`, Windows-only HOME aliases, legacy NIS/Hesiod settings | Deny internal configuration. Some are presentation knobs; others influence parsing or converter selection. Do not classify them all as vulnerabilities. Explicit expansion APIs such as wordexp need separate semantic review, including IFS and direct environ access. |

A macro redirect in glibc's **private build header** is a plausible small enforcement point for normal internal lookups, without installing that macro in application headers. Redirect internal `getenv` and secure-getter variants to a typed NULL-returning helper; exempt the public getter definitions and their symbol/alias machinery so application `getenv` and `secure_getenv` keep existing semantics. Preserve actual secure-execution sanitation and environment bookkeeping. A literal global `#define getenv(...) NULL` is not a complete, validated patch: it can rewrite definitions, miss aliases or direct scans, and affect build tools/tests. Validate static/shared builds and inspect every caller. [Internal header](https://github.com/bminor/glibc/blob/master/include/stdlib.h), [hidden symbol machinery](https://github.com/bminor/glibc/blob/master/include/libc-symbols.h), [public getenv](https://github.com/bminor/glibc/blob/master/stdlib/getenv.c).

This preserves **public environment access**, not every C API's previous behavior: `execvp` uses PATH, glob tilde expansion uses HOME, wordexp expands environment values, and getopt consults POSIXLY_CORRECT. Prefer application workarounds before retaining any required lookup. There is no single hook that denies all configuration while leaving all environment-dependent APIs unchanged. The loader and tunables initialization directly walk `environ`/`envp`; function arguments and external configuration also bypass getenv. Cover those startup consumers and the selected module activation points separately. [exec path search](https://github.com/bminor/glibc/blob/master/posix/execvpe.c), [glob](https://github.com/bminor/glibc/blob/master/posix/glob.c), [wordexp](https://github.com/bminor/glibc/blob/master/posix/wordexp.c), [tunables scan](https://github.com/bminor/glibc/blob/master/elf/dl-tunables.c).

Do not force `__libc_enable_secure=1` globally as a shortcut: it changes `secure_getenv` and other behavior unrelated to the intended restriction. Ordinary kernel-designated secure execution must continue to work correctly. [secure_getenv implementation](https://github.com/bminor/glibc/blob/master/stdlib/secure-getenv.c), [loader secure-execution documentation](https://man7.org/linux/man-pages/man8/ld.so.8.html).

## Performance and Werewolf-specific implications

The initial goal should be **eliminate configuration and optional loading surfaces while leaving hot implementations intact**. Keep optimized memcpy/memmove/memcmp and string routines; IFUNC and detected hardware capabilities; vector math; malloc/tcache/arenas; threads; ordinary DNS; and normal language-runtime/plugin loading. PowerPC, ARMv7, i386 and x32 cases in the inventory demonstrate why architecture must be recorded, not why x86_64/aarch64 dispatch should be removed.

Expected effects: fewer startup probes and no unwanted preload/audit work; roughly unchanged hot allocation/copy/math paths; potentially more direct name lookups if nscd caching is removed. These need measurements. Full eager binding can improve relocation integrity but add startup work; Wolfi's `--enable-bind-now` hardens glibc's own build and is not proof that every application/plugin has full RELRO. [Inspected Wolfi recipe](https://github.com/wolfi-dev/os/blob/169ac2651022405ddce73adf2383b923039e4f05/glibc-2.44.yaml).

The current Werewolf source has clean-environment construction in `cmd/init/init.zig` and `cmd/leash/leash.zig`, and service settings reject `LD_*` in `lib/settings.zig`. Those are useful launch controls, but do not bind a program's later children or internal environment changes to policy. Its posture includes a `nosuid` requirement for all mounts, and forms can still have privileged long-running processes such as sshd. This audit has inspected source, not attested a running deployment. Blocking setuid effects reduces several historical paths; it does not protect every root process, remote parser, or data-corruption attack.

PostgreSQL's `forms/postgresql/cmd/pg-init/pg-init.zig` explicitly sets `LD_PRELOAD` for `popen-shim.so`. Separate application work must replace shell-based setup with direct argument-vector execution and explicit locale handling, covering initdb, locale enumeration, and bootstrap/server children. Linking the shim only into initdb would not cover child processes. Do not introduce a preload exception, added shell, or permissive libc. Database collation, Python/PHP/Ruby extension loading, Java/JIT behavior, mail encodings, DNS, TLS, NSS identity, and application bundle RUNPATHs all need validation before claiming drop-in compatibility.

Replacing package files through an overlay is insufficient for production: APK dependency resolution, loader/libc version coherence, and the updater must retain the hardened package. The custom source must continue receiving Wolfi/upstream fixes; removing features is defense in depth, not a reason to stop backports.

## Required release gates derived from this history

1. **Source and package provenance:** pin Wolfi recipe plus supporting files and source commit; retain upstream hardening; prove relevant fix ancestry or equivalent backports; sign/index matching libc and loader packages; record removed features in release metadata. Fail closed on upstream patch drift rather than using fuzzy application.
2. **Environment invariance:** run the same test program with normal, long, duplicated, malformed and post-startup-added `LD_*`, `GLIBC_TUNABLES`, `MALLOC_*`, `GCONV_PATH`, `LANG`, `LANGUAGE`, all `LC_*`, `LOCPATH`, timezone, resolver and output-path controls. Disabled inputs must not cause loads, writes, crashes, or policy changes; empty-name locale selection must yield C. Verify public getters/mutation, ordinary and real secure execution, and documented changes to environment-dependent APIs.
3. **Alternate injection routes:** attempt attacker DSO constructors via loader CLI (including `--library-path`), preload file, ELF audit tags, NSS reload/reconfiguration, alternate roots, and converter aliases/cache entries. Separately probe RPATH/RUNPATH (including relative/empty elements), `$ORIGIN`, dependencies, writable-target symlinks, explicit `dlopen`/`dlmopen`, static loading, alternate loaders and executable mappings under production IPE. Untrusted native code must fail before constructor execution. Run IPE exec/mmap/mprotect and tampered-policy checks on both architectures. A passing `LD_PRELOAD` test alone is insufficient.
4. **Size and failure edges:** test SIZE_MAX/PTRDIFF_MAX allocation boundaries, large alignments, long identities/pathnames, one-byte/zero-byte buffers, negative formatting results, scanf widths, short converter output buffers, allocation-failure cleanup, regex complexity limits, qsort fallback, and wordexp reuse/append ownership. Use sanitizers in dedicated builds rather than shipping instrumentation in the runtime.
5. **Protocol and concurrency:** fuzz malformed DNS packets including TCP fallback, reverse records, no-AAAA mode, large search lists, and invalid hostnames; exercise resolver and random APIs around fork, cache races, mq_notify callback lifetimes, locale changes, and concurrent environment updates. Application signal-handler audits remain separate.
6. **Native architecture tests:** execute on both x86_64 and aarch64, with the supported CPU implementations selected by default. Add guard-page and high-boundary tests for vector string/copy routines; preserve the intended ABI and symbol versions. A build on one architecture cannot validate the other's assembler.
7. **Behavior and speed:** smoke-test the supported Werewolf forms and compare against the same unpatched Wolfi build. Measure process startup, malloc throughput/contention/RSS, memcpy/memmove sizes and alignments, string comparisons, DNS/identity lookup, TLS service throughput, and representative database/application traffic. Agree on regression thresholds before publishing a “no performance cost” claim.

The remaining exposure includes memory-safety bugs in indispensable APIs, hostile DNS and application data, incorrect API use, signals, and arbitrary corruption of process state. Removing module-loading facilities can block particular exploitation routes; it cannot turn C process memory into a security boundary.

## Detailed CVE inventory

The tables below include every identifier in the expanded collection. “Env” is a feature-reduction classification, not proof that stripping the environment fixes the CVE. “Keep/fix” means the API is useful and needs the upstream correction and regression coverage. “Remove optional” and “restrict” require the compatibility decisions above. Publication dates are record dates. An asterisk on the date marks early-2016 context outside the exact ten-year window. Historical backfills and disputed reports are labeled in the description.

Scores are `NVD v3` where available, otherwise `CNA/ADP v3` or `CNA/ADP v4`; `—` means no applicable score was obtained. Every CVE link leads to its record, with technical references and affected/fixed-version information. Selected original advisories appear in the case studies. All inventory entries except CVE-2023-4911 are absent from this KEV snapshot.

### Records published in 2016

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2015-8777](https://nvd.nist.gov/vuln/detail/CVE-2015-8777) | 2016-01-20* | 5.5 NVD v3 | Loader accepts LD_POINTER_GUARD=0 and weakens pointer protection. | Env: disable loader controls; retain pointer guarding. |
| [CVE-2015-7547](https://nvd.nist.gov/vuln/detail/CVE-2015-7547) | 2016-02-18* | 8.1 NVD v3 | DNS A/AAAA response handling overflows resolver stack buffers; remote attack through getaddrinfo. | Keep/fix DNS; malformed UDP/TCP regression corpus. |
| [CVE-2016-2856](https://nvd.nist.gov/vuln/detail/CVE-2016-2856) | 2016-03-14* | 8.4 NVD v3 | Distribution-enabled pt_chown lacks a namespace/FD check; terminal access or privilege abuse. Upstream discouraged the option. | Remove optional setuid helper; distribution configuration matters. |
| [CVE-2015-5229](https://nvd.nist.gov/vuln/detail/CVE-2015-5229) | 2016-04-08* | 7.5 NVD v3 | RHEL-specific calloc regression fails to initialize memory correctly. | Keep/fix allocator; zeroing regression, not an env issue. |
| [CVE-2014-9761](https://nvd.nist.gov/vuln/detail/CVE-2014-9761) | 2016-04-19* | 9.8 NVD v3 | Long nan/nanf/nanl payload argument overflows a stack buffer. | Keep/fix numeric conversion; bound argument processing. |
| [CVE-2015-8776](https://nvd.nist.gov/vuln/detail/CVE-2015-8776) | 2016-04-19* | 9.1 NVD v3 | Out-of-range time value causes strftime overflow/disclosure. | Keep/fix time formatting; range and boundary tests. |
| [CVE-2015-8778](https://nvd.nist.gov/vuln/detail/CVE-2015-8778) | 2016-04-19* | 9.8 NVD v3 | hcreate_r size arithmetic wraps and accesses heap outside its allocation. | Keep/fix checked arithmetic; no safe feature shortcut. |
| [CVE-2015-8779](https://nvd.nist.gov/vuln/detail/CVE-2015-8779) | 2016-04-19* | 9.8 NVD v3 | Long catalog name overflows catopen's stack buffer. | Restrict catalogs and NLSPATH; keep required API patched. |
| [CVE-2016-1234](https://nvd.nist.gov/vuln/detail/CVE-2016-1234) | 2016-06-01* | 7.5 NVD v3 | glob's alternate-directory callbacks supply a long name that exhausts/overflows stack storage. | Keep/fix glob; validate callback lengths. |
| [CVE-2016-3075](https://nvd.nist.gov/vuln/detail/CVE-2016-3075) | 2016-06-01* | 7.5 NVD v3 | nss_dns getnetbyname long-name handling consumes/overflows stack. | Remove unused networks DNS path; keep hosts DNS patched. |
| [CVE-2016-3706](https://nvd.nist.gov/vuln/detail/CVE-2016-3706) | 2016-06-10* | 7.5 NVD v3 | getaddrinfo hostent conversion overflow; incomplete earlier fix. | Keep/fix DNS/NSS conversion; regression for incomplete fixes. |
| [CVE-2016-4429](https://nvd.nist.gov/vuln/detail/CVE-2016-4429) | 2016-06-10* | 5.9 NVD v3 | SunRPC clntudp_call error handling overflows stack on crafted ICMP/UDP traffic. | Remove optional legacy RPC; independently patch libtirpc users. |
| [CVE-2016-6323](https://nvd.nist.gov/vuln/detail/CVE-2016-6323) | 2016-10-07* | 7.5 NVD v3 | ARM32 makecontext/unwinder incompatibility can hang gccgo-style applications. | Architecture-specific keep/fix; not native aarch64. |

### Records published in 2017

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2016-5417](https://nvd.nist.gov/vuln/detail/CVE-2016-5417) | 2017-02-17 | 7.5 NVD v3 | Partial IPv6 resolver initialization leaks memory across use. | Keep/fix resolver; test failed initialization/retry. |
| [CVE-2016-10228](https://nvd.nist.gov/vuln/detail/CVE-2016-10228) | 2017-03-02 | 5.9 NVD v3 | iconv CLI suffix/error handling can loop forever on invalid multibyte input. | Restrict converters; bound progress and error recovery. |
| [CVE-2015-8982](https://nvd.nist.gov/vuln/detail/CVE-2015-8982) | 2017-03-15 | 8.1 NVD v3 | strxfrm length arithmetic wraps into a stack overflow on long strings. | Keep/fix collation; locale reduction is partial mitigation. |
| [CVE-2015-8983](https://nvd.nist.gov/vuln/detail/CVE-2015-8983) | 2017-03-20 | 8.1 NVD v3 | Wide-stream buffer expansion wraps a byte-size calculation and overflows heap. | Keep/fix stdio size arithmetic. |
| [CVE-2015-8984](https://nvd.nist.gov/vuln/detail/CVE-2015-8984) | 2017-03-20 | 5.9 NVD v3 | Malformed fnmatch pattern causes an out-of-bounds read. | Keep/fix pattern parser; input/resource limits. |
| [CVE-2015-8985](https://nvd.nist.gov/vuln/detail/CVE-2015-8985) | 2017-03-20 | 5.9 NVD v3 | Regex failure-stack handling can assert and terminate the caller. | Keep/fix regex; pathological/error-state tests. |
| [CVE-2017-8804](https://nvd.nist.gov/vuln/detail/CVE-2017-8804) | 2017-05-07 | 7.5 NVD v3 | SunRPC XDR decode failure leaves allocations behind. Upstream disputed library-versus-caller ownership responsibility. | Remove optional RPC; test caller cleanup contracts. |
| [CVE-2014-9984](https://nvd.nist.gov/vuln/detail/CVE-2014-9984) | 2017-06-12 | 9.8 NVD v3 | nscd netgroup reply allocation uses an incorrect buffer size; crash or code execution. | Remove optional nscd; old implementation, 2017 record. |
| [CVE-2017-1000366](https://nvd.nist.gov/vuln/detail/CVE-2017-1000366) | 2017-06-19 | 7.8 NVD v3 | Loader stack/heap layout can be manipulated using large LD_LIBRARY_PATH inputs (Stack Clash). | Env plus bounded loader allocation and compiler stack probing. |
| [CVE-2015-5180](https://nvd.nist.gov/vuln/detail/CVE-2015-5180) | 2017-06-27 | 7.5 NVD v3 | res_query error handling dereferences null on hostile input. | Keep/fix resolver error paths. |
| [CVE-2017-12132](https://nvd.nist.gov/vuln/detail/CVE-2017-12132) | 2017-08-01 | 5.9 NVD v3 | Oversized EDNS UDP replies/fragmentation make off-path DNS spoofing easier. | Keep/fix resolver protocol policy; do not blindly remove TCP fallback. |
| [CVE-2017-12133](https://nvd.nist.gov/vuln/detail/CVE-2017-12133) | 2017-09-07 | 5.9 NVD v3 | SunRPC clntudp_call error path uses freed storage. | Remove optional RPC; preserve correct ownership if retained. |
| [CVE-2011-5320](https://nvd.nist.gov/vuln/detail/CVE-2011-5320) | 2017-10-18 | 6.2 NVD v3 | Huge zero-filled scanf input can crash old implementations; late-indexed historical flaw. | Keep/fix scanf; do not treat the 2017 record as newly vulnerable modern code. |
| [CVE-2017-15670](https://nvd.nist.gov/vuln/detail/CVE-2017-15670) | 2017-10-20 | 9.8 NVD v3 | glob tilde/home-directory expansion has a heap off-by-one. | Keep/fix glob; restrict optional expansion only after compatibility audit. |
| [CVE-2017-15671](https://nvd.nist.gov/vuln/detail/CVE-2017-15671) | 2017-10-20 | 5.9 NVD v3 | glob tilde/user-name expansion leaks memory on long inputs. | Keep/fix glob; bound work/length. |
| [CVE-2017-15804](https://nvd.nist.gov/vuln/detail/CVE-2017-15804) | 2017-10-22 | 9.8 NVD v3 | glob user-name unescaping overflows its output storage. | Keep/fix glob parser. |
| [CVE-2017-17426](https://nvd.nist.gov/vuln/detail/CVE-2017-17426) | 2017-12-05 | 8.1 NVD v3 | New tcache path misses an overflow check near SIZE_MAX and returns too-small allocations. | Keep/fix tcache; retain its performance and test boundaries. |
| [CVE-2017-16997](https://nvd.nist.gov/vuln/detail/CVE-2017-16997) | 2017-12-18 | 7.8 NVD v3 | Privileged ORIGIN RPATH/RUNPATH processing can interpret an empty token as current directory. | Restrict ELF search paths; env-only blocking is insufficient. |

### Records published in 2018

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2018-1000001](https://nvd.nist.gov/vuln/detail/CVE-2018-1000001) | 2018-01-31 | 7.8 NVD v3 | realpath underflows its buffer after getcwd returns an unreachable path; demonstrated umount chain. | Keep/fix path semantics; namespace and catalog restrictions. |
| [CVE-2017-1000408](https://nvd.nist.gov/vuln/detail/CVE-2017-1000408) | 2018-02-01 | 7.8 NVD v3 | Loader allocation leak amplifies through LD_HWCAP_MASK; related Stack Clash prerequisites. | Env; retain CPU autodetection and loader memory bounds. |
| [CVE-2017-1000409](https://nvd.nist.gov/vuln/detail/CVE-2017-1000409) | 2018-02-01 | 7.0 NVD v3 | Expanded LD_LIBRARY_PATH tokens overflow path-entry storage; hardlink/layout prerequisites. | Env and checked post-expansion sizing. |
| [CVE-2018-6485](https://nvd.nist.gov/vuln/detail/CVE-2018-6485) | 2018-02-01 | 9.8 NVD v3 | memalign/posix_memalign integer overflow yields too-small allocations. | Keep/fix allocator size/alignment checks. |
| [CVE-2018-6551](https://nvd.nist.gov/vuln/detail/CVE-2018-6551) | 2018-02-02 | 9.8 NVD v3 | Near-SIZE_MAX malloc requests are mishandled in old powerpc/i386 paths. | Keep/fix allocator; architecture-specific regression. |
| [CVE-2017-18269](https://nvd.nist.gov/vuln/detail/CVE-2017-18269) | 2018-05-18 | 9.8 NVD v3 | i386 SSE2 memmove overlap test is wrong across address-space boundaries. | Keep/fix optimized routines; old 32-bit ABI, not x86_64 native. |
| [CVE-2018-11236](https://nvd.nist.gov/vuln/detail/CVE-2018-11236) | 2018-05-18 | 9.8 NVD v3 | Very long realpath input overflows size arithmetic on 32-bit systems. | Keep/fix canonicalization; native 64-bit applicability differs. |
| [CVE-2018-11237](https://nvd.nist.gov/vuln/detail/CVE-2018-11237) | 2018-05-18 | 7.8 NVD v3 | x86 AVX-512 mempcpy implementation can write past destination. | Keep/fix CPU-specific copy routine; guard-page tests. |
| [CVE-2018-19591](https://nvd.nist.gov/vuln/detail/CVE-2018-19591) | 2018-12-04 | 7.5 NVD v3 | Crafted hostname/scope lookup leaks a socket descriptor through if_nametoindex. | Keep/fix resolver; FD exhaustion tests. |

### Records published in 2019

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2019-6488](https://nvd.nist.gov/vuln/detail/CVE-2019-6488) | 2019-01-18 | 7.8 NVD v3 | x32 ABI string assembler mishandles upper size-register bits and can corrupt/crash. | Keep/fix ABI-specific routines; x32 is distinct from x86_64. |
| [CVE-2016-10739](https://nvd.nist.gov/vuln/detail/CVE-2016-10739) | 2019-01-21 | 5.3 NVD v3 | getaddrinfo accepts IPv4 text with trailing whitespace/data; callers can misvalidate attacker strings. | Keep/fix strict address parsing; application validation remains necessary. |
| [CVE-2019-7309](https://nvd.nist.gov/vuln/detail/CVE-2019-7309) | 2019-02-03 | 5.5 NVD v3 | x32 memcmp can incorrectly report equality because of register-width handling. | Keep/fix ABI correctness; security comparisons can depend on it. |
| [CVE-2009-5155](https://nvd.nist.gov/vuln/detail/CVE-2009-5155) | 2019-02-26 | 7.5 NVD v3 | Regex alternation misparse causes incorrect results or assertion failure; old flaw recorded in 2019. | Keep/fix regex; distinguish age from publication date. |
| [CVE-2018-20796](https://nvd.nist.gov/vuln/detail/CVE-2018-20796) | 2019-02-26 | 7.5 NVD v3 | Regex matching can recurse without a bound on pathological patterns; disputed/treated as complexity misuse. | Keep regex with application resource limits; do not promise a parser-free cure. |
| [CVE-2019-9169](https://nvd.nist.gov/vuln/detail/CVE-2019-9169) | 2019-02-26 | 9.8 NVD v3 | Case-insensitive regex processing reads outside heap storage. | Keep/fix regex and locale boundary tests. |
| [CVE-2019-9192](https://nvd.nist.gov/vuln/detail/CVE-2019-9192) | 2019-02-26 | 7.5 NVD v3 | Another pathological regex recursion case; upstream explicitly disputes the vulnerability classification. | Keep with complexity limits; disputed, not a confirmed RCE. |
| [CVE-2005-3590](https://nvd.nist.gov/vuln/detail/CVE-2005-3590) | 2019-04-10 | 9.8 NVD v3 | Historical getgrouplist writes despite a zero-sized destination; NVD backfilled in 2019. | Historical context; keep fixed group lookup. |
| [CVE-2006-7254](https://nvd.nist.gov/vuln/detail/CVE-2006-7254) | 2019-04-10 | 5.5 NVD v3 | Historical nscd unhandled-client sockets leak; NVD backfilled in 2019. | Remove optional nscd; not a new 2019 discovery. |
| [CVE-2019-1010022](https://nvd.nist.gov/vuln/detail/CVE-2019-1010022) | 2019-07-15 | 9.8 NVD v3 | Disputed stack-guard bypass claim in nptl requiring an existing stack corruption flaw. | Keep protections; do not equate its Critical score with standalone RCE. |
| [CVE-2019-1010023](https://nvd.nist.gov/vuln/detail/CVE-2019-1010023) | 2019-07-15 | 8.8 NVD v3 | Disputed ldd/ELF execution claim; inspecting untrusted executables can run code. | Omit ldd from runtime; use non-executing ELF inspection in build tools. |
| [CVE-2019-1010024](https://nvd.nist.gov/vuln/detail/CVE-2019-1010024) | 2019-07-15 | 5.3 NVD v3 | Disputed ASLR predictability through cached thread stack/heap state. | Keep caches; threat-model/measure before expensive randomization changes. |
| [CVE-2019-1010025](https://nvd.nist.gov/vuln/detail/CVE-2019-1010025) | 2019-07-15 | 5.3 NVD v3 | Disputed ability to predict a created thread's heap addresses. | Keep threads/allocator; not an independent memory-corruption primitive. |
| [CVE-2019-19126](https://nvd.nist.gov/vuln/detail/CVE-2019-19126) | 2019-11-19 | 3.3 NVD v3 | x86 loader honors LD_PREFER_MAP_32BIT_EXEC across a security transition, reducing ASLR. | Env: disable mapping overrides; retain address randomization. |

### Records published in 2020

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2020-10029](https://nvd.nist.gov/vuln/detail/CVE-2020-10029) | 2020-03-04 | 5.5 NVD v3 | Noncanonical 80-bit long-double input overflows math range-reduction stack storage. | Keep/fix math; relevant to x86 long-double ABI. |
| [CVE-2020-6096](https://nvd.nist.gov/vuln/detail/CVE-2020-6096) | 2020-04-01 | 8.1 NVD v3 | ARMv7 memcpy/memmove length signedness error can continue out-of-bounds copies. | Keep/fix assembler and caller lengths; ARM32, not aarch64. |
| [CVE-2020-1751](https://nvd.nist.gov/vuln/detail/CVE-2020-1751) | 2020-04-17 | 7.0 NVD v3 / 5.1 CNA/ADP | PowerPC backtrace signal-frame processing exceeds the output array. | Keep/fix unwind bounds; architecture-specific. |
| [CVE-2020-1752](https://nvd.nist.gov/vuln/detail/CVE-2020-1752) | 2020-04-30 | 7.0 NVD v3 | glob tilde expansion uses freed storage for a user-directory path. | Keep/fix glob ownership. |
| [CVE-1999-0199](https://nvd.nist.gov/vuln/detail/CVE-1999-0199) | 2020-10-06 | 9.8 NVD v3 | Historical tdelete documentation/return-value ambiguity can cause application dangling-pointer misuse; NVD backfilled in 2020. | Historical/API-contract lesson; not a new 2020 libc overflow. |
| [CVE-2020-29562](https://nvd.nist.gov/vuln/detail/CVE-2020-29562) | 2020-12-04 | 4.8 NVD v3 | UCS4 iconv irreversible-character path asserts and aborts. | Restrict converters and test invalid-input recovery. |
| [CVE-2020-29573](https://nvd.nist.gov/vuln/detail/CVE-2020-29573) | 2020-12-06 | 7.5 NVD v3 | Noncanonical x86 long-double printf conversion overflows stack; default newer builds already avoided the old path. | Keep/fix formatting; verify compiled path rather than blanket version label. |

### Records published in 2021

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2019-25013](https://nvd.nist.gov/vuln/detail/CVE-2019-25013) | 2021-01-04 | 5.9 NVD v3 | EUC-KR iconv handling of invalid multibyte input overreads. | Restrict unused encoding; fix retained converter. |
| [CVE-2021-3326](https://nvd.nist.gov/vuln/detail/CVE-2021-3326) | 2021-01-27 | 7.5 NVD v3 | ISO-2022-JP-3 iconv invalid sequence triggers an assertion. | Restrict unused encoding; test retained converter state. |
| [CVE-2021-27645](https://nvd.nist.gov/vuln/detail/CVE-2021-27645) | 2021-02-24 | 2.5 NVD v3 | nscd netgroup processing can double-free. | Remove optional nscd. |
| [CVE-2020-27618](https://nvd.nist.gov/vuln/detail/CVE-2020-27618) | 2021-02-26 | 5.5 NVD v3 | IBM stateful encodings fail to advance on malformed input, causing infinite retry. | Restrict encodings; assert conversion progress, not just output bounds. |
| [CVE-2021-33574](https://nvd.nist.gov/vuln/detail/CVE-2021-33574) | 2021-05-25 | 9.8 NVD v3 | mq_notify notification-thread attributes outlive caller-owned storage (UAF). | Keep/fix callback lifetimes; disable only if workload proves unused. |
| [CVE-2021-35942](https://nvd.nist.gov/vuln/detail/CVE-2021-35942) | 2021-07-22 | 9.1 NVD v3 | wordexp positional-parameter arithmetic can crash or read arbitrary memory. | Remove optional wordexp or retain with checked indexing. |
| [CVE-2021-38604](https://nvd.nist.gov/vuln/detail/CVE-2021-38604) | 2021-08-12 | 7.5 NVD v3 | mq_notify fix regresses removed-notification handling into a null dereference. | Keep/fix; include regressions from security backports. |
| [CVE-2021-43396](https://nvd.nist.gov/vuln/detail/CVE-2021-43396) | 2021-11-04 | 7.5 NVD v3 | ISO-2022-JP-3 reset can emit spurious NUL; upstream disputes input-only security reachability. | Restrict unused converter; validate explicit reset semantics. |

### Records published in 2022

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2022-23218](https://nvd.nist.gov/vuln/detail/CVE-2022-23218) | 2022-01-14 | 9.8 NVD v3 | Legacy svcunix_create copies an overlong path to stack. | Remove optional SunRPC compatibility path; preserve required ABI deliberately. |
| [CVE-2022-23219](https://nvd.nist.gov/vuln/detail/CVE-2022-23219) | 2022-01-14 | 9.8 NVD v3 | Legacy clnt_create copies an overlong hostname to stack. | Remove optional SunRPC compatibility path. |
| [CVE-2021-3998](https://nvd.nist.gov/vuln/detail/CVE-2021-3998) | 2022-08-24 | 7.5 NVD v3 | realpath can return unexpected/uninitialized data and leak information. | Keep/fix path output and error semantics. |
| [CVE-2021-3999](https://nvd.nist.gov/vuln/detail/CVE-2021-3999) | 2022-08-24 | 7.8 NVD v3 | getcwd with a one-byte destination can underflow/overflow. | Keep/fix tiny-buffer edge; root callers are not trusted-input guarantees. |
| [CVE-2022-39046](https://nvd.nist.gov/vuln/detail/CVE-2022-39046) | 2022-08-31 | 5.3 NVD v3 / 7.5 CNA/ADP | Large syslog message exposes uninitialized heap bytes in logs. | Keep/fix log allocation and initialization. |

### Records published in 2023

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2023-25139](https://nvd.nist.gov/vuln/detail/CVE-2023-25139) | 2023-02-03 | 9.8 NVD v3 | sprintf locale thousands-grouping plus field width can exceed expected output sizing. | Keep/fix formatter; locale reduction only narrows reachability. |
| [CVE-2023-0687](https://nvd.nist.gov/vuln/detail/CVE-2023-0687) | 2023-02-06 | 9.8 NVD v3 / 4.6 CNA/ADP | Disputed __monstartup profiling-buffer overflow with trusted application address inputs. | Remove optional profiling; preserve disagreement in severity. |
| [CVE-2015-20109](https://nvd.nist.gov/vuln/detail/CVE-2015-20109) | 2023-06-25 | 5.5 NVD v3 | Additional fnmatch malformed-pattern crash not covered by a related earlier fix. | Keep/fix pattern parser; distinct regression case despite similar symptoms. |
| [CVE-2023-4813](https://nvd.nist.gov/vuln/detail/CVE-2023-4813) | 2023-09-12 | 5.9 NVD v3 | getaddrinfo NSS SUCCESS=continue/merge configuration can cause a UAF. | Restrict NSS; keep/fix normal lookup composition. |
| [CVE-2023-4527](https://nvd.nist.gov/vuln/detail/CVE-2023-4527) | 2023-09-18 | 6.5 NVD v3 | no-AAAA mode plus a large TCP DNS answer can leak stack data or crash getaddrinfo. | Keep/fix resolver; test combinations of mode and transport. |
| [CVE-2023-4806](https://nvd.nist.gov/vuln/detail/CVE-2023-4806) | 2023-09-18 | 5.9 NVD v3 | NSS plugin callback mix and large address/canonical-name result trigger getaddrinfo UAF. | Built-in-only NSS removes this plugin case; keep corrected ownership. |
| [CVE-2023-5156](https://nvd.nist.gov/vuln/detail/CVE-2023-5156) | 2023-09-25 | 7.5 NVD v3 | Fix for CVE-2023-4806 introduces a getaddrinfo memory leak with IPv6 mapping flags. | Keep/fix resolver; test backport regressions and flags. |
| [CVE-2023-4911](https://nvd.nist.gov/vuln/detail/CVE-2023-4911) | 2023-10-03 | 7.8 NVD v3 | KEV: early-loader tunables sanitization heap overflow; local root via privileged startup. | Env: remove tunables/alias parsing before main, not just startup wiping. |

### Records published in 2024

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2023-6246](https://nvd.nist.gov/vuln/detail/CVE-2023-6246) | 2024-01-31 | 7.8 NVD v3 / 8.4 CNA/ADP | Long argv[0]/openlog ident corrupts syslog heap allocation; demonstrated su/PAM root exploit. | Keep/fix logging; env blocking does not remove argv input. |
| [CVE-2023-6779](https://nvd.nist.gov/vuln/detail/CVE-2023-6779) | 2024-01-31 | 7.5 NVD v3 / 8.2 CNA/ADP | syslog formatter failure/large-length handling can yield an undersized heap buffer. | Keep/fix signed return handling and error edges. |
| [CVE-2023-6780](https://nvd.nist.gov/vuln/detail/CVE-2023-6780) | 2024-01-31 | 5.3 NVD v3 | syslog addition of message/header lengths overflows an integer. | Keep/fix checked allocation arithmetic. |
| [CVE-2024-2961](https://nvd.nist.gov/vuln/detail/CVE-2024-2961) | 2024-04-17 | 7.3 CNA/ADP v3 | ISO-2022-CN-EXT escape output overwrites a few bytes; demonstrated PHP/Roundcube RCE. | Remove unused converter; env-only policy misses direct iconv calls. |
| [CVE-2024-33599](https://nvd.nist.gov/vuln/detail/CVE-2024-33599) | 2024-05-06 | 8.1 CNA/ADP v3 | nscd netgroup cache exhaustion causes stack overflow. | Remove optional nscd; daemon-specific. |
| [CVE-2024-33600](https://nvd.nist.gov/vuln/detail/CVE-2024-33600) | 2024-05-06 | 5.9 CNA/ADP v3 | nscd failed not-found cache insertion leads to null dereference. | Remove optional nscd; test negative-cache errors if retained. |
| [CVE-2024-33601](https://nvd.nist.gov/vuln/detail/CVE-2024-33601) | 2024-05-06 | 7.3 CNA/ADP v3 | nscd netgroup allocation failure invokes fatal allocators and terminates daemon. | Remove optional nscd; recover from service-time OOM if retained. |
| [CVE-2024-33602](https://nvd.nist.gov/vuln/detail/CVE-2024-33602) | 2024-05-06 | 7.4 CNA/ADP v3 | nscd assumes NSS callback strings are in its buffer and corrupts memory during resize. | Remove optional nscd/external NSS; enforce callback ownership. |
| [CVE-2024-12455](https://access.redhat.com/security/cve/cve-2024-12455) | 2024-12-13 | 6.3 Red Hat v3 | Fedora 41 ppc64le-specific vDSO getrandom status convention error can corrupt data or fail randomness; absent from queried NVD records. | Vendor-specific keep/fix ABI lesson; not native architecture. [Red Hat record](https://access.redhat.com/security/cve/cve-2024-12455). |

### Records published in 2025

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2025-0395](https://nvd.nist.gov/vuln/detail/CVE-2025-0395) | 2025-01-22 | 6.2 CNA/ADP v3 | Assertion-failure message allocation is short at page boundaries; attacker-controlled program name can contribute. | Keep/fix assertions; do not remove assertions to hide failures. |
| [CVE-2025-4802](https://nvd.nist.gov/vuln/detail/CVE-2025-4802) | 2025-05-16 | 7.8 CNA/ADP v3 | Static setuid dlopen, including implicit locale/NSS calls, consults attacker LD_LIBRARY_PATH. | Env: cover static startup/dlopen; static glibc is not always load-free. |
| [CVE-2025-5702](https://nvd.nist.gov/vuln/detail/CVE-2025-5702) | 2025-06-05 | 5.6 CNA/ADP v3 | Power10 strcmp clobbers nonvolatile vector registers, changing logic or leaking strings. | Keep/fix optimized ABI; outside native target architectures. |
| [CVE-2025-5745](https://nvd.nist.gov/vuln/detail/CVE-2025-5745) | 2025-06-05 | 5.6 CNA/ADP v3 | Power10 strncmp has the same register-preservation class as strcmp. | Keep/fix optimized ABI; outside native target architectures. |
| [CVE-2025-8058](https://nvd.nist.gov/vuln/detail/CVE-2025-8058) | 2025-07-23 | 5.9 CNA/ADP v4 | regcomp cleanup double-frees after allocation failure. | Keep/fix regex; fault-inject OOM cleanup. |

### Records published in 2026

| CVE | Publication | Score | Weakness / trigger | Hardening response |
| --- | --- | --- | --- | --- |
| [CVE-2026-0861](https://nvd.nist.gov/vuln/detail/CVE-2026-0861) | 2026-01-14 | 8.4 CNA/ADP v3 | Very large alignment plus near-limit size wraps memalign arithmetic into heap corruption. | Keep/fix allocator; constrain both alignment and size. |
| [CVE-2026-0915](https://nvd.nist.gov/vuln/detail/CVE-2026-0915) | 2026-01-15 | 7.5 CNA/ADP v3 | Zero network lookup via networks: dns sends uninitialized stack bytes to the DNS resolver. | Remove unused network-database DNS; hosts DNS remains. |
| [CVE-2025-15281](https://nvd.nist.gov/vuln/detail/CVE-2025-15281) | 2026-01-20 | 7.5 CNA/ADP v3 | wordexp WRDE_REUSE plus WRDE_APPEND returns uninitialized pointers; wordfree can abort. | Remove optional wordexp or fix reuse/ownership combinations. |
| [CVE-2025-0577](https://nvd.nist.gov/vuln/detail/CVE-2025-0577) | 2026-02-18 | 4.8 CNA/ADP v3 | Concurrent fork and random generation may yield predictable post-fork getrandom/arc4random results. | Keep/fix randomness/fork coordination; do not disable entropy APIs. |
| [CVE-2026-3904](https://nvd.nist.gov/vuln/detail/CVE-2026-3904) | 2026-03-11 | 6.2 CNA/ADP v3 | nscd shared-cache client compares concurrently changing memory; x86 optimized memcmp can crash. | Disable nscd client support; preserve corrected memcmp fast path. |
| [CVE-2026-4437](https://nvd.nist.gov/vuln/detail/CVE-2026-4437) | 2026-03-20 | 7.5 CNA/ADP v3 | Reverse DNS lookup treats a non-answer section as the answer. | Keep/fix response-section validation; protocol integrity. |
| [CVE-2026-4438](https://nvd.nist.gov/vuln/detail/CVE-2026-4438) | 2026-03-20 | 5.4 CNA/ADP v3 | Reverse DNS lookup returns a name that violates DNS hostname rules. | Keep/fix hostname validation; callers still validate authorization uses. |
| [CVE-2026-4046](https://nvd.nist.gov/vuln/detail/CVE-2026-4046) | 2026-03-30 | 7.5 CNA/ADP v3 | IBM1390/IBM1399 converter assertions permit application crashes. | Remove unused converters; Wolfi already targets these removals. |
| [CVE-2026-5450](https://nvd.nist.gov/vuln/detail/CVE-2026-5450) | 2026-04-20 | 9.8 CNA/ADP v3 | scanf %mc with width greater than 1024 can overflow heap by one byte. | Keep/fix scanf; field-width and allocation-edge tests. |
| [CVE-2026-5928](https://nvd.nist.gov/vuln/detail/CVE-2026-5928) | 2026-04-20 | 7.5 CNA/ADP v3 | ungetwc uses the wrong stream read pointer; non-Unicode overlaps can underread heap or null-dereference. | Keep/fix wide stdio; encoding allowlist only partly narrows exposure. |
| [CVE-2026-5435](https://nvd.nist.gov/vuln/detail/CVE-2026-5435) | 2026-04-28 | 7.3 CNA/ADP v3 | Deprecated DNS pretty-printers exceed caller output length for TSIG data. | Remove optional debug API or keep bounded implementation; not normal DNS lookup. |
| [CVE-2026-6238](https://nvd.nist.gov/vuln/detail/CVE-2026-6238) | 2026-04-28 | 6.5 CNA/ADP v3 | Deprecated DNS pretty-printers fail to bound several record RDATA reads. | Remove optional debug API or validate every record type; not resolver receive path. |
| [CVE-2026-6368](https://nvd.nist.gov/vuln/detail/CVE-2026-6368) | 2026-08-10 | 2.1 CNA/ADP v4 | wordexp WRDE_APPEND returns invalid pointer state; later freeing aborts. | Remove optional wordexp; check append lifetime distinct from reuse case. |
| [CVE-2026-6791](https://nvd.nist.gov/vuln/detail/CVE-2026-6791) | 2026-08-10 | 6.6 CNA/ADP v4 | wordexp tilde username uses input-sized strndupa; oversized input exhausts/clashes thread stack. | Remove optional expansion; replace unbounded stack allocation if retained. |
| [CVE-2026-18374](https://nvd.nist.gov/vuln/detail/CVE-2026-18374) | 2026-08-27 | 4.9 CNA/ADP v3 | Effectively empty fopen mode ccs encoding selector can cause a heap overflow. | Keep/fix fopen; restrict optional ccs conversion separately from env. |
| [CVE-2026-89092](https://nvd.nist.gov/vuln/detail/CVE-2026-89092) | 2026-09-11 | 4.2 CNA/ADP v3 | Very large hostile DNS results can exhaust an nscd thread stack and degrade lookup performance; upstream distinguishes this from total DNS outage. | Remove optional nscd; do not overstate as proven service-wide RCE. |
| [CVE-2026-19499](https://nvd.nist.gov/vuln/detail/CVE-2026-19499) | 2026-09-14 | 7.7 CNA/ADP v3 | strfmon right-justified padding overflows a caller buffer under specific sizes. | Keep/fix monetary formatter; locale restriction is partial. |
| [CVE-2026-19542](https://nvd.nist.gov/vuln/detail/CVE-2026-19542) | 2026-09-14 | 5.6 CNA/ADP v3 | Deep tdelete rebalancing pushes beyond an alloca parent stack; large-tree prerequisites. | Keep/fix checked stack growth; resource-bound tree workloads. |
| [CVE-2026-77117](https://nvd.nist.gov/vuln/detail/CVE-2026-77117) | 2026-09-15 | 5.9 CNA/ADP v3 | SHIFT_JISX0213 pending second code point is not cleared, looping with short output buffers. | Remove unused encoding; test multistep progress if retained. |
| [CVE-2026-80489](https://nvd.nist.gov/vuln/detail/CVE-2026-80489) | 2026-09-15 | 5.9 CNA/ADP v3 | EUC_JISX0213 has the parallel pending-code-point infinite loop. | Remove unused encoding; separate converter regression. |
| [CVE-2026-8674](https://nvd.nist.gov/vuln/detail/CVE-2026-8674) | 2026-09-17 | 5.3 CNA/ADP v3 | Long search-domain config or LOCALDOMAIN triggers resolver assertion; DHCP/VPN may supply config. | Env plus validated resolv.conf generation and corrected bounds. |
| [CVE-2026-86805](https://nvd.nist.gov/vuln/detail/CVE-2026-86805) | 2026-09-22 | 6.3 CNA/ADP v3 | Privileged ORIGIN path validation and later raw-path opening race; hardlink/symlink prerequisites. | Restrict ELF paths, protected hardlinks, fix check/use mismatch. |
| [CVE-2026-95818](https://nvd.nist.gov/vuln/detail/CVE-2026-95818) | 2026-09-22 | 3.6 CNA/ADP v3 | Privileged ORIGIN prefix processing overreads input and overwrites loader stack storage. | Restrict ELF paths; retain bounds fix even without env controls. |
| [CVE-2026-97399](https://nvd.nist.gov/vuln/detail/CVE-2026-97399) | 2026-09-28 | 3.7 CNA/ADP v3 | Power8 strncasecmp reads past limit at a page boundary. | Keep/fix optimized string routine; outside native architectures. |


## Comparative research: other libc and runtime implementations

macOS, Solaris, FreeBSD, and OpenBSD do not provide alternate implementations of GNU glibc. They have their own C libraries, loaders, authentication integrations, and OS security models. The following is a **targeted survey of analogous attack surfaces during the same decade**, not an exhaustive inventory of every CVE in those operating systems, and its entries are not included in the 120-record glibc count. The comparative section inventories **29 additional CVE identifiers**, plus two non-CVE hardening/robustness examples. Additional musl/uClibc examples are included because their resolver and converter APIs offer especially close comparisons.

We searched vendor advisories/errata, original disclosures, and NVD `libc`/`dyld` records, and cross-checked selected CVEs against the same complete KEV snapshot. A missing component name in an NVD description is not proof that a library has no vulnerabilities: Apple's generic record descriptions often require consulting its release bulletin for the component label. Oracle's public records can similarly obscure implementation details.

The component-level Apple bulletins below were checked directly; older native Solaris-libc disclosures often lack enough public technical detail for a defensible function-level mapping. Consequently, the GNU inventory and the comparative survey have different completeness claims.

### macOS / Darwin: libc, dyld, and application entitlements

| Case and evidence | What is established | Transferable lesson for glibc-neutered |
| --- | --- | --- |
| [CVE-2017-1000373](https://support.apple.com/en-us/103806), macOS High Sierra security content, September 2017 | Apple lists the BSD qsort resource-consumption issue under libc; the same identifier appears in OpenBSD's sorting research. This is shared-code/algorithm exposure, not a glibc assignment. | Bound stack use of sorting/fallback algorithms, retain a correct fast algorithm, and test attacker-shaped input arrays. |
| [CVE-2017-7086](https://support.apple.com/en-us/103806), same Apple bulletin | Apple fixed resource exhaustion in glob through an algorithm change. [Original Russ Cox research](https://research.swtch.com/glob) compared historical BSD/macOS and GNU implementations. | Algorithmic complexity is a security property and a performance property. Do not replace GNU code with a superficially simpler BSD implementation without adversarial benchmarks. |
| [CVE-2025-43295 and CVE-2025-43299](https://support.apple.com/en-us/125110), Tahoe 26 release, September 2025 | Apple assigns two denial-of-service corrections to libc, with validation changes. The public bulletin does not specify the affected functions or exact trigger. | Add invalid-input tests, but **do not infer** that these are getenv, printf, resolver, or allocator bugs without further source/patch evidence. |
| [CVE-2025-43353](https://support.apple.com/en-us/125110), Libinfo, September 2025 | Crafted string processing could corrupt the heap; Apple describes a bounds correction. Libinfo is a related system library, not the libc component itself. | Name-service/string processing deserves bounds and boundary tests. The bulletin alone does not establish a glibc-equivalent function or packet vector. |
| [CVE-2026-28973](https://support.apple.com/en-us/128067), libc, Tahoe 26.6, July 2026 | Apple identifies an integer overflow with potential app sandbox escape. Public details do not specify an equivalent glibc call. | Treat size arithmetic as a boundary between application input and trusted runtime code; investigate corresponding APIs rather than importing an unverified patch. |
| [CVE-2026-20700](https://support.apple.com/en-us/126348), dyld, February 2026; **KEV** | A memory-corruption correction prevents an attacker **already able to write memory** from turning that capability into code execution. Apple reports targeted exploitation on older iOS; the macOS update also includes the fix. This is not proof of observed macOS exploitation or a standalone unauthenticated remote entry point. | Loader state and relocation machinery can amplify an existing write primitive. Env removal is insufficient; maintain bounds, state and relocation integrity. |
| [CVE-2024-45599 / GHSA-x352-xv29-r74m](https://github.com/cursor/cursor/security/advisories/GHSA-x352-xv29-r74m), Cursor before 0.41.0 | Permissive dyld/library-validation entitlements allowed injected code to inherit already-granted camera/microphone access. This is an application configuration bug, not an Apple libc defect. | Library injection can cross an **application identity** boundary without changing UID. A daemon running as an ordinary user may still hold secrets or broker access worth stealing. |
| [CVE-2026-24064](https://nvd.nist.gov/vuln/detail/CVE-2026-24064), Waves Central; [original researcher advisory](https://r.sec-consult.com/waves) | A signed trusted client permitted DYLD library injection, then exposed privileged helper operations to the injected code. | Audit trusted-client/broker boundaries as well as setuid. Code running with a trusted process's identity may acquire privileges indirectly. |

The Apple cases support rejecting optional environment-driven injection universally, not only when UIDs differ. They also show why “Hardened Runtime” or a signed executable is not enough if an exception reinstates injection. This is an analogy to Werewolf's loader/module/broker trust model; Apple TCC, entitlements, pointer authentication, and Mach-O mechanisms do not exist unchanged in Linux/glibc.

### FreeBSD: converter bounds and FILE state on failure

| Case and primary evidence | Weakness | Transferable test or restriction |
| --- | --- | --- |
| [CVE-2016-6559 / SA-16:37.libc](https://www.freebsd.org/security/advisories/FreeBSD-SA-16:37.libc.asc), disclosed December 2016 | `link_ntoa` overflows a static output buffer from a crafted argument. FreeBSD reported very limited practical exposure and no known vulnerable base utilities. | Audit rarely used address/text conversion functions and static-buffer boundaries; don't infer practical RCE from the presence of an overflow alone. |
| [CVE-2017-1082](https://nvd.nist.gov/vuln/detail/CVE-2017-1082), disclosed in the 2017 sorting/Stack Clash research | Pathological qsort inputs cause excessive recursion and possible stack exhaustion. | Keep bounded-stack sorting; test both usual and adversarial distributions, including low stack limits. |
| [CVE-2019-5600 / SA-19:09.iconv](https://www.freebsd.org/security/advisories/FreeBSD-SA-19:09.iconv.asc), July 2019; **9.8 NVD v3** | Conversion writes beyond the caller's output buffer. Application use controls whether this is a crash, corruption, or executable path. | Encoding allowlist plus short-output-buffer testing; an environment policy does not stop ordinary iconv calls. |
| [CVE-2023-5941 / SA-23:15.stdio](https://www.freebsd.org/security/advisories/FreeBSD-SA-23:15.stdio.asc), November 2023 | The stdio flush path leaves FILE write-space state wrong after a failed write, permitting later heap overflow. | Fault-inject short writes, EIO, ENOSPC, interrupted writes, and custom-stream failures; verify subsequent operations remain bounded. |
| [CVE-2026-58081 and CVE-2026-58082 / SA-26:49.iconv](https://www.freebsd.org/security/advisories/FreeBSD-SA-26:49.iconv.asc), June 2026; **9.8 CNA/ADP v3 each** | HZ/UTF-7/VIQR/ZW modules miss output-length checks; an ISO-2022 intermediate stack buffer assumes a maximum smaller than some encodings actually require. | Remove unused stateful encodings and test intermediate buffers as well as public output buffers. Narrow the supported conversion graph, not just module filenames. |

A contemporary **non-CVE robustness example** is [FreeBSD bug 298747](https://lists.freebsd.org/archives/freebsd-bugs/2026-October/036646.html): `clearenv()` could crash when the environment was already empty. This is not evidence that glibc has the same fault; it is a useful regression input for a project specializing in environment handling.

### OpenBSD: fail-open sanitation, authentication parsing, and path exceptions

| Case and primary evidence | Weakness | Transferable lesson |
| --- | --- | --- |
| [CVE-2017-1000373](https://nvd.nist.gov/vuln/detail/CVE-2017-1000373); [OpenBSD 6.1 errata](https://www.openbsd.org/errata61.html) | BSD qsort's recursion can be driven by hostile input into large stack consumption. | Do not conflate the glibc 2024 comparator/fallback issue with this different sorting fault. Include both bounded recursion and error-fallback tests. |
| [CVE-2019-19521](https://www.openwall.com/lists/oss-security/2019/12/04/5); [OpenBSD 6.6 errata](https://www.openbsd.org/errata66.html); **9.8 NVD v3** | libc's BSD authentication layer mishandles a username interpreted as a helper option. Original research finds remote exposure through smtpd, ldapd and radiusd; sshd has additional defenses. | Preserve separation between data and helper options; remove unused identity mechanisms and scrutinize helper argument construction. GNU glibc does not contain OpenBSD's BSD Authentication subsystem. |
| [CVE-2019-19726](https://www.openwall.com/lists/oss-security/2019/12/11/9); [OpenBSD 6.6 erratum 013](https://www.openbsd.org/errata66.html), December 2019 | Under a small RLIMIT_DATA, the loader's attempt to split/allocate environment paths fails and it does not remove `LD_LIBRARY_PATH`. A privileged process can pass the setting into subsequent execution and load attacker code. | **Avoid allocating/parsing a value merely to reject it.** Policy enforcement must survive OOM, empty environments and duplicates. Test descendants and security transitions, not just the first executable. |
| [OpenBSD 7.8 erratum 018](https://www.openbsd.org/errata78.html), March 2026, no CVE shown | pledge's special allowances for libc paths were tightened, including removal of an old temporary-path exception and denial of traversal outside the timezone directory. This is kernel/path-policy hardening, not a libc CVE. | “Required by libc” is not a safe blanket path exemption. Validate TZ/locale/catalog access against the actual allowed subtree and policy model. |

OpenBSD also offers design comparisons: [its innovations list](https://www.openbsd.org/innovations.html) documents syscall pinning, `unveil`, and overflow-aware/zeroing allocation interfaces. These require OS and application cooperation; they are not features we can simply switch on in glibc. Preserve Linux/Werewolf's existing protections and assess their equivalents separately.

### Solaris: runtime layout, locale inputs, privilege identity, and PAM

| Case and evidence | What is established | Transferable lesson |
| --- | --- | --- |
| [CVE-2017-3629](https://nvd.nist.gov/vuln/detail/CVE-2017-3629) and [CVE-2017-3630](https://nvd.nist.gov/vuln/detail/CVE-2017-3630); [original Stack Clash advisory](https://www.openwall.com/lists/oss-security/2017/06/19/1) | Solaris loader/stack placement and resource-limit behavior participated in Stack Clash; the demonstrated chain used locale-controlled input and a writable/executable runtime segment. Oracle's CVE taxonomy names the kernel, so these should not be relabeled as pure libc CVEs. | Stack guard distance, resource limits, runtime segment permissions and input-dependent allocations matter together. Keep ELF W^X and stack probing; bound locale-related allocation. |
| [CVE-2017-3631](https://nvd.nist.gov/vuln/detail/CVE-2017-3631), same research | Solaris's pathname-based forced-privilege policy could be evaded through alternate execution paths, contributing to a stronger privilege-escalation chain. | Pin trust to the executed object and effective authority, not its displayed filename or argv[0]. This is a kernel/policy lesson, not a glibc patch. |
| [CVE-2020-14871](https://nvd.nist.gov/vuln/detail/CVE-2020-14871); [Oracle October 2020 CPU](https://www.oracle.com/security-alerts/cpuoct2020.html); [Mandiant's original investigation](https://cloud.google.com/blog/topics/threat-intelligence/critical-buffer-overflow-vulnerability-in-solaris-can-allow-remote-takeover/); **KEV** | A PAM username parser stack overflow was found in real intrusions and reachable through authentication services. Oracle gives a **10.0 v3.1** score for affected deployments, while explicitly excluding exploitation in Solaris 11.1+ and ZFSSA 8.7+ releases. **PAM, not libc**, owns the flaw. | Root authentication dependencies are part of the process's attack surface. Disabling glibc's env controls cannot repair a PAM parser; remove unused authentication paths and patch retained ones. |

The search also found older Solaris libc/loader environment flaws, including `LC_MESSAGES`, `LD_AUDIT`, and long `argv[0]` paths. Those precede the ten-year window and are not counted as recent discoveries. Public Oracle library CVEs with unspecified vectors cannot justify claiming that a particular glibc function is vulnerable. We did not identify enough public implementation detail to produce an exhaustive decade-wide native Solaris-libc inventory; the verified analogous chains above are more actionable than speculative mappings.

### Closely related Linux alternatives: musl and uClibc

| Case and evidence | Analogous weakness | Implication |
| --- | --- | --- |
| [CVE-2016-8859](https://nvd.nist.gov/vuln/detail/CVE-2016-8859), TRE/musl regex | State/tag count arithmetic can overflow into out-of-bounds writes. | Pattern engines need checked counts, allocation limits, and complexity tests even in a smaller libc. |
| [CVE-2017-15650](https://www.openwall.com/lists/oss-security/2017/10/19/5), musl DNS | Unexpected RR type and address count in a DNS reply overflow result storage. | Test type confusion between A/AAAA responses and cap accumulation of results. |
| [CVE-2019-14697](https://nvd.nist.gov/vuln/detail/CVE-2019-14697), musl x87 math | Floating-point stack imbalance causes corruption outside application source expectations. | ABI and architecture-specific optimized code need tests; this is not evidence against all vectorization. |
| [CVE-2020-28928](https://nvd.nist.gov/vuln/detail/CVE-2020-28928), musl wide-character conversion | Combined destination size/source limit mishandling writes out of bounds. | Exercise combinations of API limits, not each size independently. |
| [CVE-2025-26519](https://www.openwall.com/lists/musl/2025/02/13/1), musl iconv | Untrusted EUC-KR input controls an out-of-bounds write while converting to UTF-8. | The converter allowlist principle generalizes, but code and affected encodings differ. Switching libc does not eliminate conversion vulnerabilities. |
| [CVE-2026-40200](https://www.openwall.com/lists/musl/2026/04/10/3), musl qsort | Smoothsort's double-word operations corrupt a stack array beyond very large input thresholds. The maintainer distinguishes practical 32-bit input sizes from enormous 64-bit thresholds. | Record arithmetic/architecture prerequisites; test internal algorithm metadata and large-count boundaries. |
| [CVE-2026-6042](https://nvd.nist.gov/vuln/detail/CVE-2026-6042), musl GB18030 iconv | Reported algorithmic-complexity issue in the four-byte decoder. | Converter progress and CPU cost matter as well as writes; verify the upstream fix and exact input prerequisites before equating this with remote RCE. |
| [CVE-2022-30295](https://supportannouncement.us.dlink.com/announcement/publication.aspx?name=SAP10297); [original Nozomi research](https://www.nozominetworks.com/blog/nozomi-networks-discovers-unpatched-dns-bug-in-popular-c-standard-library-putting-iot-at-risk), uClibc/uClibc-ng | Predictable DNS transaction identifiers reduce resistance to forged replies. | Keep entropy and transaction validation correct; a small resolver and no loader tunables are not substitutes for protocol security. |

### Additional KEV results from the comparative cases

Two additional entries in this selected comparative set are present in the **2026.10.13** CISA snapshot:

- **CVE-2026-20700**, Apple dyld: added **2026-02-12**, ransomware field **Unknown**. This runtime memory-corruption entry is separate from glibc's Looney Tunables entry.
- **CVE-2020-14871**, Oracle Solaris/PAM: added **2021-11-03**, ransomware field **Unknown**. This is an adjacent authentication-library case, not native Solaris libc.

The other CVEs explicitly inventoried in this comparative section were checked by ID and are absent from this snapshot. This does not claim that these are all Apple/Solaris/BSD KEVs: operating-system catalogs contain many unrelated kernel, browser and application entries. [CISA machine-readable catalog](https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json).

**New tests justified by this comparison:** reject env overrides under allocation failure; preserve policy across descendant execution; verify stdio state after failed writes; bound sorting stack depth and internal count arithmetic; enforce both converter input progress and output/intermediate sizes; validate libc-specific path-policy exceptions; and test trusted broker clients against code-injection facilities. These add security coverage while preserving useful optimized implementations.


## Evidence snapshots and reproducibility

The primary-source feeds were retrieved on 2026-10-13. Their content can change; these hashes identify what was used rather than promising that a future request returns the same bytes.

| Input | Records/size | SHA-256 |
| --- | --- | --- |
| [CISA KEV JSON](https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json) | 1,740 entries; 1,778,944 bytes | `37dd07dbfeb4259ec10e5551ac68f2c9235b26224b2b0de68af61d518176ea8f` |
| [NVD keyword glibc](https://services.nvd.nist.gov/rest/json/cves/2.0?keywordSearch=glibc&resultsPerPage=2000) | 189 records; 1,286,176 bytes | `132f25b984764c1d9204a84bef780b53ffa8401769dd30dad0508d31b8a30f16` |
| [NVD keyword GNU C Library](https://services.nvd.nist.gov/rest/json/cves/2.0?keywordSearch=GNU%20C%20Library&resultsPerPage=2000) | 290 records; 1,486,020 bytes | `1983c6bf604264ae5de5175a575c6a829e3e4b023f3b71f57d2bd9c9ae6c022c` |
| NVD individual IDs: CVE-2019-1010022, CVE-2024-12455, CVE-2026-6791, CVE-2021-3156, CVE-2021-4034, CVE-2024-6387, CVE-2025-32463, CVE-2017-1000367, CVE-2015-0235 | 8 returned records; 138,342 bytes; CVE-2024-12455 absent | `77112878efb786c4a2ce60ea6699d81d5778f3657e39d7d4f00ebdd967557d63` |
| [Debian glibc tracker HTML](https://security-tracker.debian.org/tracker/source-package/glibc) | 36,619 bytes; 180 distinct CVE IDs | `3abb1d74b75f9ada0dcdcc7449d9ce5314532d0f1c2e692cf78c705c2a654e40` |

Additional comparative search inputs (also retrieved 2026-10-13):

| Input | Records/size | SHA-256 |
| --- | --- | --- |
| [NVD keyword libc](https://services.nvd.nist.gov/rest/json/cves/2.0?keywordSearch=libc&resultsPerPage=2000) | 677 records; 4,765,349 bytes | `e8d61687e2c822b17e36297e774d8e9f85e1340336072978b9bf573f592cbed9` |
| [NVD keyword dyld](https://services.nvd.nist.gov/rest/json/cves/2.0?keywordSearch=dyld&resultsPerPage=2000) | 30 records; 117,776 bytes | `7b4dc59859ef4f7129197aa926fcfefc0a2e11d149d1df85a27acb946ebecfe9` |
| NVD individual comparative IDs (16 records; the IDs are enumerated in the comparative tables) | 77,627 bytes | `44b30fc935063ee48d092308fdeacc4b3f5a032659fe5eec0cf1aee7f0fcc11b` |
| [FreeBSD advisory index](https://www.freebsd.org/security/advisories/) | 198,570 bytes | `b7f48d9666fc95bf53bc3e6aade556693c3517d7866e55191786123a1ef81fe8` |

The inspected Wolfi checkout was `169ac2651022405ddce73adf2383b923039e4f05`, dated 2026-10-13. Its 2.44 recipe pointed to glibc source commit `34106fee698c83eb0af71d4d0381e66b399be746`. The available bminor GitHub mirror checkout was older (`04e750e75b73957cf1c791535a3f4319534a52fc`, 2026-01-20); it was useful for implementation anchors and early advisories but **was not used as evidence that later 2026 fixes are present**. Later records were checked through current NVD/CNA and distribution data. Direct Sourceware access was unavailable for some pages.

To update: repeat both keyword queries with pagination if needed; intersect new records with source-package and CPE evidence; include individual missing IDs; review rejected/disputed assignments and architecture prerequisites; compare the entire KEV catalog by CVE ID; reread original disclosures for important chains; and re-evaluate the restrictions against actual applications. Retain the retrieval date and score source rather than silently updating historical conclusions.
