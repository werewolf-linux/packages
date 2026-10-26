# Plex Media Server

Plex's unmodified vendor binaries, with a codec bundle downloaded and verified at
build time. This belongs in the application feed: kernel confinement and privilege
separation are implemented by `../../../werewolf/forms/plex`, rather than patches to Plex.
Plex includes its own musl loader, libraries and a legacy Python runtime; installing
hardened glibc or Python does not replace those bundled components.

`plex.yaml` pins the current public release and every codec by SHA-256 for x86_64
and aarch64. `InRelease`, the two package indexes and `PlexSign.v2.key` retain the
vendor's signed provenance. `sources.json` records the codec manifest results.
`go run ./cmd/package plex-verify` (from the repository root) checks the repository signature, signing-key fingerprint,
package-index hashes and all recipe pins without network access.

Build with `make PACKAGE=plex build` from this repository. The recipe installs
only the application under `/usr/lib/plexmediaserver`, its license material and
the codec dependencies; no vendor maintainer scripts, service manager or updater
runs. `/usr/lib/plexmediaserver/isolated/Plex Transcoder` is a second copy of the
unchanged transcoder, with its original relative library lookup preserved. A
supervisor can interpose on the ordinary helper path without modifying that ELF.

Codec downloads are independent vendor artifacts. For an update, verify the new
signed APT metadata and both DEB hashes, derive the codec set from the new
transcoder's version, capture codec manifests over vendor HTTPS, and update every
URI and SHA-256 together. Run `package plex-verify`, build both architectures, and run the
form's real media and confinement tests. Never warm a writable codec cache in a
running VM or substitute a newly downloaded module when a pinned codec is missing.

Plex remains proprietary; its bundled license files are retained. This recipe
does not claim to rebuild, inspect or patch the proprietary server's source.

`package.yaml` enables automatic tracking of the signed public APT repository.
The updater verifies both architectures in a scratch context; an unrecognized
codec ABI fails preparation and requires a reviewed codec manifest refresh.
