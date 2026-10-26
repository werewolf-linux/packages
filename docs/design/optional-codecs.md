# Optional Node compression libraries

## Summary

Build the hardened Node runtime without Brotli and Zstd while retaining zlib,
gzip and deflate. Missing codecs must also be absent from HTTP negotiation.

## Background

Node's `--shared-brotli` chooses a shared library; removing it bundles the same
decoder. Upstream currently has no build option to omit either codec. Node's
native zlib binding exposes the decoders to both application code and fetch.

## Goals

- No Brotli or Zstd implementation or linkage in the shipped Node executable.
- No public or internal codec constructors when disabled.
- Fetch advertises only available encodings; unsupported responses fail.
- Gzip/deflate, HTTPS, workers and existing hardening tests still pass.

## Non-Goals

Removing SQLite, ICU, TLS, or TypeScript support without a consumer audit.
Removing Zstd from Werewolf's boot tooling. Replacing native addon dependencies.

## Detailed design

Add independent `--without-brotli` and `--without-zstd` configure options. They
exclude bundled and shared linkage, native contexts, registration and version
metadata. Header-only numeric constants remain so zlib's JS initialization and
constant consumers remain compatible; headers do not bring decoder code.

The JS zlib module omits unavailable codec exports. Web CompressionStream only
accepts Brotli when its binding exists. Bundled Undici chooses Accept-Encoding
from the available constructors, including its checked-in generated bundle.
Explicitly requested unsupported encoded responses must reject without parsing.

The package recipe enables both omissions and drops the shared Brotli flag.
Keep build headers until they are no longer needed; runtime linkage is the
acceptance criterion. Native addons must be rebuilt against matching headers
and audited separately for their own decoder dependencies.

## Drawbacks

Applications requiring Brotli/Zstd APIs need a different runtime build. This adds
a maintained Node source patch; upstream changes require source and build review.

## Alternatives Considered

Runtime gates leave vulnerable native code linked and require every entry point
to remain guarded. Updating the library is still necessary wherever it remains,
but does not reduce the unused decoding surface. Removing the shared flag alone
does not remove the implementation.

## Security Considerations

Check both public APIs and internal bindings, shared-library dependencies and
bundled symbols. Do not advertise codecs merely because numeric constants exist.
Applications requesting unsupported encodings get a failed operation.

## Reliability Considerations

Exercise local HTTP gzip/deflate negotiation, forced unsupported responses,
stream APIs and existing Node policy tests. Build and test native addons before
switching application forms to the new runtime revision.
