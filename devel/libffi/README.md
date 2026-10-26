# libffi

Wolfi's libffi recipe, `libffi.yaml`, with its callback trampolines in read-only
library code; currently 3.8.0. It keeps Wolfi's names (`libffi`, `libffi-dev`,
`libffi-pic-dev`, `libffi-doc`) and outranks Wolfi's build, so everything that
links `libffi.so.8` gets it.

## Use

Werewolf: nothing to add. Every image lists the hardened feed, and Python's
`ctypes`, cffi and any other libffi user take this build. Wolfi, after
[feed setup](../../docs/usage.md#install):

```sh
apk add libffi@hardened
```

A Python service that uses `ctypes` still needs the interpreter's own grant,
`allow: [python:native]` on Werewolf, or `/etc/runtime-hardening/allow/SERVICE/python/native`
on Wolfi ([allowances](../../docs/usage.md#grant-an-allowance)). There is no
`libffi:*` grant and no libffi denial record.

## Policy

A callback, a C function pointer into Python or another language, needs code
made for it. Wolfi turns libffi's static trampolines off, so libffi writes that
code at run time: into a page both writable and executable, or into a memory
file mapped twice, writable and executable. Werewolf refuses each: MDWE refuses
writable executable memory, `vm.memfd_noexec=2` an executable memory file, and
`noexec` every writable mount. Static trampolines, libffi's own default on Linux,
run from code in the library's read-only text; only their data is writable.

| Build option | Change from Wolfi | Allowance / policy |
|---|---|---|
| `--enable-exec-static-tramp` | Callbacks run from the library's read-only code. | Always on; the build checks `FFI_EXEC_STATIC_TRAMP` |
| `--disable-pax_emutramp` | No PaX emulated trampolines, which only a PaX kernel uses. | Build-time only |

There are no source patches: `patches/series` is empty.

## Tests

Wolfi's tests run unchanged. Ours ([tests/probe.c](tests/probe.c)) sets
`PR_SET_MDWE`, makes and calls a closure, and requires its code to lie in
`libffi.so`'s own mapping and no mapping to be writable and executable. Wolfi's
build fails it: its closure code is an executable memory file.

## Troubleshooting

| Symptom | Check / fix |
|---|---|
| Hardened Python refuses `import ctypes` | Grant `python:native` to the service; libffi cannot grant the interpreter's permission. |
| A callback fails under MDWE | Inspect `/proc/PID/maps`: the loaded `libffi.so.8` must be this build (`apk info -v libffi`, `-r1000` or higher). |
| Loader reports missing `libffi.so.8` | Include `libffi` in the service's root or OCI image and check its loader paths. |
