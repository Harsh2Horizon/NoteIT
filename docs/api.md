# NoteIt API

Base URL: `http://localhost:8080`. Bodies use `Content-Type: application/json`. Browser clients use the HttpOnly `noteit_session` cookie; command-line clients can use a cookie jar. No JWT or local-storage bearer token is required. Unknown JSON fields are rejected, and request bodies are limited to 1 MiB.

## Authentication

| Method | Path | JSON body / behavior |
| --- | --- | --- |
| POST | `/api/auth/register` | `{ "email": "you@example.com", "password": "at least twelve characters" }`; 202; sends a verification email |
| POST | `/api/auth/verify` | `{ "token": "64-character token from email link" }`; consumes the token; 200 |
| POST | `/api/auth/resend-verification` | `{ "email": "you@example.com" }`; 202; generic response; at most one email/minute/account |
| POST | `/api/auth/login` | `{ "email": "you@example.com", "password": "..." }`; 200 with user ID/email and session cookie |
| GET | `/api/auth/me` | Returns the verified signed-in user's ID and email |
| POST | `/api/auth/logout` | `{}`; revokes the session and clears its cookie |

Verification does not sign in automatically. Passwords are 12+ characters and at most 128 UTF-8 bytes. Email addresses are trimmed and lowercased. An existing registration returns the same generic 202 response and does not replace the existing password. Use resend if needed. Unverified login returns 403; incorrect credentials return 401.

## Notes

All these endpoints require an authenticated, verified owner. Another account receives 404 for an individual note, including mutations.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/notes` | Paginated summaries; active notes by default |
| POST | `/api/notes` | Create a note; optional `title`, `content`, `tags`; 201 |
| GET | `/api/notes/{id}` | Complete note, including archived/trashed notes for their owner |
| PATCH | `/api/notes/{id}` | Autosave fields or change pin/archive state; requires `version` |
| DELETE | `/api/notes/{id}` | Move to Trash and revoke publication; body `{ "version": 4 }` |
| POST | `/api/notes/{id}/restore` | Restore a trashed note to active; body `{ "version": 5 }` |
| GET | `/api/tags` | Unique tags and counts from this account's non-deleted notes, including archive |

Example create body:

```json
{
  "title": "Weekend ideas",
  "content": "# A little list\n\n- Walk outside\n- Read **something good**",
  "tags": ["personal", "ideas"]
}
```

Example autosave/pin body:

```json
{
  "version": 1,
  "content": "# Updated text\n\nEverything saves automatically.",
  "pinned": true
}
```

PATCH supports `title`, `content`, `tags`, `pinned`, and `archived`. Omitted/null fields leave the stored value unchanged; `tags: []` clears tags. Only supplied fields change. The returned `version` increments on every successful mutation, including publication changes. Use that version for the next mutation. Trashed notes cannot be edited or published. Archived notes can be edited, but cannot be published until unarchived.

Limits: title 200 Unicode characters, content 200,000 UTF-8 bytes, 20 tags/note, 40 Unicode characters/tag. Tags are trimmed, lowercased, and deduplicated. Content is text; HTML-looking content is stored literally and escaped by the demonstration renderer. No binary/media fields or upload routes exist.

### Filters

`GET /api/notes?state=active&q=postgres&tag=work&pinned=true&limit=30&offset=0`

- `state`: `active` (default), `archived`, or `trash`.
- `q`: up to 200 bytes; PostgreSQL web-search syntax over title/content, with title/tag substring matching too.
- `tag`: exact normalized tag; combined with the other filters.
- `pinned`: optional `true` or `false`.
- `limit`: 1–100, default 50.
- `offset`: 0–100,000, default 0.

Response: `{ "notes": [...], "total": 12, "limit": 30, "offset": 0 }`. Notes sort by pinned first, then newest update, then ID. Summaries contain an `excerpt` (up to 180 characters) and an empty `content`; fetch the individual note before editing. Count/page queries are separate, so totals can change during concurrent edits.

### Complete note shape

```json
{
  "id": "f1f1f1f1-1234-4567-890a-123456789012",
  "title": "Weekend ideas",
  "content": "# A little list",
  "tags": ["personal", "ideas"],
  "pinned": false,
  "archived": false,
  "deleted_at": null,
  "version": 1,
  "visibility": "private",
  "share_url": null,
  "allowed_emails": [],
  "created_at": "2026-10-03T10:00:00Z",
  "updated_at": "2026-10-03T10:00:00Z"
}
```

### Conflicts

A stale `version`, or an action not permitted in the note's current state, returns:

```json
{
  "error": "This note changed or this action is unavailable in its current state. Reload before continuing.",
  "current": { "id": "...", "version": 3 }
}
```

`current` is the full latest note, shortened above for clarity. Preserve the local draft and let the user resolve the conflict. Do not blindly retry with the server's version: that would defeat lost-update protection. There are no collaborative merges in this sprint.

## Publications

`PUT /api/notes/{id}/publication` (owner only):

```json
{
  "version": 3,
  "visibility": "restricted",
  "allowed_emails": ["friend@example.com"]
}
```

Visibility is `private`, `restricted`, or `public`. Up to 50 allowlist addresses; they are normalized and deduplicated. For private/public modes the allowlist is cleared. Returns the updated complete note with its `share_url`. Updating publication settings preserves the existing URL.

`DELETE /api/notes/{id}/publication` with `{ "version": 4 }` revokes the URL, clears the allowlist, and resets visibility to private. Publishing again creates a new URL.

`GET /api/published/{token}` is a read-only access-controlled endpoint used by `/s/{token}` pages. Public notes need no session; private and restricted notes require a verified session. The owner is always allowed on a currently published link. Anonymous private/restricted requests receive 401. Unauthorized signed-in readers and revoked/missing links receive 404. Allowed responses include only `title`, `content`, and `updated_at`.

## Errors and health

Errors use `{ "error": "message" }`, except conflicts which also include `current`.

| Status | Meaning |
| --- | --- |
| 400 | Invalid fields, limits, state/filter, JSON, or verification token |
| 401 | Missing/expired session or invalid credentials |
| 403 | Unverified account or cross-origin mutation |
| 404 | Missing resource or no access |
| 409 | Stale version or invalid action for current note state |
| 415 | JSON Content-Type required |
| 429 | Auth rate limit; `Retry-After: 60` |
| 500 | Unexpected internal failure |
| 503 | Authentication service or email delivery unavailable |

Each Go service exposes internal `GET /healthz` with a database ping. Nginx exposes `/healthz` for its own process health. `/internal/session` requires the shared internal key and is not proxied by the frontend. API and frontend responses disable browser caching.
