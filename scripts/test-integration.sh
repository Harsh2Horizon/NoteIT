#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
export GOCACHE="${GOCACHE:-$PWD/.cache/go-build}"
export GOMODCACHE="${GOMODCACHE:-$PWD/.cache/go-mod}"
if [ -n "${TEST_AUTH_DATABASE_URL:-}" ] && [ -n "${TEST_NOTES_DATABASE_URL:-}" ]; then
    exec go test -race -count=1 -v ./tests/...
fi
if command -v pg_config >/dev/null 2>&1; then
    pg_bin=$(pg_config --bindir)
else
    echo 'Install PostgreSQL tools or provide TEST_AUTH_DATABASE_URL and TEST_NOTES_DATABASE_URL for empty test databases.' >&2
    exit 1
fi
test_dir=$(mktemp -d /tmp/noteit-test.XXXXXX)
cleanup() {
    "$pg_bin/pg_ctl" -D "$test_dir/data" -m immediate stop >/dev/null 2>&1 || true
    rm -rf "$test_dir"
}
trap cleanup EXIT INT TERM
"$pg_bin/initdb" -D "$test_dir/data" --auth=trust --no-locale --encoding=UTF8 >"$test_dir/init.log"
"$pg_bin/pg_ctl" -D "$test_dir/data" -l "$test_dir/postgres.log" -o "-k $test_dir -h '' -p 55439" -w start || { cat "$test_dir/postgres.log"; exit 1; }
"$pg_bin/createdb" -h "$test_dir" -p 55439 noteit_auth_test
"$pg_bin/createdb" -h "$test_dir" -p 55439 noteit_notes_test
export TEST_AUTH_DATABASE_URL="postgres:///noteit_auth_test?host=$test_dir&port=55439"
export TEST_NOTES_DATABASE_URL="postgres:///noteit_notes_test?host=$test_dir&port=55439"
go test -race -count=1 -v ./tests/...
