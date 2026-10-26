# Samba

Wolfi's Samba recipe, `samba.yaml`, without PAM; currently 4.25.1. It keeps
Wolfi's names (`samba-server`, `libauth-samba`, `samba-libs` and the rest) but
`samba-pam-winbind`, and outranks Wolfi's build.

## Use

Werewolf: nothing to add. The `samba` form takes this build from the hardened
feed. Wolfi, after [feed setup](../../docs/usage.md#install):

```sh
apk add samba-server@hardened
```

## Policy

Wolfi builds Samba `--with-pam`, so `libauth-samba`, which the server needs,
links `libpam`, and `libpam` brings `pam_unix` and its `unix_chkpwd` helper.
Werewolf's Samba checks each user against its own password database, written
from the machine's settings, never through PAM.

| Build option | Change from Wolfi | Allowance / policy |
|---|---|---|
| `--without-pam` | No PAM authentication or session code; `linux-pam-dev` leaves the build. | Build-time only |
| No `samba-pam-winbind` | The PAM module for winbind is not built, so its package goes. | Build-time only |

There are no source patches of ours; Wolfi's two apply unchanged.

## Tests

Wolfi's tests run unchanged. Ours ([tests/probe.sh](tests/probe.sh)) requires
no `pam_winbind.so` and no installed server program or Samba library naming
`libpam`.

## Troubleshooting

| Symptom | Check / fix |
|---|---|
| `obey pam restrictions` or a PAM-based `passdb` does nothing | This build has no PAM; use Samba's own password database (`tdbsam`), as the form does. |
| An image still holds `libpam` | Another package depends on it, such as Wolfi's libcap before its hardened build: `apk info -r linux-pam` in the image's lock. |
