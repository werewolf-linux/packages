#!/bin/sh
# probe.sh PROBE: hold the installed php to the protections in patches/series,
# then grant each capability group and see it return. The package test runs it
# as root with /etc/runtime-hardening/allow free to use. PHP names another
# binary and ALLOW another tree, for a dev build off the read-only root.
set -eu
probe=$1
capabilities=$(dirname "$probe")/capabilities.php
php=${PHP:-php}
allowdir=${ALLOW:-/etc/runtime-hardening/allow}
base=$allowdir/all/php
work=$(mktemp -d)
work=$(cd "$work" && pwd -P)
server_pid=
cleanup() {
    if [ -n "$server_pid" ]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
    rm -rf "$work" "$allowdir"
}
trap cleanup EXIT

grant() { mkdir -p "$base"; : > "$base/$1"; }
revoke() { rm -f "$base/$1"; }

expect() {
	name=$1; want=$2; shift 2
	if got=$("$@" 2>"$work/err") && [ "$got" = "$want" ]; then echo "probe: ok: $name"; return; fi
	echo "probe: FAIL: $name" >&2
	echo "  want $want" >&2
	echo "  got  $got" >&2
	cat "$work/err" >&2
	exit 1
}

refuse() {
	name=$1; token=$2; shift 2
	if "$@" >/dev/null 2>"$work/err"; then
		echo "probe: FAIL: $name ran" >&2
		exit 1
	fi
	if ! grep -q "$token" "$work/err"; then
		echo "probe: FAIL: $name refusal does not name $token" >&2
		cat "$work/err" >&2
		exit 1
	fi
	echo "probe: ok: $name"
}

echo '<?php echo "script ran";' > "$work/child.php"
echo '<?php @eval('"'"'echo "eval ran";'"'"');' > "$work/eval.php"

cat > "$work/user-stream.php" <<'SOURCE'
<?php
class Source {
    public $context;
    function stream_open($p, $m, $o, &$opened) { return true; }
    private $code = "<?php echo 123;";
    function stream_read($n) { $code = $this->code; $this->code = ""; return $code; }
    function stream_stat() { return []; }
    function url_stat($p, $f) { return []; }
    function stream_eof() { return $this->code === ""; }
}
stream_wrapper_register("unverified", Source::class);
include "unverified://source";
SOURCE
printf '<?php include "php://filter/resource=%s/child.php";\n' "$work" > "$work/filter.php"

report() { "$php" "$probe"; }

# The probe is a script on a writable mount, so running it needs writable-source;
# that is the harness enabler, tested on its own with a child below.
rm -rf "$allowdir"
grant writable-source

closed='{"debug":false,"deserialize":false,"exec":false,"listen":false,"native":false,"phar":false,"phar_class":false,"phar_extension":false,"process":false,"special-files":false}'
expect 'closed: no exec, native, deserialize or phar' "$closed" report
grep -F '"group":"deserialize"' "$work/err" | grep -F '"detail":"stdClass"'

cat > "$work/caught.php" <<'PHP'
<?php
ini_set('display_errors', '0');
ini_set('log_errors', '0');
foreach (['system', 'dl'] as $function) {
    try { $function('denied-argument'); } catch (Throwable $e) { }
}
try { system('denied-argument'); } catch (Throwable $e) { }
echo "blocked";
PHP
expect 'caught function denials bypass PHP error settings' blocked "$php" "$work/caught.php"
grep -F '"group":"exec"' "$work/err" | grep -F '"operation":"system"'
grep -F '"group":"native"' "$work/err" | grep -F '"operation":"dl"'
test "$(grep -c '"operation":"system"' "$work/err")" = 2

# inline: -r gives code, refused unless php:inline.
refuse '-r is refused' php:inline "$php" -r 'echo 1;'
grant inline
expect '-r returns once inline is allowed' '1' "$php" -r 'echo 1;'
revoke inline
refuse 'eval, even suppressed, needs inline' php:inline "$php" "$work/eval.php"
cat > "$work/caught-eval.php" <<'PHP'
<?php
ini_set('display_errors', '0');
ini_set('log_errors', '0');
try { @eval('return 7;'); } catch (Error $error) { }
echo "blocked";
PHP
expect 'caught eval denial bypasses PHP error settings' blocked "$php" "$work/caught-eval.php"
grep -F '"group":"inline"' "$work/err" | grep -F '"operation":"eval"'

