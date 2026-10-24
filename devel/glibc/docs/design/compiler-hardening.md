# Compiler hardening beyond Wolfi

## Summary

Preserve current Wolfi hardening as the release floor. Since revision 11, add what costs no CPU time: strict flexible-array bounds, code kept apart from data on both architectures and a 6.18 minimum kernel. Keep additions that cost run time out until a measured workload justifies them.

## Background

Chainguard's [2025 investigation](https://www.chainguard.dev/unchained/this-shit-is-hard-hardening-glibc) describes unwinding and early IFUNC failures exposed by hardening. Current Wolfi specs are newer than that article: GCC 16 uses `-fhardened` and `-fzero-init-padding-bits=all`, with architecture-specific protection. The [OpenSSF guide](https://best.openssf.org/Compiler-Hardening-Guides/Compiler-Options-Hardening-Guide-for-C-and-C++.html), dated 2026-08-20 when inspected, also recommends strict flexible arrays and clearing used GPRs. The r18 binaries (2026-10-15) show the wrapper's flags reach glibc: x86 libc sets up frame pointers, which only the spec adds, and carries stack-clash probes; both architectures carry their control-flow markings (BTI, PAC, GCS; IBT, SHSTK). glibc builds only with GCC: its clang support (2.43) is experimental, and clang's extra protections (CFI, ShadowCallStack, SafeStack) need every library and program built for them.

## Goals

- Keep Wolfi's compiler wrapper, configure flags, upstream corrections, ABI and unwinding tests.
- Check compiler invocations and resulting ELF properties, rather than infer protection from a flag name.
- Add an extra only when it costs no run time, after native x86_64/aarch64 correctness validation; one that costs run time also needs performance validation.

## Non-Goals

- Replace glibc's per-file bootstrap/fortification rules with a blanket flag list.
- Add runtime controls or disable optimized CPU dispatch, tcache, or arenas.
- Claim compiler hardening prevents arbitrary code execution.

## Detailed design

| Mechanism | Current Wolfi floor | Additional evaluation |
| --- | --- | --- |
| Bounds checks | Highest supported glibc fortification, currently level 3 | Adopted: `-fstrict-flex-arrays=3` (`configparms` `CFLAGS`) bounds trailing arrays by their declared size. Compare test failures with the previous release, not just the ceiling. |
| Initialization | Automatic-variable zeroing and GCC 16 padding initialization | Already present; preserve rather than advertise as an addition. |
| Stack protection | Strong canaries, clash probes, non-executable ELF stack | Rejected for now: `--enable-stack-protector=all` adds work to small frequent calls. |
| Return state | x86 CET; ARM PAC/BTI; normal register lifetime | Rejected for now: `-fzero-call-used-regs=used-gpr` adds instructions to every return and misses handwritten assembly. |
| Branch targets | BTI applied to marked objects only | Deferred: enforcing would refuse unmarked Wolfi programs and libraries that 7 of 20 forms ship (2026-10-15: `node`, `php`, `gitea`, `ollama`, gmp, x264/x265, libruby). The CI arm runner does not expose BTI. |
| Code layout | x86 links with `-z separate-code`; arm64 maps headers, symbols and read-only data executable (477 KiB of libc) | Adopted: `-z separate-code` for every shared object and the loader on both architectures. |
| Minimum kernel | `--enable-kernel=4.9` | Adopted: 6.18, the oldest kernel Werewolf boots; fallbacks for older kernels go. It may only rise (`scripts/verify.py`). |
| Relocations | RELRO and eager binding | Verify actual ELF properties; no extra relaxation for startup speed. |
| Control-flow redundancy | Hardware-aware protections | GCC software redundancy flags add checks; defer unless a measured workload justifies their cost and coverage. |
| Shadow stacks | Upstream CET/GCS behavior | x86: glibc enables SHSTK when every startup object is marked, then refuses to `dlopen` an unmarked one; it needs `CONFIG_X86_USER_SHADOW_STACK`, which Alpine's kernel lacks and Werewolf's leaves off for that reason. Wolfi's `libjvm.so` is unmarked while `java` is marked, so Java would fail on CET hosts; so do extensions linking unmarked gmp, aom, x265 or NSS. arm64 GCS stays off: Wolfi's libcrypto and cyrus-sasl lack GCS marking, so optional mode would refuse them. |
| Allocator | glibc 2.44 has no fastbins | Nothing to add: `M_MXFAST` is a no-op. |

`scripts/verify.py` checks the derived recipe retains every upstream configure option, except a raised minimum kernel, and the exact compiler environment. Package builds use `openssf-compiler-options`; glibc still controls fortification and bootstrap exclusions. Record compiler versions, resolved specs, configuration and ELF results with releases. Plain Ubuntu/GCC builds validate patch behavior but do not establish equivalence to Wolfi's complete toolchain.

GCC documents [strict flexible arrays](https://gcc.gnu.org/onlinedocs/gcc/C-Dialect-Options.html), [register clearing](https://gcc.gnu.org/onlinedocs/gcc/Optimize-Options.html), and [hardening/stack protection](https://gcc.gnu.org/onlinedocs/gcc/Instrumentation-Options.html). Candidate flags affect generated code, including optimizer assumptions; compile success alone is insufficient. Preserve glibc's existing file-specific exemptions.

The guide's `-z nodlopen` marks a particular shared object unavailable for new explicit loads; it is not a global ban on `dlopen` and does not replace loader/NSS/converter restrictions or IPE. Do not blanket-apply it to libc or plugins without namespace and loader compatibility tests. PIE belongs on executables, PIC on shared libraries. C++ library assertion flags do not independently harden glibc's C implementation.

## Drawbacks

Extra instructions can increase code size and call overhead. New assumptions can expose old source idioms; fixing them downstream could expand the patch set. Hardware enforcement can reject otherwise usable packaged software. No overhead percentage is assumed.

## Alternatives Considered

- Copy the OpenSSF command line verbatim: its application guidance needs adaptation for libc, early loader code, shared libraries, and architecture-specific assembly.
- Add every available GCC check: more checks alone do not establish useful protection or acceptable performance.
- Only inspect build flags: a later override or a mixed dependency can defeat the intended protection; inspect outputs and run failure-path tests too.

## Security Considerations

Retain upstream early-loader/IFUNC and unwinding fixes. Test backtrace, C++ exceptions, cancellation, signals and static startup with the hardened library. The five feature restrictions remain fixed regardless of any compiler experiment.

## Reliability Considerations

Compare the same source, toolchain and workload with one added mechanism at a time. Gate on upstream and injection-test results, ABI stability, native supported-CPU tests, and no repeatable regression over 5% in required workloads. Release an addition only after its native builds pass with no test failing that passed in the previous release (`scripts/compare-results.py`); record a rejected candidate's reason.
