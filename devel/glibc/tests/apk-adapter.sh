mv /usr/bin/apk /usr/bin/apk.real
for app in info add del version update; do ln -s apk.real "/usr/bin/apk_$app"; done
cat > /usr/bin/apk <<'NEUTERED_APK'
#!/bin/sh
case "$1" in
  info|add|del|version|update) app=$1; shift; exec "/usr/bin/apk_$app" "$@" ;;
  *) exec /usr/bin/apk.real "$@" ;;
esac
NEUTERED_APK
chmod 755 /usr/bin/apk
test "$(apk version -t 0-r0 1-r0)" = '<'
if test -n '${{vars.neutered-test-repository}}'; then
  printf '%s\n' '${{vars.neutered-test-repository}}' 'https://packages.wolfi.dev/os' > /etc/apk/repositories
  apk update
fi
