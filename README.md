# NoteIt

A small, text-only notebook with Go services, PostgreSQL, and a plain HTML/CSS/JavaScript demonstration UI. Notes save automatically. Everything needed to run the app locally is defined in Docker Compose.

## Start locally

Prerequisites: Docker Desktop (or Docker Engine with Compose), `make`, and `openssl`.

```sh
make up
```

This generates `.env` with random database passwords and an internal service key, builds the images, initializes the databases, runs migrations, and waits for service health checks.

- App: <http://localhost:8080>
- Local verification inbox: <http://localhost:8025>

Create an account with any valid email address and a password of at least 12 characters. Open Mailpit, follow the verification link, confirm the email, and sign in. Local email stays in Mailpit; it is not delivered to a real inbox. Use a second verified account to test restricted publishing.

```sh
make logs                  # Follow container logs
make down                  # Stop services; retain notes and accounts
docker compose up -d --build --wait  # Rebuild after code changes
```

The PostgreSQL named volume persists data across restarts and image rebuilds. `docker compose down -v` deletes that volume and all local accounts/notes. Do not use it to perform routine upgrades.

If port 8080 is occupied, set both `APP_PORT=8088` and `APP_URL=http://localhost:8088` in `.env`. Always browse using the configured origin; request origin checks intentionally reject a different host. `MAILPIT_PORT` changes the inbox port; open that port manually because the demo's inbox link defaults to 8025.

## Included behavior

- Registration, one-use email verification, resend verification, sign-in, and logout.
- Automatic note saving after a 650 ms typing pause, with saved/saving/error feedback.
- Notes contain UTF-8 text and basic Markdown: paragraphs, headings, emphasis, strike-through, code, links, quotes, flat lists, and task lists.
- Preview escapes raw HTML, renders Markdown images as their alt text, and blocks embedded media. No uploads or attachments are accepted by the API.
- Multiple normalized tags, tag filtering, keyword search over title/content, title/tag substring matching, pagination, and pinned notes first.
- Pin/unpin, archive/unarchive, and soft deletion into Trash with restore. There is no automatic purge in this sprint.
- Read-only publications with private, restricted, or public access. Published content tracks the latest saved text.
- Version-based conflict detection prevents autosave from silently overwriting another tab's changes.
- Unsaved drafts remain in this browser, scoped by account and note. On conflict, download the local draft before loading the server version. Drafts are removed after a successful save; they are stored as plain text in local storage while pending.

There is no Save button for note text. Publication settings use an explicit action because they change who can access a note.

### Publishing rules

| Mode | Who can read the link |
| --- | --- |
| Private | The signed-in owner only |
| Restricted | The owner and verified accounts whose email addresses are on the allowlist |
| Public | Anyone with the link, including anonymous visitors |

An empty restricted allowlist grants access only to the owner. Allowed people must register and verify their own email; NoteIt does not send sharing invitations. Readers cannot edit. Tags, account IDs, allowlists, and editing metadata are omitted from published responses.

Changing an allowlist or visibility takes effect on subsequent requests. Revoke a link to invalidate its URL; publishing again generates a new random URL. Archiving or deleting also revokes the link. Restoring/unarchiving never republishes automatically. Revocation cannot erase copies a reader already downloaded or a page they already opened.

## Services and storage

```text
Browser → frontend (nginx + static UI, localhost:8080)
              ├── /api/auth/* → auth (Go, internal :8081)
              └── /api/*      → backend (Go, internal :8082)
                                  └── session checks → auth

auth    → PostgreSQL / noteit_auth  (users, sessions, verification tokens)
backend → PostgreSQL / noteit_notes (notes, tags, publications, search index)
auth    → Mailpit SMTP             (local verification inbox)
```

One PostgreSQL 17 container hosts two databases with separate owners and connection permissions. Services share no tables and make no cross-database joins. The backend obtains identity through the auth service's internal HTTP endpoint. Each Go service runs its own embedded, ordered SQL migrations under a transaction/advisory lock when starting. Add future migrations as new numbered files; do not edit migrations that have already been applied.

The frontend is the only public application ingress. Auth, backend, PostgreSQL, and SMTP have no host port mappings. App and Mailpit bindings are loopback-only. Session cookies are HttpOnly and SameSite=Lax, with Secure enabled when `APP_URL` uses HTTPS. Sessions expire in seven days and are revoked at logout. Passwords use salted PBKDF2-HMAC-SHA256 (600,000 iterations); only hashes of random session/verification tokens are stored. Verification links expire in 30 minutes. Login, registration, and verification attempts have a shared per-IP in-memory limit of 20/minute; verification email resend also has a one-minute per-account cooldown. This limiter is appropriate to this single local auth instance; multiple replicas need a shared limiter.

