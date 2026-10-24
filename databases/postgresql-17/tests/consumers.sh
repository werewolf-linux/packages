#!/bin/sh
set -eu
export PATH=/usr/libexec/postgresql17:$PATH
# Verify both sides of authentication: libpq must not retain GSSAPI/LDAP either.
for binary in /usr/libexec/postgresql17/postgres /usr/lib/libpq.so.5; do
    if ldd "$binary" | grep -E 'lib(pam|ldap|gssapi|krb5)'; then
        echo "optional authentication library remains: $binary" >&2
        exit 1
    fi
done
pg_config --configure | grep -- '--with-llvm'
work=$(mktemp -d)
trap 'su -p -s /bin/sh pgtest -c "pg_ctl -D $work/data -m immediate stop" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT
chown pgtest "$work"
export PGHOST=$work PGUSER=pgtest PGDATABASE=postgres
su -p -s /bin/sh pgtest -c "initdb -D $work/data --no-locale --encoding=UTF8 --auth-local=peer --auth-host=scram-sha-256"
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$work/server.key" -out "$work/server.crt" -days 1 -subj /CN=localhost -addext subjectAltName=DNS:localhost
chmod 600 "$work/server.key"
chown pgtest "$work/server.key" "$work/server.crt"
cat >> "$work/data/postgresql.conf" <<EOF
unix_socket_directories = '$work'
listen_addresses = '127.0.0.1'
ssl = on
ssl_cert_file = '$work/server.crt'
ssl_key_file = '$work/server.key'
EOF
su -p -s /bin/sh pgtest -c "pg_ctl -D $work/data -l $work/server.log -w start"
su -p -s /bin/sh pgtest -c "psql -v ON_ERROR_STOP=1" <<'SQL'
ALTER ROLE pgtest PASSWORD 'consumer-test-password';
CREATE EXTENSION pg_trgm;
CREATE EXTENSION pgcrypto;
CREATE EXTENSION "uuid-ossp";
CREATE TABLE documents(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), body jsonb NOT NULL);
INSERT INTO documents(body) VALUES ('{"title":"common consumer","count":3}');
CREATE INDEX documents_search ON documents USING gin ((body->>'title') gin_trgm_ops);
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM documents WHERE body->>'title' ILIKE '%consumer%') THEN
    RAISE EXCEPTION 'JSONB/search query failed';
  END IF;
END $$;
SQL
export PGPASSWORD=consumer-test-password
# TLS hostname verification plus password authentication, as application drivers use.
export PGHOST=localhost PGSSLMODE=verify-full PGSSLROOTCERT=$work/server.crt
[ "$(psql -At -c "SELECT body->>'count' FROM documents")" = 3 ]
psql -At -c 'SET jit_above_cost=0; EXPLAIN (ANALYZE, FORMAT JSON) SELECT sum(i) FROM generate_series(1,10000) AS i' > "$work/jit.json"
grep '"JIT"' "$work/jit.json"
pg_dump --format=custom --file="$work/backup" postgres
createdb restored
pg_restore --exit-on-error --dbname=restored "$work/backup"
[ "$(psql -d restored -At -c 'SELECT count(*) FROM documents')" = 1 ]
echo 'PASS: peer/SCRAM/TLS, JSONB, common extensions, LLVM JIT and backup/restore'
