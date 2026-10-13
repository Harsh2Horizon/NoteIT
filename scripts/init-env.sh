#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ -e .env ]; then
    echo '.env already exists; keeping your settings.'
    exit 0
fi
umask 077
set -C
secret() { openssl rand -hex 32; }
cat > .env <<EOF
APP_URL=http://localhost:8080
APP_PORT=8080
MAILPIT_PORT=8025
POSTGRES_PASSWORD=$(secret)
AUTH_DB_PASSWORD=$(secret)
NOTES_DB_PASSWORD=$(secret)
INTERNAL_API_KEY=$(secret)
SMTP_HOST=mailpit
SMTP_PORT=1025
SMTP_FROM=hello@noteit.local
SMTP_REQUIRE_TLS=false
SMTP_USERNAME=
SMTP_PASSWORD=
EOF
echo 'Created .env with random local credentials.'
