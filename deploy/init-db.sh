#!/bin/sh
set -eu
# psql's literal quoting keeps generated or user-provided passwords out of SQL syntax.
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
    -v auth_password="$AUTH_DB_PASSWORD" -v notes_password="$NOTES_DB_PASSWORD" <<'SQL'
CREATE USER noteit_auth PASSWORD :'auth_password';
CREATE USER noteit_notes PASSWORD :'notes_password';
CREATE DATABASE noteit_auth OWNER noteit_auth;
CREATE DATABASE noteit_notes OWNER noteit_notes;
REVOKE CONNECT ON DATABASE noteit_auth FROM PUBLIC;
REVOKE CONNECT ON DATABASE noteit_notes FROM PUBLIC;
GRANT CONNECT ON DATABASE noteit_auth TO noteit_auth;
GRANT CONNECT ON DATABASE noteit_notes TO noteit_notes;
SQL
