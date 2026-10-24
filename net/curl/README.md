# curl

Wolfi curl and libcurl with an HTTP(S)-only build policy. The package names and
libcurl ABI remain unchanged. `curl-hardened` and `libcurl-hardened` identify this
build; Werewolf's hardened feed coverage prevents fallback to stock packages.

Retained: HTTP/1.1, OpenSSL certificate and hostname verification, IPv4/IPv6,
threaded system DNS, gzip/deflate, cookies with public-suffix checks, basic/bearer
authentication, redirects, uploads, ranges, conditional requests and ETags.
The shared library still supports PHP's HTTP requests and Git over HTTPS.

Omitted: Brotli, Zstd, HTTP/2 and HTTP/3, SSH, LDAP, FTP, mail and other non-HTTP
protocols, proxy support, Kerberos/GSSAPI, NTLM, digest/AWS authentication,
libidn2 URL conversion, DoH, Unix sockets, netrc, alt-svc, HSTS, manual generation,
shell completions, the wcurl shell wrapper, static library and OCI entrypoint. Applications requiring
these features need a separately reviewed build. An application can still
explicitly disable TLS verification through libcurl; this is dependency reduction,
not a new authorization boundary.

libpsl remains because accepting public-suffix cookies is unsafe. Its own Unicode
dependencies remain. OpenSSL and zlib remain for HTTPS and gzip/deflate.

`tests/probe.py` exercises local HTTP/HTTPS, rejected encodings/protocols, uploads,
authentication, cookies, ETags and certificate failures in the APK test guest.
Wolfi's Git HTTPS authentication, linkage, CLI and development-package tests remain.
The upstream build suite excludes the `netrc` keyword because loading netrc
credentials is disabled; the package probe verifies those credentials are ignored.
Wolfi's existing numbered exception and test failure reporting remain.
Derivation refuses changed upstream configure options, checkout or subpackage layout.

This is a Wolfi/glibc package. It cannot replace curl inside Pi-hole's Alpine/musl
OCI filesystem; that image needs its own compatible rebuild.
