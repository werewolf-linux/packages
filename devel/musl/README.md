# musl for Werewolf

An opt-in musl 1.2.6 shared runtime, installed at
`/usr/lib/werewolf-musl/libc.so`. It does not replace the system libc or provide
the stock musl APK name. Forms select it for compatible musl applications.
It retains the normal musl ABI and libc features.

When every configured IPv4 DNS server is in 127.0.0.0/8, the resolver binds its
UDP client socket to 127.0.0.1. Nonlocal and mixed resolver lists retain the
upstream binding behavior. The DNS protocol, name lookup results and policy
remain the resolver’s responsibility. IPv6 transport behavior is unchanged.

The recipe also applies the upstream fix for CVE-2026-107891:
https://www.openwall.com/lists/oss-security/2026/10/21/18
The captured patch is the maintainer’s published attachment. Both patches are
applied strictly during the build; upstream changes require review.

Build with `make PACKAGE=musl build` in this repository. The build compiles a
probe against the new runtime and runs it in its own network namespace. The
probe inspects `/proc/net/udp` while DNS requests are pending, checks successful
resolution and NXDOMAIN, and verifies nonlocal/mixed lists keep their original
binding. No test program, chroot, compiler, headers or static archive is shipped.
The probe needs the build runner’s namespace and chroot capabilities.

Plex compatibility is exercised by Werewolf’s `forms/plex` VM tests. Its form
explicitly replaces the bundled loader links and confines its separate server
and transcoder processes. This package does not rebuild proprietary Plex code.

Automatic updates select release tags from the upstream GitHub mirror and
refresh the official release archive checksum. Existing security/policy patches
must still apply and the probes must pass before an update is accepted.
