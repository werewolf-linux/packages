# Security review of the glibc policy patches

## Summary

The four patches and nscd build switches remove specific parsing and native-code activation paths. Review found security-sensitive fallbacks in the initial environment-denial patch; those have been corrected. Secure-execution sanitation is retained. This is a source review against a pinned Wolfi baseline, not a universal proof of application security or production readiness.

## Background

Reviewed base: glibc `34106fee698c83eb0af71d4d0381e66b399be746`, with Wolfi's three patches applied first. The selected design still has five mechanisms. The revised policy changes 18 source files, adding 80 and removing 71 lines. Public environment APIs and kernel `AT_SECURE` semantics remain intact.

## Goals

- Remove activation paths without removing their security checks or cleanup.
- Keep disabled environment controls from selecting a more permissive fallback.
- Record evidence and remaining assumptions separately from conclusions.

## Non-Goals

- Certify arbitrary applications, custom hardening profiles, or every possible environment value as behaviorally equivalent.
- Treat a clean patch application, successful compilation, or smaller diff as proof of security.

## Detailed design

| Mechanism | Removed surface | Preserved safeguards / review result |
| --- | --- | --- |
| Internal environment and tunables | Internal lookups; dynamic-loader environment scan; environment and cached tunable parsing | Public getters and mutation, real `AT_SECURE`, compiled allocator/CPU defaults and hardware detection remain. Corrected permissive fallbacks described below. |
| Loader injection | Preload environment/file/CLI, audit environment/CLI/ELF, CLI search overrides | Existing ELF validation, mapping/error cleanup, symbol checks, RELRO, dependencies and explicit loading remain. Requests fail before constructor or audit activation. |
| NSS | External name construction, DSO loading, symbol lookup and module initialization | Built-in files/DNS binding and pointer mangling remain. Failure state is published under the existing lock with release semantics; callers retain normal unavailable handling and errno restoration. |
| Converters | External registry/cache/path discovery and converter DSO loading | Existing one-time initialization, built-in modules/aliases, graph search, buffer bounds, cleanup and errno restoration remain. Missing external conversion follows the existing failure path to `EINVAL`. |
| nscd | Daemon, client IPC and cache protocol | Existing supported configure switches; direct files/DNS APIs remain. No substitute cache or new parser. |

**`process_envvars_secure`:** the deleted loop only recognized `LD_AUDIT` and `LD_PRELOAD` and scheduled their activation. It was not the sanitizer. The complete `UNSECURE_ENVVARS` iteration and `unsetenv` calls remain, along with the final mode/debug/version/binding checks and the returned environment count. `(getenv)(nextp)` intentionally calls the real hidden getter: the private function-like macro would otherwise return NULL and break stack/auxiliary-vector bookkeeping. This is a compile-time exemption, not an environment-controlled escape.

Compared the sanitizer tail byte-for-byte against the pinned unmodified function, allowing only that real-getter spelling and the changed eager-binding invariant (`dl_lazy != 0` instead of `!= 1`). Kernel secure-mode setup, the insecure-variable list and the startup check protecting file descriptors 0/1/2 are unchanged. Static startup retains its separate dangerous-variable removal loop. No global forcing or clearing of `__libc_enable_secure` is introduced.

**`wordexp` child sanitation:** its real `IFS` presence check is retained with `(getenv) ("IFS")`, solely to decide whether to remove that variable before shell execution. Ignoring this check would skip existing sanitation. Parsing still uses the fixed default separators; this exemption does not enable environment tuning. Native syscall tracing proves explicit application spawning can forward IFS, whereas wordexp removes it while forwarding an ordinary application variable. The probe detects the regression before the correction and covers dynamic/static callers afterward.

**Corrected fallbacks in patch 1:**

