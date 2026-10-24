# Audit dispatch removal: revision 16

Validated 2026-10-20 on aarch64 Linux under QEMU/HVF, Alpine kernel 6.18.57,
Wolfi GCC 16.2.0-r1 and Melange 0.62.1. Source:
`34106fee698c83eb0af71d4d0381e66b399be746`.

The focused build applied all five policy patches with fuzz disabled, compiled
glibc, then ran the mandatory audit probe and `tests/run.py`. It used pinned
upstream source plus our patches, not the complete derived Wolfi package recipe.

| Check | Result |
| --- | --- |
| Forged audit state, every dispatch entry point | PASS; no callbacks or substitutions |
| Invalid audit pointers | PASS; no dereference |
| Same fixture with unpatched dispatch | PASS; callbacks and substitutions observed |
| Built dispatch object's loader-state references | None |
| IFUNC startup, dlopen, dlmopen and dlsym | PASS |
| Existing focused hardening probes, including static startup | PASS |
| Host repository tests | 167 passed |

The object retains undefined assembly aliases for `__memset_generic` and
`__strlen_generic` from ARM64 loader headers. It has no `_rtld_global` reference.
The control uses private state and the original dispatch source; it does not
modify the running loader. See [fixture setup](../design/test-harness.md).

The local validation APK has SHA-256:
`eb287b9701eda10ef70dd815a5cf489d0d437db76073b42e7e6bcd61c2f5664e`.
That focused build used an audit-dispatch patch with SHA-256:
`959669eba7c33a8add4052e644fc575da78693876dcb7c48812f5c3c04789002`.

This is development evidence, not release qualification. x86_64, the full
upstream suite, ABI comparison and installed-package checks remain required.
Initial discovery refused Wolfi's latest recipe because the capture hook did
not recognize its literal checkout pin. The recipe adaptation later restored
discovery with the same pinned commit. Full package testing also caught an
unsorted test declaration, corrected without changing dispatch code. No source
fallback, failure-ceiling change, publication or Werewolf source-pin update was made.

Subsequent focused validation passed the same control and policy probes on
emulated x86_64. See the [native package validation](native-r16.md) for the
complete derived recipe's results on both architectures.