The Go runtime containers run as an unprivileged user with a read-only filesystem. SQL is parameterized; owner checks cover every note mutation. Browser mutations enforce origin checks, and the proxy overwrites client IP headers. The text renderer and Content Security Policy disable media and raw HTML execution. External HTTP(S)/mailto links remain usable.

For real email delivery, configure `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, and a verified `SMTP_FROM`, and set `SMTP_REQUIRE_TLS=true`. The SMTP client supports STARTTLS (typically port 587), not implicit TLS on port 465. Local Mailpit uses unencrypted SMTP inside Docker. Public deployment needs an HTTPS ingress and the corresponding HTTPS `APP_URL`; this Compose setup is for local hosting.

Database credentials are consumed during the first PostgreSQL initialization. Editing them in `.env` does not rotate an existing database user's password. Keep `.env` private and back up the PostgreSQL volume before changing storage or credentials.

## API

See [docs/api.md](docs/api.md) for routes, examples, filters, payloads, and error behavior. All note writes except creation require the current `version`. The API returns `409` with the current note when a stale write or invalid-state action is rejected.

## Architecture documentation

Start with the [architecture overview](docs/architecture.md) for system diagrams, service boundaries, and end-to-end flows. The [frontend guide](docs/frontend-architecture.md) explains views, browser state, autosave, draft recovery, and Markdown rendering. The [backend guide](docs/backend-architecture.md) covers every handler, database schema, authentication, concurrency, and publication access. The [operations guide](docs/operations.md) covers configuration, deployment, tests, troubleshooting, and verification limits.

## Tests

```sh
make test               # Go unit tests with race detector + JS autosave/Markdown tests
make check              # go vet, JS syntax, Docker Compose validation (run make init first)
make integration        # Isolated PostgreSQL + full test suite inside Docker
make integration-local  # Alternative: temporary local PostgreSQL, then API integration tests
```

Host tests need Go 1.26+ and Node 22+. The Docker test runner supplies its own Go and Node. `make integration` uses `compose.test.yaml`, fresh ephemeral databases, and automatic cleanup without touching application data. The integration suite exercises real PostgreSQL, handlers, migrations, and the inter-service HTTP client using an in-process HTTP transport. It covers email verification/replay/expiry, SMTP failure rollback, auth/session revocation, ownership isolation, concurrent writes, search/tags, every publication mode, access removal, archive/delete/restore, and request protection. It does not replace a browser or SMTP delivery smoke test.

GitHub Actions runs the Docker test suite. To use separately provisioned empty test databases, set `TEST_AUTH_DATABASE_URL` and `TEST_NOTES_DATABASE_URL` and run `make integration-local`; never point these at application databases. Integration tests skip when those variables are absent from a normal `make test` run.

### Verification in the build environment

Go builds, unit tests with the race detector, `go vet`, JavaScript tests, syntax checks, and both Compose configurations were checked locally. Docker startup and database integration execution could not run in the restricted build session: access to the Docker socket, PostgreSQL shared memory, and local listening sockets was denied. Consequently, live containers, email delivery, browser interaction, and PostgreSQL integration results still need verification by running the commands above outside that restriction.

## Layout and future work

```text
cmd/auth/          Authentication service entry point
cmd/backend/       Notes service entry point
internal/auth/     Passwords, sessions, verification, SMTP, auth migrations
internal/notes/    Notes, tags, search, publishing, notes migrations
internal/platform/ Shared HTTP, configuration, migrations, database helpers
web/              Framework-free demonstration UI and JavaScript tests
deploy/           Dockerfiles, nginx, database provisioning
tests/            PostgreSQL-backed API integration suite
scripts/          Local setup and isolated test runners
```

Vector search, embeddings, uploads/media, collaboration, revision history, password reset, account administration, and a production UI are deferred. PostgreSQL full-text search is the initial search implementation; a future local knowledge/vector service can consume note IDs and versions through a dedicated integration without changing the text-first note model.

Implementation references: [Docker startup health dependencies](https://docs.docker.com/compose/how-tos/startup-order/), [Mailpit Docker setup](https://mailpit.axllent.org/docs/install/docker/), and [pgx documentation](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool).
