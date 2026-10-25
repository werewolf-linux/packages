#!/bin/sh
set -eu
bindir=$(pg_config --bindir)
export PATH=$bindir:$PATH
# Package tests run with one mapped UID. The native form test runs a server.
for binary in "$bindir/postgres" /usr/lib/libpq.so.5; do
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
libdir=$(pg_config --pkglibdir)
sharedir=$(pg_config --sharedir)
for module in pg_trgm pgcrypto uuid-ossp llvmjit; do
    test -f "$libdir/$module.so"
done
for extension in pg_trgm pgcrypto uuid-ossp; do
    test -f "$sharedir/extension/$extension.control"
done
echo 'PASS: PostgreSQL clients, TLS/JIT configuration, extensions and restricted linkage'
