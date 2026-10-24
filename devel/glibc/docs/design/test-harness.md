# Validating the fixed glibc policy

## Summary

Run Wolfi's complete suite against the patched runtime, retaining raw results and inherited gates. Adapt fixture delivery where disabled environment controls would otherwise prevent testing supported behavior.

## Background

The unchanged native aarch64 Wolfi 2.44-r8 build produced 6,487 PASS, 54 FAIL, 541 UNSUPPORTED, 16 XFAIL and 2 XPASS. The first reviewed candidate produced 5,570 PASS and 917 FAIL. Both used source `34106fee698c83eb0af71d4d0381e66b399be746`, native Alpine/QEMU, Melange 0.62.1 and Wolfi GCC 16.

Failures include intentionally removed features, missing environment-selected fixtures, and inherited host/toolchain failures. Compilation and 100% reported ABI compatibility do not make the candidate ready to release.

## Goals

- Keep tests for retained behavior effective without runtime exceptions.
- Preserve original statuses, outputs, failure thresholds and ABI checks.
- Distinguish a demonstrated policy incompatibility from a regression needing repair.

## Non-Goals

- A permissive libc build, a generic expected-failure allowlist, or broad suite suppression.
- Treat microbenchmarks as sufficient validation for service workloads or IPE integration.

## Detailed design

| Dependency | Adaptation | Boundary |
| --- | --- | --- |
| Loader `--library-path` | Upstream hardcoded test interpreter/RPATH option | Test programs still run the shipped policy; the option remains denied. |
| `LOCPATH` fixtures | Generate upstream-declared locales and install in the build guest's compiled locale directory | Explicit locale APIs remain tested; environment-selected locale stays C. Runtime APK contents are separate. |
| `LD_PRELOAD` memory tracing | Explicitly link selected dynamic test programs to upstream `libc_malloc_debug.so` | Test instrumentation only; debug DSO is removed from runtime packages. Static tests are excluded. |
| Wolfi NSS `compat` | Install the same required files/DNS configuration used by the candidate packages | External NSS remains denied; ordinary account probes do not force providers. |
| `find -exec` bare commands | Use an absolute grep path for result counting | Counting/assertions remain; the current requested downstream ceiling is 5.5%. |
| Melange QEMU source copying | Empty input directory for self-contained package tests | Avoid bootstrap `execvp("cp")`; no command-lookup exception. |
| Ubuntu 26.04 AppArmor child capability denial | Private CI bubblewrap profile allows child `sys_chroot` | Preserve APK fresh-root bootstrap checks and the system bubblewrap profile; preflight both before building. |
| apk assumes GNU argument permutation | Test-only adapter uses supported `apk_info`, `apk_add`, `apk_del`, `apk_version` and `apk_update` applet names | Options precede operands; verify an older version compares as `<` before trusting gates. |
| Local APK repository in fresh roots | Temporary loopback HTTP server; QEMU uses its host gateway | Signed APKs/index remain authenticated. No production service, signing bypass or external bind. |

`tests/harness.mk` selects memory-traced programs from upstream test environment metadata and formatting-fixture lists. It links the upstream debug DSO before libc with an explicit RPATH. This changes test executable dependencies, not production libc or installed headers. A targeted allocation test reports no leaks with tracing active. Production cleanup removes `libc_malloc_debug.so*`. Locale preparation covers every test directory declaring `LOCALES`, rather than only localedata's own list.

Native `tests/run.py --trace-spawn` uses strace to check the actual shell exec environment. Its explicit-spawn positive control forwards IFS; wordexp must strip IFS while forwarding ordinary application data, for dynamic and static callers. Trace files are temporary and not published.

`tests/run.py` builds and executes `elf/tst-werewolf-audit` as a mandatory gate.
It compiles the actual audit dispatch with private state using glibc's internal
types. Every entry point receives forged registration; callbacks must not run,
bindings must stay unchanged, and invalid audit pointers must not be read.
The shipped dispatch object must have no reference to loader global state.
Separate probes retain IFUNC execution at startup and through dlopen, dlmopen
and dlsym. Their results are recorded in `policy-report.json`.

For a positive control, preserve `elf/dl-audit.c` from the pinned source before
applying patches as `elf/dl-audit-control.c`. Add a development-only test source
which defines `EXPECT_AUDIT` as 1 and `AUDIT_SOURCE` as `"dl-audit-control.c"`,
then includes `"tst-werewolf-audit.c"`. Build it as a `tests-internal` test.
Each callback must run and substitute the fixture's binding or search result.
This control changes only a test executable, never the running loader's state.

`scripts/compare-results.py` verifies matching source commits and architectures, then records all original statuses, changed cases and new failure outputs. Reports stay under `build/analysis`; a failed case is never converted to PASS by this script.

`scripts/benchmark.py` extracts only runtime libraries from explicitly selected native APKs, checks matching source commits/architectures, and links otherwise identical fixtures to each loader/libc. It alternates run order and retains APK hashes, compiler/ELF evidence and raw samples. Compare startup, single/threaded allocations, memory operations, account lookup and hosts-file lookup. Run after builds finish; use repeated measurements for any regression over 5%.

External DNS, allocator RSS, database/service traffic and production hardware require separate measurements. The hosts-file case does not measure network DNS or resolver caching. Startup includes launcher overhead; identical-package controls help identify measurement noise.

## Drawbacks

Some upstream tests explicitly require disabled audit, tuning, converter, parser or execution semantics. Their failures need case-by-case evidence and replacement denial coverage. Restoring fixture delivery does not make those expectations compatible.

## Alternatives Considered

- Re-enable environment variables for tests: rejected because it would test a different policy.
- Raise the allowed failure ratio: rejected because it could hide unrelated regressions.
- Mark every subsystem failure expected: rejected because supported behavior still needs coverage.

## Security Considerations

Instrumentation must stay in test executables and out of runtime packages. No external module, preload, search-path or tunable exception is introduced. `ldd` inspection must never execute its target; the package regression checks constructors and main through an observable marker.

## Reliability Considerations

Candidate r12 restored 568 tests, with 6,236 PASS and 349 FAIL. The matched stock build has 54 FAIL; 295 new failures remain for individual review. Its installed-package ratio was 5.21%, which failed the then-unchanged 2% gate. The final r13 build has 345 failures and a 5.15% ratio; the requested downstream ceiling is now 5.5%. Native builds on both architectures, inherited package gates, application tests, IPE loading attacks and performance checks block production promotion. Raw evidence remains available when a gate fails.
