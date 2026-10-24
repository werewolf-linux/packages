#!/bin/sh
set -eu
work=$1
node=${NODE:-node}
base=${ALLOW:-/etc/runtime-hardening/allow}/all/node
trap 'rm -rf "$work" "$base"' EXIT
grant() { mkdir -p "$base"; : > "$base/$1"; }
revoke() { rm -f "$base/$1"; }
refuse() {
    name=$1; token=$2; shift 2
    if "$@" >"$work/out" 2>"$work/err"; then
        echo "probe: FAIL: $name ran" >&2; exit 1
    fi
    if ! grep -F "$token" "$work/err"; then
        echo "probe: FAIL: $name failed for the wrong reason" >&2
        cat "$work/err" >&2; exit 1
    fi
    echo "probe: ok: $name"
}
expect() {
    name=$1; want=$2; shift 2
    if ! got=$("$@" 2>"$work/err") || [ "$got" != "$want" ]; then
        echo "probe: FAIL: $name: expected $want, got $got" >&2
        cat "$work/err" >&2; exit 1
    fi
    echo "probe: ok: $name"
}
cat > "$work/addon.c" <<'ADDON_C'
#include <node_api.h>
static napi_value init(napi_env env, napi_value exports) {
    napi_value value;
    napi_create_int32(env, 42, &value);
    napi_set_named_property(env, exports, "value", value);
    return exports;
}
NAPI_MODULE(NODE_GYP_MODULE_NAME, init)
ADDON_C
case $(uname -s) in
    Darwin) cc -bundle -undefined dynamic_lookup -I"${NODE_INCLUDE:-/usr/include/node}" -o "$work/addon.node" "$work/addon.c" ;;
    *) cc -shared -fPIC -I"${NODE_INCLUDE:-/usr/include/node}" -o "$work/addon.node" "$work/addon.c" ;;
esac
echo 'console.log("ran")' > "$work/child.cjs"
echo 'require("node:worker_threads").parentPort.postMessage(42)' > "$work/worker.cjs"
echo 'module.exports = 42' > "$work/module.cjs"
echo 'export default 42' > "$work/module.mjs"
echo '{"value":42}' > "$work/data.json"
cat > "$work/package.json" <<'PACKAGE_JSON'
{"scripts":{"preinstall":"echo hook-ran > hook-marker"}}
PACKAGE_JSON
rm -rf "$base"
grant writable-source
closed='{"exec":false,"native":false,"sqlite":false,"inspector":false}'
report() { "$node" "$work/probe.cjs" report "$work/addon.node"; }
expect 'default capability floor' "$closed" report
grep -F '"group":"exec"' "$work/err" | grep -F '"event":"runtime-denial"'
grep -F '"group":"native"' "$work/err" | grep -F 'addon.node'
grep -F '"group":"native"' "$work/err" | grep -F 'sqlite extension'
expect 'every child-process API, including raw binding, refuses' blocked \
    "$node" "$work/probe.cjs" exec-paths "$work/child.cjs"
expect 'replacing process.stderr.write cannot hide a caught denial' blocked \
    "$node" "$work/probe.cjs" hide-stderr
grep -F '"group":"exec"' "$work/err" | grep -F '/denied-node-tool'
expect 'audit flags cannot lift the floor' "$closed" "$node" --permission-audit \
    --allow-child-process --allow-addons --allow-wasi \
    "$work/probe.cjs" report "$work/addon.node"
expect 'permission flags cannot lift the floor' "$closed" "$node" --permission \
    --allow-fs-read='*' --allow-fs-write='*' --allow-net --allow-worker \
    --allow-child-process --allow-addons --allow-wasi \
    "$work/probe.cjs" report "$work/addon.node"
expect 'ordinary writable data and SQLite still work' ran \
    "$node" "$work/probe.cjs" data "$work/data.txt"
expect 'WASI cannot bypass native restrictions' blocked "$node" "$work/probe.cjs" wasi
if "$node" --help | grep -q -- --experimental-ffi; then
    expect 'FFI pointers cannot bypass native restrictions' blocked \
        "$node" --experimental-ffi "$work/probe.cjs" ffi
