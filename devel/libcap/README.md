# libcap

Wolfi's libcap recipe, `libcap.yaml`, without its PAM module; currently 2.78. It
keeps Wolfi's names (`libcap`, `libcap-dev`, `libcap-utils`, `libcap-doc`) and
outranks Wolfi's build, so everything that links `libcap.so.2` gets it.

## Use

Werewolf: nothing to add. Every image lists the hardened feed. Wolfi, after
[feed setup](../../wolfi/docs/usage.md#install):

```sh
apk add libcap@hardened
```

## Policy

Wolfi's `libcap` package carries `pam_cap.so`, so every program that links
libcap, Squid and Samba among them, depends on `libpam`, and `libpam` brings
`pam_unix` and its `unix_chkpwd` helper. Werewolf checks no UNIX password and has
no PAM use, so libcap is built without the module.

| Build option | Change from Wolfi | Allowance / policy |
|---|---|---|
| `PAM_CAP=no` | No `pam_cap.so`; `linux-pam-dev` leaves the build. | Build-time only |

There are no source patches. libcap is on git.kernel.org with no GitHub mirror,
so its releases are read from there: the `git-tags` upstream source, tags
`v1.2.N`.

## Tests

Wolfi's tests run unchanged. Ours ([tests/probe.sh](tests/probe.sh)) requires
no `pam_cap.so` and no installed library naming `libpam`.

## Troubleshooting

| Symptom | Check / fix |
|---|---|
| A PAM stack names `pam_cap.so` | This build has none; Werewolf grants capabilities through each service's form, not PAM. |
| An image still holds `libpam` | Another package depends on it: `apk info -r linux-pam` in the image's lock. |