grant inline
expect 'eval returns only with inline' 'eval ran' "$php" "$work/eval.php"
revoke inline

standard_input() { printf '%s\n' '<?php echo "stdin ran";' | "$php"; }
refuse 'writable-source does not grant stdin' php:inline standard_input
standard_input_path() { printf '%s\n' '<?php echo "stdin ran";' | "$php" -f php://stdin; }
refuse 'CLI rejects php://stdin as a filesystem path' 'input file' standard_input_path
refuse 'writable-source does not grant filters' php:inline "$php" "$work/filter.php"
refuse 'writable-source does not grant user streams' php:inline "$php" "$work/user-stream.php"
refuse 'auto_prepend_file cannot load a generated stream' php:inline \
    "$php" -d "auto_prepend_file=\"php://filter/resource=$work/child.php\"" "$work/child.php"
grant inline
expect 'filters need the inline group' 'script ran' "$php" "$work/filter.php"
expect 'user streams need the inline group' '123' "$php" "$work/user-stream.php"
revoke inline
revoke writable-source
refuse 'stdin needs inline' php:inline standard_input
grant inline
expect 'stdin returns with inline alone' 'stdin ran' standard_input
revoke inline

# writable-source: a script whose file is on a writable mount, refused unless allowed.
revoke writable-source
refuse 'a script on a writable mount is refused' php:writable-source "$php" "$work/child.php"
# A trusted entrypoint does not make the files it includes trusted. Inline is
# the harness enabler here; it must not grant writable-source or stream wrappers.
grant inline
for construct in include include_once require require_once; do
    refuse "$construct refuses writable code" php:writable-source \
        "$php" -r "$construct '$work/child.php';"
done
refuse 'include_path cannot hide writable code' php:writable-source \
    "$php" -d "include_path=$work" -r "include 'child.php';"
ln -s "$work/child.php" "$work/alias.php"
refuse 'a source symlink is checked by its target descriptor' php:writable-source \
    "$php" -r "include '$work/alias.php';"
refuse 'file:// cannot hide writable code' php:writable-source \
    "$php" -r "include 'file://$work/child.php';"
# auto_prepend_file must not bypass the gate.
refuse 'auto_prepend_file refuses writable code' php:writable-source \
    "$php" -d "auto_prepend_file=$work/child.php" "$work/child.php"
grant writable-source
expect 'inline and writable-source grants compose' 'script ran' \
    "$php" -r "include '$work/child.php';"
revoke inline
expect 'the same script runs once writable-source is allowed' 'script ran' "$php" "$work/child.php"

# Warm the shared OPcache in a persistent process, then remove only the file
# allowance. A cache hit must still cross the source guard, without a restart.
printf '%s\n' '<?php $s = opcache_get_status(false); echo $s && $s["opcache_statistics"]["num_cached_scripts"] > 0 ? "cached" : "uncached";' > "$work/cache.php"
port=$((18000 + $$ % 30000))
"$php" -d opcache.enable=1 -d opcache.enable_cli=1 -d opcache.validate_timestamps=0 \
    -d opcache.file_update_protection=0 -d display_errors=stderr \
    -S "127.0.0.1:$port" -t "$work" >"$work/server.log" 2>&1 &
server_pid=$!
ready=false
for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if curl -fsS --max-time 1 -o /dev/null "http://127.0.0.1:$port/child.php"; then ready=true; break; fi
    sleep 0.1
done
[ "$ready" = true ] || { cat "$work/server.log" >&2; exit 1; }
expect 'OPcache really holds compiled code' cached curl -fsS --max-time 5 "http://127.0.0.1:$port/cache.php"
expect 'warm script executes' 'script ran' curl -fsS --max-time 5 "http://127.0.0.1:$port/child.php"
revoke writable-source
: > "$work/cache-response"
: > "$work/server.log"
# PHP may preserve HTTP 200 when display_errors=stderr. Judge execution and
# the refusal diagnostic, not just the HTTP status.
if curl -fsS --max-time 5 -o "$work/cache-response" "http://127.0.0.1:$port/child.php"; then
    :
else
    [ "$?" = 22 ] || { cat "$work/server.log" >&2; exit 1; }
fi
grep -q php:writable-source "$work/server.log"
if grep -q 'script ran' "$work/cache-response"; then
    echo 'probe: FAIL: cached writable code ran without its allowance' >&2; exit 1
fi
echo 'probe: ok: cached writable code is refused without a restart'
grant writable-source
expect 'the running cache observes the restored allowance' 'script ran' \
    curl -fsS --max-time 5 "http://127.0.0.1:$port/child.php"
kill "$server_pid"
wait "$server_pid" 2>/dev/null || true
server_pid=

# The multipart parser is shared by automatic POST handling and
# request_parse_body(). Keep it shut before callbacks or temporary files exist.
mkdir "$work/upload-tmp" "$work/progress"
cat > "$work/upload.php" <<'PHP'
<?php
$post = $_POST;
$files = $_FILES;
if ($_SERVER['REQUEST_METHOD'] === 'PUT' || isset($_GET['parse'])) {
    try {
        [$post, $files] = request_parse_body(['max_file_uploads' => 5]);
    } catch (RequestParseBodyException $error) {
        echo str_contains($error->getMessage(), 'php:upload') ? 'refused' : 'wrong exception';
        return;
    }
}
$file = $files['upload']['tmp_name'] ?? '';
$uploaded = $file !== '' && is_uploaded_file($file);
if (isset($_GET['revoke'])) {
    unlink(trim(file_get_contents(__DIR__ . '/upload-allowance')));
}
$moved = $uploaded && @move_uploaded_file($file, __DIR__ . '/uploaded');
echo json_encode(['post' => $post['note'] ?? null, 'files' => count($files),
                  'uploaded' => $uploaded, 'moved' => $moved]);
PHP
printf '%s\n' "$base/upload" > "$work/upload-allowance"
cat > "$work/move-upload.php" <<'PHP'
<?php
echo @move_uploaded_file(__FILE__, __DIR__ . '/should-not-exist') ? 'moved' : 'refused';
PHP
expect 'upload moves are denied and logged even when suppressed' refused "$php" "$work/move-upload.php"
grep -F '"group":"upload"' "$work/err" | grep -F '"operation":"move_uploaded_file"'

# Explicitly enabling PHP's ordinary settings cannot supply the allowance.
"$php" -d file_uploads=1 -d display_errors=stderr -d log_errors=0 \
    -d "upload_tmp_dir=$work/upload-tmp" -d "session.save_path=$work/progress" \
    -d session.upload_progress.enabled=1 -d session.upload_progress.cleanup=0 \
    -S "127.0.0.1:$port" -t "$work" >"$work/upload-server.log" 2>&1 &
server_pid=$!
ready=false
for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if curl -fsS --max-time 1 -o /dev/null "http://127.0.0.1:$port/child.php"; then ready=true; break; fi
    sleep 0.1
done
[ "$ready" = true ] || { cat "$work/upload-server.log" >&2; exit 1; }

multipart() {
    method=$1; endpoint=${2:-upload.php}
    curl -fsS --max-time 5 -X "$method" -b PHPSESSID=uploadprobe \
        -F PHP_SESSION_UPLOAD_PROGRESS=probe -F note=plain \
        -F "upload=@$work/child.php;filename=payload.php" \
        "http://127.0.0.1:$port/$endpoint"
}
no_upload='{"post":null,"files":0,"uploaded":false,"moved":false}'
yes_upload='{"post":"plain","files":1,"uploaded":true,"moved":true}'
expect 'POST multipart is refused without upload' "$no_upload" multipart POST
expect 'PUT request_parse_body cannot bypass upload' refused multipart PUT
expect 'text-only multipart also stays outside the parser' "$no_upload" \
    curl -fsS --max-time 5 -F note=plain "http://127.0.0.1:$port/upload.php"
expect 'a malformed boundary is refused before multipart parsing' "$no_upload" \
    curl -fsS --max-time 5 -H 'Content-Type: multipart/form-data' \
        --data-binary invalid "http://127.0.0.1:$port/upload.php"
expect 'ordinary URL-encoded forms still work' \
    '{"post":"plain","files":0,"uploaded":false,"moved":false}' \
    curl -fsS --max-time 5 -d note=plain "http://127.0.0.1:$port/upload.php"
test -z "$(ls -A "$work/upload-tmp")"
test -z "$(ls -A "$work/progress")"
test ! -e "$work/uploaded"
grep -F '"group":"upload"' "$work/upload-server.log" | grep -F '"operation":"parse"'

: > "$work/target"
ln -s "$work/target" "$base/upload"
expect 'an upload allowance symlink is refused' "$no_upload" multipart POST
revoke upload
grant upload
expect 'POST uploads and moves return with upload alone' "$yes_upload" multipart POST
cmp "$work/child.php" "$work/uploaded"
test -z "$(ls -A "$work/progress")"
grep -F '"group":"upload-progress"' "$work/upload-server.log" | grep -F '"operation":"session.upload_progress"'
ln -s "$work/target" "$base/upload-progress"
expect 'a symlink cannot enable upload progress' "$yes_upload" multipart POST
test -z "$(ls -A "$work/progress")"
revoke upload-progress
grant upload-progress
expect 'both grants permit session upload progress' "$yes_upload" multipart POST
grep -q upload_progress_probe "$work/progress/sess_uploadprobe"
rm -f "$work/progress/sess_uploadprobe"
revoke upload
expect 'upload-progress alone cannot enable uploads' "$no_upload" multipart POST
test -z "$(ls -A "$work/progress")"
grant upload
revoke upload-progress
expect 'progress revocation preserves ordinary uploads' "$yes_upload" multipart POST
test -z "$(ls -A "$work/progress")"
expect 'PUT uploads return through request_parse_body' "$yes_upload" multipart PUT
cmp "$work/child.php" "$work/uploaded"
expect 'upload does not grant exec, native, deserialize or phar' "$closed" report
rm -f "$work/uploaded"
expect 'revocation after parsing prevents an upload move' \
    '{"post":"plain","files":1,"uploaded":true,"moved":false}' multipart POST 'upload.php?revoke'
test ! -e "$work/uploaded"
grep -F '"group":"upload"' "$work/upload-server.log" | grep -F '"operation":"move_uploaded_file"'
expect 'a running server observes upload revocation' "$no_upload" multipart POST
expect 'explicit parsing also observes upload revocation' refused multipart PUT
kill "$server_pid"
wait "$server_pid" 2>/dev/null || true
server_pid=

# Disabling automatic POST handling must not bypass the explicit parsing guard.
"$php" -d enable_post_data_reading=0 -d display_errors=stderr \
    -S "127.0.0.1:$port" -t "$work" >"$work/explicit-server.log" 2>&1 &
server_pid=$!
ready=false
for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if curl -fsS --max-time 1 -o /dev/null "http://127.0.0.1:$port/child.php"; then ready=true; break; fi
    sleep 0.1
done
[ "$ready" = true ] || { cat "$work/explicit-server.log" >&2; exit 1; }
expect 'explicit POST parsing still needs upload' refused \
    curl -fsS --max-time 5 -F "upload=@$work/child.php" "http://127.0.0.1:$port/upload.php?parse"
grant upload
expect 'explicit POST parsing returns with upload' "$yes_upload" \
    curl -fsS --max-time 5 -F note=plain -F "upload=@$work/child.php" "http://127.0.0.1:$port/upload.php?parse"
revoke upload
kill "$server_pid"
wait "$server_pid" 2>/dev/null || true
server_pid=

# Each function group returns alone. Namespace APIs depend on the platform.
for group in exec native deserialize process listen special-files debug; do
	grant "$group"
	expect "$group is taken back alone" \
		"$(printf '%s' "$closed" | sed 's/"'"$group"'":false/"'"$group"'":true/')" report
	revoke "$group"
done

# Optional capabilities are disabled before evaluating arguments, even with
# an empty disable_functions setting. The same table must log every group.
expect 'optional functions remain closed despite -d' closed \
    "$php" -d disable_functions= "$capabilities" floor
for group in exec namespace process listen special-files debug; do
    grep -F '"group":"'"$group"'"' "$work/err" >/dev/null
done
for group in namespace process listen special-files debug; do
    ln -s "$work/target" "$base/$group"
    expect "$group refuses a symlink grant" closed "$php" "$capabilities" floor
    revoke "$group"
done
grant namespace
expect 'namespace grants do not open process creation' namespace "$php" "$capabilities" namespace
revoke namespace
grant process
expect 'process alone permits fork and detachment' forked "$php" "$capabilities" process
revoke process
grant listen
expect 'listen alone permits creating and accepting a listener' listening "$php" "$capabilities" listen
revoke listen
grant special-files
expect 'special-files alone permits FIFO creation' created "$php" "$capabilities" special-files "$work"
revoke special-files
"$php" -d display_errors=stderr -i >"$work/info" 2>"$work/err"
test ! -s "$work/info"
grep -F '"group":"debug"' "$work/err" >/dev/null
grant debug
expect 'debug alone restores phpinfo' information "$php" "$capabilities" debug
"$php" -i >"$work/info"
grep -q 'PHP Version' "$work/info"
revoke debug

# Debug output does not grant execution, uploads, or ordinary reflection.
expect 'logging, exception traces and programmatic inspection work without debug' ordinary \
    "$php" "$capabilities" debug-ordinary "$work"
grep -q debug-ordinary-log "$work/debug-ordinary.log"
expect 'ReflectionExtension info cannot bypass debug' refused \
    "$php" "$capabilities" debug-module-denied
grep -F '"group":"debug"' "$work/err" | grep -F '"operation":"module_info"'
for extension in main standard; do
    refuse "--ri $extension requires debug" php:debug "$php" --ri "$extension"
done
# The old diagnostics grant must not restore debug functions or traces.
grant diagnostics
expect 'the retired diagnostics grant does not restore debug functions' "$closed" report
revoke diagnostics

fatal_probe() {
    mode=$1; setting=$2
    if "$php" -d display_errors=stderr -d log_errors=0 -d "fatal_error_backtraces=$setting" \
        "$capabilities" "$mode" "$base/debug" >"$work/fatal-out" 2>"$work/fatal-err"; then
        echo "probe: FAIL: $mode did not terminate" >&2; exit 1
    else
        test "$?" = 255
    fi
}
no_fatal_trace() {
    if grep -q 'Stack trace:' "$work/fatal-out" "$work/fatal-err"; then
        echo 'probe: FAIL: fatal backtrace captured without permission' >&2; exit 1
    fi
    grep -F '"group":"debug"' "$work/fatal-err" | grep -F '"operation":"fatal_error_backtraces"'
}
for mode in debug-fatal debug-fatal-quiet debug-oom; do
    fatal_probe "$mode" 1
    no_fatal_trace
done
fatal_probe debug-fatal-enable 0
no_fatal_trace
ln -s "$work/target" "$base/debug"
fatal_probe debug-fatal 1
no_fatal_trace
revoke debug
grant diagnostics
fatal_probe debug-fatal 1
no_fatal_trace
revoke diagnostics
grant debug
fatal_probe debug-fatal 1
grep -q 'Stack trace:' "$work/fatal-err"
grep -q 'debug_fatal_probe' "$work/fatal-err"
# An allowance permits the feature; it does not override an explicit INI opt-out.
fatal_probe debug-fatal 0
if grep -q 'Stack trace:\|runtime-denial' "$work/fatal-err"; then
    echo 'probe: FAIL: debug overrode fatal_error_backtraces=0' >&2; exit 1
fi
"$php" --ri main >"$work/info"
grep -q fatal_error_backtraces "$work/info"
"$php" --ri standard >"$work/info"
grep -q standard "$work/info"
fatal_probe debug-fatal-revoke 1
no_fatal_trace
echo 'probe: ok: fatal capture needs debug despite INI settings and observes revocation'

# A harmless mail transport proves all three entrypoints cross the helper.
cat > "$work/sendmail" <<'SH'
#!/bin/sh
cat >/dev/null
printf 'sent\n' >> "$0.sent"
SH
chmod +x "$work/sendmail"
expect 'mail-mode error_log needs exec while file logging works' refused \
    "$php" -d "sendmail_path=$work/sendmail" "$capabilities" mail-denied "$work"
test ! -e "$work/sendmail.sent"
grep -q ordinary-log "$work/ordinary.log"
grep -F '"group":"exec"' "$work/err" | grep -F '"detail":"error_log"'
grant exec
expect 'exec permits both mail functions and mail-mode error_log' sent \
    "$php" -d "sendmail_path=$work/sendmail" "$capabilities" mail
test "$(wc -l < "$work/sendmail.sent" | tr -d ' ')" = 3
expect 'the shared helper observes exec revocation in a running PHP' refused \
    "$php" -d "sendmail_path=$work/sendmail" "$capabilities" mail-revoke "$base/exec"
test "$(wc -l < "$work/sendmail.sent" | tr -d ' ')" = 3
test "$(grep -c '"group":"exec"' "$work/err")" = 3

# No SQLite headers are required: the extension records reaching its entrypoint.
cat > "$work/loader.c" <<'C'
#include <stdio.h>
struct sqlite3;
struct sqlite3_api_routines;
int sqlite3_extension_init(struct sqlite3 *db, char **error,
                          const struct sqlite3_api_routines *api) {
    (void)db; (void)error; (void)api;
    FILE *f = fopen(__FILE__ ".loaded", "w");
    if (!f) return 1;
    fputs("loaded", f);
    fclose(f);
    return 0;
}
C
cc -shared -fPIC -o "$work/loader.so" "$work/loader.c"
expect 'SQLite queries work while native library loading is denied' refused \
    "$php" -d "sqlite3.extension_dir=$work" "$capabilities" native-denied "$work"
grep -F '"group":"native"' "$work/err" | grep -F '"operation":"loadExtension"'
ln -s "$work/target" "$base/native"
expect 'SQLite native loading rejects a symlink grant' refused \
    "$php" -d "sqlite3.extension_dir=$work" "$capabilities" native-denied "$work"
revoke native
grant native
expect 'native permits reaching SQLite extension entrypoints' loaded \
    "$php" -d "sqlite3.extension_dir=$work" "$capabilities" native-load "$work"
expect 'SQLite loaders observe native revocation after connecting' refused \
    "$php" -d "sqlite3.extension_dir=$work" "$capabilities" native-revoke "$work" "$base/native"

# FFI and SQLite3 are not added to the packages. Dev builds that include them
# exercise their guarded paths too; the shipped driver is PDO SQLite.
if [ "$("$php" "$capabilities" ffi-present)" = yes ]; then
    printf '#define FFI_SCOPE "probe"\nint abs(int);\n' > "$work/ffi.h"
    expect 'ffi.enable cannot bypass native' refused \
        "$php" -d ffi.enable=1 "$capabilities" ffi-denied "$work/ffi.h"
    grep -F '"group":"native"' "$work/err" | grep -F '"operation":"ffi"'
    refuse 'FFI header preloading needs native' php:native \
        "$php" -d "ffi.preload=$work/ffi.h" "$capabilities" ffi-scope
    grant native
    expect 'native permits FFI declarations and header loading' loaded \
        "$php" -d ffi.enable=1 "$capabilities" ffi-load "$work/ffi.h"
    expect 'native permits FFI header preloading and scope access' preloaded \
        "$php" -d "ffi.preload=$work/ffi.h" "$capabilities" ffi-scope
    revoke native
fi

# Phar is not built; an allowance cannot restore its extension, class or wrapper.
grant phar
expect 'Phar stays absent even with an allowance' "$closed" report
revoke phar

# A link is not an allowance, even to a regular file: lock times will be links.
: > "$work/target"
ln -s "$work/target" "$base/exec"
expect 'a link is not an allowance' "$closed" report
rm -f "$base/exec"

echo "probe: ok: every php protection holds, and each group takes its own back"
