#!/bin/sh
set -eu
export PATH=/usr/libexec/postgresql17:$PATH

# The build runner maps only one UID; PostgreSQL refuses to run a server as
# root. Exercise its installed server through Werewolf's native form test.
# These build checks still reject unwanted authentication libraries and
# missing client, extension and LLVM JIT payloads before packaging.
for binary in /usr/libexec/postgresql17/postgres /usr/lib/libpq.so.5; do
    if ldd "$binary" | grep -E 'lib(pam|ldap|gssapi|krb5)'; then
        echo "optional authentication library remains: $binary" >&2
        exit 1
    fi
done
pg_config --configure | grep -- '--with-llvm'
pg_config --configure | grep -- '--with-openssl'
for binary in postgres psql pg_dump pg_restore; do
    "$binary" --version
done
for module in pg_trgm pgcrypto uuid-ossp llvmjit; do
    test -f "/usr/lib/postgresql17/$module.so"
done
for extension in pg_trgm pgcrypto uuid-ossp; do
    test -f "/usr/share/postgresql17/extension/$extension.control"
done
echo 'PASS: PostgreSQL clients, TLS/JIT configuration, extensions and restricted linkage'
