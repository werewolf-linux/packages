# busybox

Wolfi's busybox recipe, `busybox.yaml`, with `CONFIG_FEATURE_PREFER_APPLETS` on in all
three variants (`busybox`, `busybox-full`, `busybox-static`); currently 1.38.0. It keeps
Wolfi's names and outranks Wolfi's build, so every Werewolf image with busybox gets it.

## Use

Werewolf: nothing to add. Every image lists the hardened feed. Wolfi, after
[feed setup](../../wolfi/docs/usage.md#install):

```sh
apk add busybox-full@hardened
```

## Policy

Werewolf's glibc refuses a bare-name `execvp` rather than search `PATH`
([glibc](../glibc/README.md)). Wolfi's busybox runs a command by name with a plain
`execvp`, so `env`, `timeout`, `nohup`, `nice`, `xargs`, `setsid`, `time`, `flock` and
`chroot` could run nothing named without a path, in every operator shell. The shell
was unaffected: ash searches `PATH` itself.

| Build option | Change from Wolfi | Effect |
|---|---|---|
| `CONFIG_FEATURE_PREFER_APPLETS=y` | A name that is an applet runs through `CONFIG_BUSYBOX_EXEC_PATH`, `/usr/bin/busybox`, which Wolfi already sets | `env true`, `timeout 5 sh -c …` and `xargs grep` work; `timeout 5 ssh` still needs `/usr/bin/ssh`, as glibc's rule intends |

The option also lets busybox run some applets in-process (no fork) or re-exec itself,
both through that absolute path. There are no source patches: `patches/series` is
empty.

## Tests

Wolfi's tests run unchanged. Ours ([tests/probe.sh](tests/probe.sh)) runs `true` by name
through `env`, `timeout`, `nohup`, `nice`, `setsid`, `flock`, `xargs` and `time` with
`PATH` naming nothing, which no `PATH` search survives, in each variant. Derivation
refuses a changed checkout, subpackage layout or configuration step, and stops for
review once Wolfi sets the option itself. Werewolf's `forms/sshd/test/checks` holds the
image to it.

## Troubleshooting

| Symptom | Check / fix |
|---|---|
| `env: can't execute 'NAME': No such file or directory` | NAME is not a busybox applet: give its path, such as `/usr/bin/ssh`. |
| An applet shows as `busybox` in `ps` | It was re-executed through `/usr/bin/busybox`; its arguments are unchanged. |
