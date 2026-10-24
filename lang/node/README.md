# Node.js

Wolfi's newest stable Node.js recipe with runtime restrictions; currently `nodejs-26`.

## Use and allowances

Werewolf: use the `node` form or add `nodejs-hardened` to `packages`; that alias
requires our build and follows its newest stream. Wolfi, after [feed setup](../../wolfi/docs/usage.md#install):

```sh
apk add nodejs-hardened@hardened
```

Run `node /usr/lib/app/main.js` as a non-root user, with application code and
`node_modules` mounted read-only. Werewolf's Node form grants top-level `jit` for V8.
Add `allow: [node:native]` to the existing Werewolf service for native addons, or
create `/etc/runtime-hardening/allow/SERVICE/node/native` on Wolfi. Substitute any
supported group below; see [grant scope and restart instructions](../../wolfi/docs/usage.md#grant-an-allowance).

## Patches

| Patch | One-line change | Allowance / policy |
|---|---|---|
| [0001](patches/0001-refuse-effective-root.patch) | Refuse effective UID 0 at process and embedded-environment initialization. | Always denied |
| [0002](patches/0002-read-runtime-allowances-from-the-verified-root.patch) | Read regular allowance files from the service or `all` namespace. | Always on |
| [0003](patches/0003-deny-child-processes-and-native-code-unless-allowed.patch) | Gate child processes/native access and reject security-revert flags. | `exec`, `native`; security reverts always denied |
| [0004](patches/0004-ignore-environment-options-and-user-module-paths.patch) | Ignore internal environment options; remove `NODE_OPTIONS`, `NODE_PATH` and home module searches. | `environment` restores some options; named exclusions stay disabled |
| [0005](patches/0005-gate-inline-code-and-modules-on-writable-mounts.patch) | Gate inline code and ordinary module provenance; reject custom startup snapshots. | `inline`, `writable-source`; custom snapshots always denied |
| [0006](patches/0006-gate-disk-compilation-caches-with-writable-source.patch) | Disable disk compilation caches unless writable source is allowed. | `writable-source` |
| [0007](patches/0007-log-denials-to-stderr.patch) | Emit JSON denial records directly to stderr. | Always on |
| [0008](patches/0008-omit-optional-compression-libraries.patch) | Omit Brotli/Zstd implementations, APIs and HTTP negotiation. | Build-time omission |

| Group | Restores |
|---|---|
| `exec` | Child-process APIs/native bindings and `node --run`, including package-manager lifecycle scripts. |
| `native` | Native addons, FFI (including pointer access), WASI, OpenSSL STORE loaders, SQLite extensions and `--openssl-config`. |
| `inline` | `-e`, `-p`, stdin, REPL, worker `eval` and ordinary `data:` module imports. |
| `writable-source` | Ordinary CommonJS/ESM main/dependency code on writable mounts, virtual module filesystems and disk compile caches. |
| `environment` | Build-compatibility access to internal `NODE_*`/`OPENSSL_*` options; Werewolf accepts `node:environment`. |

`environment` never restores `NODE_OPTIONS`, `NODE_PATH` or home searches. Without
`native`, startup also removes `OPENSSL_CONF`, `OPENSSL_CONF_INCLUDE`, `OPENSSL_MODULES`
and `OPENSSL_ENGINES` because OpenSSL reads them directly. Other application variables remain.
Flags, permission audit mode, worker options and JS settings cannot grant allowances.

## Debug

Read [stderr denial records](../../wolfi/docs/usage.md#debug-a-refusal), then match the failure:

| Symptom | Fix |
|---|---|
| `ERR_ACCESS_DENIED` from spawn/exec/fork or `--run` refused | Grant `exec`, then satisfy Node permissions and Werewolf's allowed executable rules. |
| Addon/FFI/WASI/SQLite extension denied | Grant `native`; installing the addon alone does not authorize loading it. |
| `--openssl-config` refused | Grant `native`; environment-based OpenSSL options may also require `environment`. Prefer explicit configuration. |
| Inline code, REPL or worker eval refused | Use a file in the image or grant `inline`. |
| Module source refused | Mount code and dependencies read-only or grant `writable-source`; unknown mount provenance is refused too. |
| Compile cache reports `DISABLED` | Leave it disabled or grant `writable-source` and enable it through the API/explicit options. |
| Module missing despite `NODE_PATH` | Install under the application's `node_modules` or use explicit paths. |
| npm/npx, inspector, SEA, snapshot or security-revert unavailable | These are omitted/fixed restrictions; no allowance restores them. Build dependencies before deployment. |
| V8 executable-memory failure on Werewolf | Check top-level `jit` and OS memory policy; `native` does not grant JIT permission. |

Ordinary workers, JSON data modules, `fs` data reads and SQLite queries remain.
`eval`, `Function`, `vm`, custom-loader generated code and in-process bytecode are
outside the source gate. These checks do not sandbox hostile JavaScript.
Granting `exec` also permits independently installed npm/pnpm/yarn to launch scripts;
use `--ignore-scripts` during dependency installation and run reviewed builds explicitly.

## Build differences

Keep Wolfi's commit, toolchain, ordinary runtime and headers for application builds.
Build with four compiler jobs, including Ninja's `JOBS` setting. Bubblewrap exposes
all host CPUs through `nproc`; using that count exhausts memory on large runners.
Configure without inspector, npm/npx, `NODE_OPTIONS`, SEA, LIEF, Brotli and Zstd; discard manpages.
Gzip/deflate remain. Fetch advertises only available encodings; Brotli/Zstd responses
fail. No allowance restores omitted codecs. Native addons may carry independent
codec implementations and require their own audit.
Omit minimal/minimal-compat, shared libnode, OCI-entrypoint and documentation subpackages.
Epoch is `1000 × policy revision + Wolfi epoch`; `wolfi-origin` preserves CVE matching.
[Probes](tests/probe.sh) check APIs, workers, grants and real/effective UID mismatches.
Build guests grant groups privately; APKs contain no hardening exception files.
[Build instructions](../../wolfi/README.md#building-locally); patches: MIT.

The separate `node:ffi` API is omitted. Native addons remain gated by the native
allowance; built-in SQLite and TypeScript are retained for common consumers.
