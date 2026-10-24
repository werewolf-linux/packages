# Native glibc revision 16 validation

Package: `glibc-neutered-2.44`, version `2.44-r24`.
Source: `34106fee698c83eb0af71d4d0381e66b399be746`.
Wolfi snapshot: `497d6a9acc1ceed040ae5addfe2ad644b1a4765d`.
Captured input build identity:
`d88e5775fac785dcf2a41ae8fa723325341714a822fb6c407387f5906c5cf556`.
Audit-dispatch patch SHA-256:
`70c8077e447bc9fa55b40be6894b508d6e083df3c04c315a26259ddd27500f4d`.

| Host | Architecture | Kernel |
| --- | --- | --- |
| galadriel | x86_64 | 6.18.57-2-lts |
| uruk-hai | aarch64 | 7.1.13-200.fc44.aarch64 |

Both builds use Melange 0.62.1, Wolfi's captured recipe and compiler environment,
user-namespace root and bubblewrap. Signing uses temporary local keys.
CPU affinity is limited to eight CPUs on x86_64 and 32 on ARM64. The x86_64
process scope also enforces an 8 GiB memory limit with no swap.

| Gate | x86_64 | aarch64 |
| --- | --- | --- |
| Capture, derivation and patch preparation | PASS | PASS |
| Mandatory audit and IFUNC probes | PASS | PASS |
| Existing focused hardening probes | PASS | PASS |
| Upstream build-time gate, including Makefile lint | PASS | PASS |
| Upstream runtime result ceiling | PASS (5.05%) | PASS (5.07%) |
| ABI compatibility gate | PASS (99.9%) | PASS (99.96%) |
| Installed-package tests | PASS | PASS |

Both native build commands exited zero, including all installed-package checks.

The x86_64 build records 6,714 PASS, 357 FAIL, 16 XFAIL and 438 UNSUPPORTED
results. ARM64 records 6,362 PASS, 340 FAIL, 16 XFAIL, 505 UNSUPPORTED and two
XPASS results. These are raw result files, including focused probes;
the existing 5.5% failure ceiling is unchanged. This is a passing package gate,
not a claim that every upstream test passed.

The x86_64 ABI report has six medium binary findings and no removed symbols.
Direct comparison with Wolfi's stock `glibc-2.44-2.44-r8.apk` shows identical
exported versions, types and sizes for every flagged symbol: `_sys_siglist`,
`sys_sigabbrev`, `_sys_errlist` and `stpcpy`. The report pairs different legacy
symbol versions and models `stpcpy`'s IFUNC resolver as its public signature.
The public `stpcpy` argument and return-value probe also passes.

ARM64's two binary findings concern `stpcpy` parameters. Its exported version,
type and size match stock Wolfi, and the public argument and return-value probe
passes. No symbols were removed. The source report also flags the internal
`__GI_setlocale` alias; the public `setlocale` probes pass.

The full host suite has 172 passing tests. The recipe adaptation accepts Wolfi's
literal checkout pin and reviewed GitLab mirror, preserving the exact source pin
in package metadata. Tests reject unknown mirrors, changed branches, additional
checkout options and unresolved pins. The previous variable-based pin remains
supported for captured historical recipes.

The first ARM64 package build stopped on upstream Makefile lint: the new audit
test declaration was out of order. Sorting it fixed that gate. No dispatch code,
test suppression or failure ceiling changed in that correction. The existing
raw runtime failure ceiling remains 5.5%; expected policy failures stay recorded.

The earlier focused builds also passed the unpatched audit positive control on
aarch64 and emulated x86_64. See [focused evidence](audit-dispatch-r16.md).
The control uses private state and never edits the running loader.

These are development builds signed with temporary local keys. No package was
published and no Werewolf package-source pin was advanced.

Runtime APK SHA-256 hashes (`2.44-r24`):

- x86_64 `glibc-neutered-2.44`:
  `4cf059497717e07b4d0884f2779d9f6ea1a2268e7c31ce46064a3dd25f35646f`
- x86_64 `ld-linux-neutered-2.44`:
  `4a09c96404e60063ad64d1609fbe4725938b72f8b39540d6b16cf2433f3859da`
- aarch64 `glibc-neutered-2.44`:
  `1a2fa172cf62c7e352b6e7bdfca5a0c28a91b7416c0d707ef8b3b1e4f420a66a`
- aarch64 `ld-linux-neutered-2.44`:
  `56bb17e76a92606b0624509654add2efec00a63632d6cc59292971164428c8f2`
