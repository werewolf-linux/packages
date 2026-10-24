#!/bin/sh
# probe.sh PROBE: hold the installed python to the protections in patches/series,
# then grant each capability group and see it return. The package test runs it
# as root with /etc/runtime-hardening/allow free to use. PYTHON names another
# interpreter and ALLOW another tree, for a dev build off the read-only root.
set -eu
probe=$1
python=${PYTHON:-python3}
allowdir=${ALLOW:-/etc/runtime-hardening/allow}
base=$allowdir/all/python
work=$(mktemp -d)
trap 'rm -rf "$work" "$allowdir"' EXIT

grant() { mkdir -p "$base"; : > "$base/$1"; }
revoke() { rm -f "$base/$1"; }

expect() {
	name=$1; want=$2; shift 2
	got=$("$@" 2>"$work/err") || true
	if [ "$got" = "$want" ]; then echo "probe: ok: $name"; return; fi
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

version=$("$python" -V 2>&1 | cut -d' ' -f2 | cut -d. -f1-2)
mkdir -p "$work/script" "$work/path" "$work/userbase/lib/python$version/site-packages"
cp "$probe" "$work/script/probe.py"
echo 'NAME = "sibling"' > "$work/script/sibling.py"
echo 'VALUE = 1' > "$work/path/pathevil.py"
echo 'VALUE = 1' > "$work/userbase/lib/python$version/site-packages/userevil.py"
echo 'print("script ran")' > "$work/child.py"

report() {
	# PYTHON* are set but must be ignored, so pathevil and userevil stay unseen.
	env PYTHONPATH="$work/path" PYTHONUSERBASE="$work/userbase" "$python" "$work/script/probe.py"
}

# The probe is a script on a writable mount, so running it needs writable-source;
# that is the harness enabler, tested on its own with a child below.
rm -rf "$allowdir"
grant writable-source

closed='{"deserialize": false, "ignore_environment": 1, "native": false, "no_user_site": 1, "pythonpath": false, "sibling": true, "sqlite_extensions": false, "user_site": false, "userbase": false}'
expect 'closed: PYTHON* ignored, user site off, no native or deserialize' "$closed" report
grep -F '"group":"native"' "$work/err" | grep -F '"detail":"_ctypes"'
grep -F '"group":"deserialize"' "$work/err" | grep -F 'builtins.len'

# The fallback unpickler logs too, even with sys.stderr replaced and the
# exception caught. It must use the same native stderr writer.
cat > "$work/pure-pickle.py" <<'PY'
import io, pickle, sys
sys.stderr = io.StringIO()
try:
    pickle._Unpickler(io.BytesIO(pickle.dumps(len))).load()
except pickle.UnpicklingError:
    print('blocked')
PY
expect 'caught pure-Python pickle denial reaches stderr' blocked "$python" "$work/pure-pickle.py"
grep -F '"group":"deserialize"' "$work/err" | grep -F 'builtins.len'

# inline: -c and piped stdin give code, refused unless python:inline.
refuse '-c is refused' python:inline "$python" -c 'print(1)'
refuse 'piped stdin is refused' python:inline sh -c 'echo "print(1)" | "$0"' "$python"
grant inline
expect '-c returns once inline is allowed' '1' "$python" -c 'print(1)'
revoke inline

# writable-source: a script whose file is on a writable mount, refused unless allowed.
revoke writable-source
refuse 'a script on a writable mount is refused' python:writable-source "$python" "$work/child.py"
grant writable-source
expect 'the same script runs once writable-source is allowed' 'script ran' "$python" "$work/child.py"

# native (ctypes) and deserialize (pickle) each return with their group alone.
grant native
expect 'native allows ctypes' "$(printf '%s' "$closed" | sed 's/"native": false/"native": true/')" report
revoke native
grant deserialize
expect 'deserialize allows pickle' "$(printf '%s' "$closed" | sed 's/"deserialize": false/"deserialize": true/')" report
revoke deserialize

# A link is not an allowance, even to a regular file: lock times will be links.
: > "$work/target"
ln -s "$work/target" "$base/native"
expect 'a link is not an allowance' "$closed" report
rm -f "$base/native"

echo "probe: ok: every python protection holds, and each group takes its own back"
