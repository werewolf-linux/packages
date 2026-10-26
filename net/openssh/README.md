# OpenSSH

Wolfi's OpenSSH recipe, `openssh.yaml`, built without PAM, SELinux, zlib or
PKCS#11 support; currently 10.6p1. It keeps Wolfi's package names (`openssh-server`,
`openssh-client`, `openssh-keygen` and the rest) and outranks Wolfi's build, so
every Werewolf sshd form gets it. Design: Werewolf's
[SSH hardening](https://github.com/werewolf-linux/werewolf/blob/main/docs/design/ssh-hardening.md).

## Use

Werewolf: nothing to add. Every image lists the hardened feed. Wolfi, after
[feed setup](../../Werewolf/docs/usage.md#install):

```sh
apk add openssh-server@hardened
```

## Policy

Werewolf runs sshd with `UsePAM no`, on a kernel without SELinux. Wolfi's build
still loads libpam, libselinux and libpcre2 into the root processes that face
the network, the path liblzma took into sshd (CVE-2024-3094). Compression and
agent PKCS#11 support carried OpenSSH's own bugs.

| Build option | Change from Wolfi | Effect |
|---|---|---|
| `--without-pam` | No libpam; nothing pulls in linux-pam or its setgid `unix_chkpwd` | `UsePAM` is an unsupported option |
| `--without-selinux` | No libselinux or libpcre2 | No SELinux contexts for sessions |
| `--without-zlib` | No compression (CVE-2016-10012; 10.6's cross-channel plaintext recovery and inflation fixes) | `Compression yes` is a configuration error |
| `--disable-pkcs11` | No PKCS#11 providers in ssh, ssh-agent or ssh-add (CVE-2023-38408) | `ssh -I` and `ssh-add -s` fail; the helper is a stub |

OpenSSL stays: P-256 security keys, including Apple's Secure Enclave keys, need its
ECDSA. Wolfi already builds without Kerberos/GSSAPI, libsystemd and libfido2. The
`openssh-pam-config` package is not published.

Two patches. [0001](patches/0001-no-pty-reopen.patch): Werewolf runs sshd without
`CAP_DAC_OVERRIDE`, so its root child cannot open a pty it has already given to the
user. Where `TIOCSCTTY` made the pty the controlling tty, that reopen was redundant
(the `/dev/tty` check follows), and is left out rather than logged as an error.
[0002](patches/0002-no-default-listen-in-session.patch): sshd-session, which never
listens, no longer resolves the default listen address for every connection when
`ListenAddress` lines are queued; glibc's sort of that lookup asked netlink, which
Werewolf refuses sshd, and each refusal was logged.

## Tests

Wolfi's tests run unchanged. Ours ([tests/probe.sh](tests/probe.sh)) requires
that sshd, sshd-session, sshd-auth, ssh, ssh-agent and ssh-keygen load no PAM,
SELinux, PCRE2 or zlib library; that sshd refuses `Compression yes` and has no
`UsePAM`; that both security key types remain; and that ssh refuses PKCS#11.
Derivation refuses changed upstream configure options, checkout, pipeline, build
dependencies or subpackage layout.

## Troubleshooting

| Symptom | Check / fix |
|---|---|
| `Bad yes/delayed/no argument` for Compression | Remove `Compression yes`; this build has none. |
| `Unsupported option UsePAM` | Remove the line; PAM is not built in. |
| `no support for PKCS#11.` | Use a FIDO security key, or a separately reviewed build. |
