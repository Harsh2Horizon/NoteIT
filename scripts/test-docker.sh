#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
cleanup() { docker compose -f compose.test.yaml down --remove-orphans; }
trap cleanup EXIT INT TERM
docker compose -f compose.test.yaml up --build --abort-on-container-exit --exit-code-from tests