fi
expect 'NODE_OPTIONS is ignored' "$closed" env \
    NODE_OPTIONS=--require=/missing-werewolf-hook \
    "$node" "$work/probe.cjs" report "$work/addon.node"
echo 'NODE_OPTIONS=--require=/missing-werewolf-hook' > "$work/injected.env"
expect 'dotenv cannot inject NODE_OPTIONS' "$closed" "$node" \
    --env-file="$work/injected.env" "$work/probe.cjs" report "$work/addon.node"
expect 'workers cannot inject NODE_OPTIONS' ran \
    "$node" "$work/probe.cjs" worker-options "$work/worker.cjs"
expect 'OpenSSL environment cannot load native code' cleared env \
    OPENSSL_CONF=/missing-werewolf-config OPENSSL_MODULES=/missing-werewolf-modules \
    "$node" "$work/probe.cjs" openssl-env

refuse 'eval' node:inline "$node" -e 'console.log("ran")'
refuse 'print' node:inline "$node" -p 42
refuse 'stdin' node:inline sh -c 'echo "console.log(42)" | "$1"' sh "$node"
refuse 'data URL import' node:inline "$node" "$work/probe.cjs" data-import
refuse 'worker eval' node:inline "$node" "$work/probe.cjs" worker-eval
grant inline
expect 'eval allowance' ran "$node" -e 'console.log("ran")'
expect 'data URL allowance' ran "$node" "$work/probe.cjs" data-import
expect 'worker eval allowance' ran "$node" "$work/probe.cjs" worker-eval
revoke writable-source
refuse 'writable main' node:writable-source "$node" "$work/child.cjs"
grep -F '"group":"writable-source"' "$work/err" | grep -F 'child.cjs'
refuse 'virtual filesystems cannot mask writable code' node:writable-source \
    "$node" --experimental-vfs "$work/child.cjs"
expect 'JSON data stays readable without writable-source' 42 \
    "$node" -e 'console.log(require(process.argv[1]).value)' "$work/data.json"
expect 'ESM JSON data stays readable without writable-source' 42 \
    "$node" --input-type=module -e 'console.log((await import(process.argv[1], {with:{type:"json"}})).default.value)' "$work/data.json"
expect 'disk code caches require writable-source' blocked "$node" -e \
    'const m=require("node:module");console.log(m.enableCompileCache(process.argv[1]).status === m.constants.compileCacheStatus.DISABLED ? "blocked" : "enabled")' "$work/compile-cache"
test ! -e "$work/compile-cache"
refuse 'writable required dependency' node:writable-source \
    "$node" -e 'require(process.argv[1])' "$work/module.cjs"
refuse 'writable imported dependency' node:writable-source \
    "$node" --input-type=module -e 'await import(process.argv[1])' "$work/module.mjs"
grant writable-source
expect 'writable source allowance' ran "$node" "$work/child.cjs"
revoke inline

grant exec
expect 'exec alone' '{"exec":true,"native":false,"sqlite":false,"inspector":false}' report
revoke exec
grant native
expect 'native alone' '{"exec":false,"native":true,"sqlite":true,"inspector":false}' report
revoke native
: > "$work/target"
ln -s "$work/target" "$base/exec"
expect 'symlink is not an allowance' "$closed" report
rm "$base/exec"
mkdir "$base/exec"
expect 'directory is not an allowance' "$closed" report
rmdir "$base/exec"

# The built-in task runner executes package scripts before a JS environment
# exists, so it must enforce the same floor separately from child_process.
cd "$work"
refuse 'package preinstall script' node:exec "$node" --run preinstall
test ! -e hook-marker
grant exec
"$node" --run preinstall
test "$(cat hook-marker)" = hook-ran
revoke exec
refuse 'custom startup snapshot' 'snapshots are disabled' "$node" --snapshot-blob=/missing
refuse 'security revert' 'security reverts are disabled' "$node" --security-revert=CVE-0000-0000
if "$node" --help | grep -q -- --openssl-config; then
    refuse 'OpenSSL configuration cannot load native code' node:native \
        "$node" --openssl-config=/missing "$work/child.cjs"
fi
test ! -e /usr/bin/npm
test ! -e /usr/bin/npx
test ! -e /usr/lib/node_modules/npm
echo 'probe: ok: every Node protection holds, and each allowance restores only its capability'
