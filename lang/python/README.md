# Python

Wolfi's newest stable CPython recipe with runtime restrictions; currently `python-3.15`.

## Use and allowances

Werewolf: add `python-3.15` to `packages`; migrate forms pinned to older streams.
Wolfi, after [feed setup](../../docs/usage.md#install):

```sh
apk add python-3.15@hardened
```

Run `python3 /usr/lib/app/main.py` as a non-root user with code mounted read-only.
Install dependencies into the image or a virtual environment; `pyvenv.cfg` still works.
Add `allow: [python:native]` to the existing Werewolf service for ctypes, or create
`/etc/runtime-hardening/allow/SERVICE/python/native` on Wolfi. Substitute any group
below; see [grant scope and restart instructions](../../docs/usage.md#grant-an-allowance).

## Patches

| Patch | One-line change | Allowance / policy |
|---|---|---|
| [0001](patches/0001-ignore-environment.patch) | Ignore Python configuration environment variables and disable user site packages. | `environment` restores environment only; user site always disabled |
| [0002](patches/0002-no-working-directory-path.patch) | Remove working-directory search from `-m`, `-c` and stdin; scripts retain their directory. | Always on |
| [0003](patches/0003-native.patch) | Refuse loading `_ctypes`. | `native` |
| [0004](patches/0004-deserialize-pickle.patch) | Gate global/class resolution in C and pure-Python pickle. | `deserialize` |
| [0005](patches/0005-run-gate.patch) | Refuse `-c`, piped stdin and main scripts on writable mounts. | `inline`, `writable-source` respectively |
| [0006](patches/0006-refuse-effective-root.patch) | Refuse effective UID 0 during preinitialization, including embedded Python. | Always denied |
| [0007](patches/0007-log-denials-to-stderr.patch) | Emit JSON denials directly to stderr, including pure-Python pickle failures. | Always on |

`environment` is intended for build compatibility; Werewolf accepts `python:environment`.
On Wolfi its file is `.../python/environment`; prefer explicit flags and installed
modules. Application environment reads remain available. Legacy `Py_Initialize`
retains CPython's environment/user-site defaults; other applicable checks remain.

## Debug

Use [stderr denial records](../../docs/usage.md#debug-a-refusal) to identify the group.

| Symptom | Fix |
|---|---|
| `ImportError` importing ctypes | Grant `native`; OS restrictions and shared-library dependencies still apply. |
| `UnpicklingError` resolving a class/global | Prefer plain data or grant `deserialize` for required object reconstruction. Plain data still unpickles. |
| `python -c` or piped script refused | Put the script in the image or grant `inline`. |
| Main script under `/tmp`, `/data` or a writable image layer refused | Mount code read-only or grant `writable-source`; file mode alone does not suffice. |
| `PYTHONPATH`, `PYTHONHOME`, `PYTHONSTARTUP`, `PYTHONUNBUFFERED` ignored | Install modules in the interpreter/venv path; use explicit flags such as `-u`, `-B`, `-X utf8`. |
| `ModuleNotFoundError` after `pip install --user` or `python -m` | Install into the image/venv; neither `~/.local` nor the working directory is automatically searched. |
| `sqlite3.Connection.enable_load_extension` absent | SQLite extension loading is compiled out; `native` cannot restore it. |
| Low-port bind fails | Configure the supervisor's bind capability; Werewolf grants it for a declared port below 1024. |
| Startup refuses root | Drop effective UID before starting or embedding Python; no production allowance exists. |

The source gate covers the main script, not all imports, `-m` modules, `eval` or
custom loaders. Interactive input is not gated by `inline`. `native` gates ctypes,
not every extension module; `deserialize` does not cover third-party unsafe loaders.
Process execution remains subject to OS/Werewolf policy, without a Python `exec` gate.

## Build differences

Keep Wolfi's commit, patches, toolchain, ordinary interpreter PGO and retained tests.
Remove `--enable-loadable-sqlite-extensions`; no allowance restores SQLite loading.
Ship `python-3.15` (a metapackage), `-runtime` (the `python`, `python3` and `pydoc3`
commands, and where Wolfi builds the interpreter), `-base` (the interpreter and
standard library), `-dev`, `-base-dev`; use development packages for builds.
Omit free-threaded `python-3.15t`/`t-dev`, experimental `-jit`, Tkinter `-tk`,
manpages `-doc` and their build copies/files. Omit `-privileged-netbindservice`,
which would attach `CAP_NET_BIND_SERVICE` to the interpreter binary.
Epoch is `1000 × policy revision + Wolfi epoch`; `wolfi-origin` preserves CVE matching.
Build guests grant the groups privately; APKs contain no hardening exception files.
[Installed-package probes](tests/probe.sh) check each grant and reject symlink grants.
[Build instructions](../../docs/wolfi.md#building-locally); patches: PSF-2.0.

Readline and curses native extensions are omitted. GDBM, shelve and SQLite remain
for application data, including persistent Celery schedules; the installed-package
probe writes and reopens a GDBM-backed schedule.
