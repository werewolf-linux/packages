# Pi-hole curl

## Summary

An HTTP(S)-only curl for Pi-hole's Alpine/musl OCI filesystem. It installs at
`/oci/pi-hole/usr/lib/werewolf-pihole/curl`; the form prepends that directory to
the service's PATH and prunes the image's original curl.

## Detailed design

Build from the same pinned curl source as the hardened Wolfi package. Link
libcurl into the executable while keeping musl, OpenSSL 3 and zlib shared with
the image. This avoids shipping another TLS library or a glibc loader. Package
dependency/provide inference is disabled because these are OCI-local libraries,
not dependencies or providers for the host's Wolfi package solver.

Keep gzip/deflate, TLS validation, redirects, ETags, conditional requests, custom
DNS resolution and IPv4/IPv6. Omit Brotli, Zstd, HTTP/2/3, c-ares, IDN conversion,
proxy support, non-HTTP protocols, cookies/PSL, MIME and shell wrappers. Gravity
does not use cookies or MIME; local-file lists are handled by its own script.

## Security Considerations

`check-linkage.py` requires exactly the expected musl/OpenSSL/zlib dependencies.
`probe.py` checks HTTPS trust, redirects, uploads, ETags and rejected encodings.
The form's blocklist test verifies the installed binary inside the actual OCI
tree. Changing Pi-hole's libc or OpenSSL ABI requires rebuilding and retesting.

## Reliability Considerations

Keep the upstream OCI image for FTL, web assets and gravity. Removing original
libcurl also requires pruning Git's HTTP helpers. Keep DNS diagnostic tools and
their HTTP/2 libraries because gravity uses them on failure paths. The package updater follows curl's stable underscore tags, refreshing the exact
source commit and testing it before publication.