- Ignoring restricted `PATH` previously selected `CS_PATH` (`/bin:/usr/bin`). Bare-name execution now fails with `ENOENT`; explicit paths still work. This avoids bypassing Werewolf's narrow command wrappers. Applications must supply explicit executable paths.
- Ignoring `POSIXLY_CORRECT` previously restored GNU argument permutation and caret bracket-negation behavior. Fixed defaults now stop options at the first operand and use strict bracket semantics; portable `[!a]` negation remains. Explicit argument-parser modes remain application requests.
- Ignoring private `TMPDIR` previously made `tempnam` fall back to a shared directory. Its environment-selected directory search now fails if no valid explicit directory is supplied. Prefer atomic creation with an explicit private template. `tmpfile` and `mkstemp` retain their existing creation safeguards.
- Ignoring `GMON_OUT_PREFIX` previously fell back to writing `gmon.out` in the current directory. Disable the common profiling writer; retain `_mcleanup`'s sampling stop, buffer freeing and state reset. Actual stock/pre-correction Wolfi package controls demonstrate the private-prefix versus current-directory behavior; corrected dynamic/static probes require neither output.
- Ignoring `LD_BIND_NOW` previously left the loader's lazy-binding default. Dynamic and static-linked loader initialization now select eager binding, including ordinary `RTLD_LAZY` requests. The secure-mode invariant requires that hardened default; relaxing the invariant is not used as a workaround.
- Discarding a tunable policy must not leave executable-stack permission negotiable. Compile `glibc.rtld.execstack` with default 0; existing checks reject requesting main programs/dependencies before application code runs. Environment/cache requests cannot restore modes 1 or 2. [Upstream semantics](https://sourceware.org/glibc/manual/latest/html_node/Dynamic-Linking-Tunables.html).

**Loader rejection cleanup:** the DSO audit-tag gate uses the established `lose` label before the point where local failure handling stops being valid. That path closes the descriptor, unmaps segments and frees the partially constructed object before reporting an error. It does not pretend loading succeeded or leave a partially initialized handle. Mapping and dynamic-header inspection happen before this gate; it is a denial of activation, not a promise that no file mapping ever occurs.

**`ldd` inspection:** upstream avoids executing the inspected application through `LD_TRACE_LOADED_OBJECTS`. Denying that variable without adapting the tool can run application code. Patch 2 now uses the loader's explicit `--list` mode and refuses optional modes requiring disabled controls. A regression fixture first proves its constructor/main marker works, then requires listing and refusal to leave it absent. The native Linux probe passes; package-level coverage uses the shipped script and loader.

**NSS state:** external modules cannot reach a successful loading branch in this build. Built-in branches return before the denial. Multiple external requests publish the same failed state under the established lock; builtin function tables and pointer mangling are not replaced. The existing loaded-state shortcut is not a new external loading route; it cannot prevent arbitrary corruption of process memory.

The required NSS configuration package replaces only `/etc/nsswitch.conf` from `wolfi-baselayout`, selecting built-in files/DNS services. Wolfi's account defaults use `compat`, which the patch denies. Ordinary account probes use the installed configuration without forcing a test provider.

**Converters:** `__gconv_load_cache` has no remaining configuration-init caller in the inspected runtime call graph and is hidden. Its cache pointer remains NULL. The built-in graph is initialized through the original once/lock path. Returning NULL from `__gconv_find_shlib` reaches existing error/cleanup handling; no fabricated function pointer or successful conversion result is introduced.

## Drawbacks

Stricter defaults change application behavior: bare command names, options after operands, nonportable bracket patterns, and environment-selected temporary directories need application work. Eager binding may change startup costs or expose unresolved plugin symbols earlier. These changes need workload and performance validation; do not restore a globally weaker fallback for compatibility.

## Alternatives Considered

- Deny every getter and assume NULL is safe: rejected because NULL can select a broader or weaker default.
- Delete the complete secure-environment function: rejected because sanitation and stack bookkeeping protect applications and descendants.
- Replace public getters or force secure mode: rejected because applications must retain their own environment access and real privilege semantics.

## Security Considerations

The reviewed activation gates reduce libc attack surface within the fixed Werewolf profile. No unconditional claim covers every application: a caller can depend on parsing, identity-source, locale or path semantics for its own policy. Installed NSS configuration must use supported files/DNS services; existing `UNAVAIL=continue` fallback rules are not automatically an authentication denial. Trusted system resolver/timezone/locale files remain inputs.

Removing the new system-wide tunable cache also removes custom administrators' locked hardening settings. The inspected Wolfi support files contain library search paths, not a custom tunable hardening profile. Before adopting an image with such a profile, translate required protections into fixed build defaults; this review does not claim that arbitrary stricter deployments remain equivalent. Hardware-dependent CET/GCS enforcement is not silently enabled or advertised as mandatory.

Normal dependencies, explicit loading and trusted-code reuse remain. IPE must reject untrusted executable mappings through all retained routes. C library changes cannot make arbitrary memory corruption or malicious interpreted input safe. See the main design's loader attack model and production IPE gates.

## Reliability Considerations

Regression probes check real privileged dynamic/static execution, public getters versus secure getters, dangerous-variable removal, C locale and post-startup mutation, implicit versus explicit execution, strict parsing/pattern defaults, temporary-directory failure, eager resolution of an unresolved lazy DSO, and dynamic/static executable-stack refusal. Existing injection, builtin NSS/conversion, thread and unwinding probes remain.

Focused probes pass on native aarch64 and emulated x86_64, including real privileged dynamic/static execution on aarch64. The native Wolfi/GCC 16 build also passes its compiler check and focused probes through the ldd/NSS corrections. The final IFS/profiling corrections also pass native Wolfi/GCC 16 focused checks in r13, along with GCC 13 dynamic/static syscall tracing. Strict patch application and recipe/provenance checks pass.

Native fixture corrections restored 568 tests: candidate r12 reports 6,236 PASS and 349 FAIL, versus 6,487 PASS and 54 FAIL in the matched stock build. The then-unchanged 2% installed-package gate correctly rejected its 5.21% ratio. Final r13 records 6,240 PASS / 345 FAIL in the suite summary and 5.15% across installed result files; the current requested downstream ceiling is 5.5%. No failures have been reclassified or waived. The final IFS/profiling build passed native focused hardening checks and the unchanged ABI gate (99.96% binary / 99.98% source before inherited rounding); see the [r13 inventory](../audit/native-r13.md). Native x86_64 and remaining installed-package checks still govern publication; individual failure review, form/IPE tests and service performance checks govern production adoption.

The ABI checker reports 99.96% binary and 99.98% source compatibility for r12; the inherited HTML gate displays a rounded 100.0%. Its warnings concern missing stpcpy parameter metadata and a hidden setlocale alias return type. Source signatures remain unchanged and focused public calls validate copying and returned locale pointers. Retain the report; the rounding is not proof of exact ABI identity.
