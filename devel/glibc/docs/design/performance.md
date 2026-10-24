# Native glibc performance comparison

## Summary

The first comparison found no median slowdown in six microbenchmarks. These measurements cover candidate 2.44-r12, before the final IFS/profiling corrections. Required services, external DNS, allocator RSS and production hardware remain unvalidated.

## Background

Both runtime APK pairs were built natively from glibc `34106fee698c83eb0af71d4d0381e66b399be746` with Wolfi GCC 16. Benchmark callers use the same GCC 13 compiler and source, with each native loader/libc selected explicitly. Execution uses the aarch64 Ubuntu Lima VM: kernel `6.8.0-142-generic`. Nine rounds alternate order after warm-up. APK hashes, ELF evidence and raw samples are retained in `build/analysis/benchmark-r12.json`.

## Goals

- Identify a repeatable slowdown before changing production forms.
- Preserve optimized allocation, threading and memory operations.

## Non-Goals

- Establish service performance, DNS caching behavior, memory consumption or a speedup.

## Detailed design

| Case | Stock elapsed (ms) | Candidate elapsed (ms) | Median change |
| --- | ---: | ---: | ---: |
| startup | 0.179 | 0.173 | -3.32% |
| allocate | 29.495 | 29.360 | -0.46% |
| threads | 40.167 | 39.726 | -1.10% |
| memory | 110.097 | 110.068 | -0.03% |
| identity | 17.043 | 16.722 | -1.88% |
| hosts-file | 26.328 | 25.879 | -1.70% |

Startup reports elapsed time per process, including launcher overhead; other rows report complete loop elapsed time. An identical-package control varied by 6.64% for startup and up to 4.22% for threaded allocation while builds were active. Repeat any apparent regression separately before attributing it to libc.

## Drawbacks

Short VM microbenchmarks have scheduler and launcher noise. Files-based host lookup does not measure network DNS or nscd cache removal under service traffic.

## Alternatives Considered

- Timing concurrent stock/candidate workloads: rejected because resource contention obscures the difference.
- Infer performance from the source diff: insufficient evidence for startup, lookup and allocator behavior.

## Security Considerations

Each pair uses matching native loader/libc APKs. The runner verifies source commits and architectures and records input hashes. No disabled loader or environment feature is restored for performance.

## Reliability Considerations

Repeat the final candidate on both native architectures and required workloads. The proposed service gate remains no correctness failures or repeatable regression over 5%. The current raw failure ceiling is the requested downstream 5.5%; native package publication and production adoption remain separate gates.
