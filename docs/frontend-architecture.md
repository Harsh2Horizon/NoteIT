# Frontend architecture

See the [architecture overview](architecture.md) for service topology and the [backend guide](backend-architecture.md) for server rules. Sources are [`web/index.html`](../web/index.html), [`web/app.js`](../web/app.js), [`web/autosave.mjs`](../web/autosave.mjs), [`web/markdown.mjs`](../web/markdown.mjs), and [`web/styles.css`](../web/styles.css).

## 1. Delivery and dependencies

The frontend is static HTML/CSS/JavaScript served by nginx. `index.html` loads `/styles.css` and `/app.js` as a native ES module; app.js imports the autosave and Markdown modules. There is no `package.json`, npm dependency installation, compilation step, hydration, or server rendering. Node is used only for JavaScript testing and syntax checking.

The frontend image copies the complete `web/` directory into nginx's document root, including `.test.mjs` files. nginx supplies an explicit JavaScript MIME type for `.mjs`. Ordinary missing paths fall back to index.html, supporting `/s/{token}` without an application router; missing `.mjs` files return 404.

The browser needs native modules, fetch, `AbortSignal.timeout`, localStorage/sessionStorage, `<dialog>.showModal()`, and Blob/object URL APIs. Clipboard copying uses `navigator.clipboard` with a manual-copy fallback. There is no compatibility/transpilation layer.

## 2. DOM structure and view switching

All three main views initially have `hidden` set. The header, notice, and publication dialog are shared.

| DOM area | Purpose and important controls |
| --- | --- |
| `auth-view` | Login/register tabs, email/password form, resend button, verification panel |
| `workspace` | Folder/tag sidebar, note list/search/pagination, editor panel |
| `shared-view` | Published title, update date, Markdown body, error and sign-in prompt |
| `notice` | Global status messages; errors remain visible, other messages hide after 9 seconds |
| `editor` | Title, comma-separated tags, content textarea, preview, save status, note actions |
| `conflict` | Download local draft and load latest server note |
| `publish-dialog` | Visibility, allowlist, share URL, copy and revoke controls |

`$(id)` resolves a DOM element, `show()` changes its `hidden` flag, and `view()` selects one main view. CSS enforces `[hidden]{display:none!important}`. Most list content uses created elements and `textContent`; `innerHTML` is reserved for the custom Markdown renderer's output.

Accessibility hooks include labels (some visually hidden), folder navigation labels, a global status role, polite save-status announcements, publication error alerts, and focus styles. The UI is not accompanied by automated accessibility or keyboard-navigation tests.

## 3. State model

The `state` object contains:

| Field | Meaning |
| --- | --- |
| `user` | Authenticated `{id, email}`, or null |
| `note` | Last complete note acknowledged/fetched from the API |
| `saver` | Current note's Autosave instance |
| `folder` | UI folder: active, pinned, archived, or trash |
| `tag` | Selected exact tag filter or empty string |
| `offset`, `total`, `notes` | Current page offset, total results, summary records |
| `mode` | Login/register form mode |
| `listRequest` | Increasing number used to reject obsolete list results |
| `preview` | Whether content is being displayed in preview mode |

Editor input values live in the DOM. They can be newer than `state.note`, especially during pending saves or conflicts. `editorData()` produces a full title/content/tags snapshot; every editor input sends this snapshot to Autosave. Tag parsing trims comma-separated entries; lowercasing, deduplication, and limits are enforced by the backend.

Module-level `navigating` and `mutating` flags guard note switching/creation and explicit mutations. Other variables hold notice/search timers. This is event-driven DOM code, with no reactive store or component lifecycle.

## 4. Bootstrap and auth flows

`boot()` runs once when the module loads:

1. Read `verify` from the URL fragment. If present, replace the URL with `/`, show the verification panel, register its click handler, and return without checking the current session. The token survives only in the closure until verification/page reload; verification is an explicit click.
2. Otherwise call `/api/auth/me`. A 401 means anonymous; other errors show a notice. Update the account header.
3. Match the path against `/s/[a-f0-9]{64}`. If it matches, load the publication, even for an anonymous user.
4. Otherwise show the workspace for an authenticated user or the login form.

There is no `popstate` or `hashchange` listener. Navigation to sign-in/home/shared links generally reloads the page. Folder selection and search are not encoded in the URL or saved across reloads. An unknown path served by the fallback follows the ordinary workspace/auth bootstrap.

The auth submit handler disables its submit button during requests. Registration shows the generic response, switches to login, and clears the password. Login stores the returned identity and checks `sessionStorage['noteit-return']`; only a path matching the strict shared-link pattern is accepted as a return destination. Otherwise it enters the workspace. Verification does not automatically sign in.

Resend validates the email input before calling its API. Sign-out first awaits autosave flush, then calls logout, disposes the saver, clears in-memory editor/list data, and navigates to `/`. A blocked conflict or failed save can stop sign-out at the flush step. It does not bulk-delete local drafts from other notes/accounts.

## 5. API adapter and error display

`api(path, {method='GET', body})` uses same-origin fetch with a 15-second timeout. It adds JSON Content-Type and a serialized body only when a body is supplied. Session credentials are sent by the browser; there is no Authorization header or JavaScript-held bearer token.

The response body is parsed as JSON. If parsing fails, a generic service-unavailable error body is substituted. Non-success HTTP responses throw an Error with `status` and `data`; conflicts therefore retain the backend's current note in `err.data`, though the UI resolves them by a fresh GET rather than displaying that object directly.

Network/timeout failures throw a connection-unavailable message without an HTTP status. `action(fn)` catches errors from most event handlers and shows a global notice. Publication submission instead displays errors inside the dialog. API errors do not automatically route the user to login; an expired-session save leaves the draft pending and presents the error.

## 6. Lists, filters, and tags

`enterWorkspace()` loads notes and tags in parallel. `loadNotes()` translates the UI's Pinned folder into `state=active&pinned=true`; other folders use their state directly. It sends search text, selected tag, `limit=30`, and offset.

Each list request captures an incremented `listRequest`. A response is applied only if its request number is still current. Old requests are not canceled. If deletion/filtering leaves the offset beyond the new total, the code subtracts one page and retries until a valid page is reached.

`renderList()` rebuilds all cards. A card contains title (with a pin bullet), plain-text excerpt, localized month/day update date, and at most two tags. Selecting a card fetches a complete note; summaries must not populate an editor because their `content` is empty. Pagination shows a range and disables unavailable directions.

`loadTags()` rebuilds alphabetically returned tag buttons with counts. Tag and folder changes flush pending edits before changing filters. Search uses a separate 250 ms debounce and resets offset; pagination and search do not flush the selected editor, because they change the list rather than the open note. List filtering does not automatically close a selected note that no longer matches the filter.

## 7. Opening and creating notes

`openNote(id)` declines a request while navigation or mutation is in progress, disables editor controls, and flushes the prior note. It GETs the complete target note, assigns it, binds a fresh saver, renders the editor/list selection, and checks for a local draft.

For a non-deleted note with a valid stored draft, draft fields replace the editor values. If its stored version matches the fetched version, Autosave resumes. A version mismatch sets `blocked=true` before enqueueing the restored editor snapshot, showing conflict state. A trashed note's stored draft is not loaded.

Creating a note flushes the current one and POSTs empty text/tags. The returned UUID/version becomes the selected note. Folder/tag/search/page filters are reset to the active first page, notes/tags are refreshed, and the title input receives focus. Initial creation requires a network round trip; local drafts are associated with already-created server note IDs.

`renderEditor()` sets title/content/tags, action captions, and note state. Trash fields are read-only and only Restore is exposed among mutation controls. Publishing is disabled for archive. Rendering resets to Write mode; it does not retain preview mode across note selection/mutation.

`updateMeta()` displays the saved note's update date and a whitespace-separated word count from the current content. If preview is active, it rerenders the current local text. Dates use browser locale/timezone and omit year/time in the displayed month/day text.

## 8. Autosave state machine

The standalone `Autosave` class is independent of fetch and the DOM. Its constructor receives an initial version, a save function, status/saved callbacks, and a delay (default 650 ms).

| Property | Purpose |
| --- | --- |
| `version` | Last acknowledged server version |
| `pending` | Fields waiting to be sent, or null |
| `running` | Promise for the single in-flight save, or null |
| `timer` | Debounce or retry timer |
| `blocked` | Conflict stops all further saves |
| `disposed` | Prevents future edits/saves and clears the timer |
| `dirty` getter | True when pending or running is non-null |

`edit(changes)` merges changes into pending, replacing older values for the same field. It resets the debounce timer and emits pending/conflict status. `flush()` clears the timer, rejects if blocked, waits for a running request if necessary, and sends pending fields plus the current version. Successful responses advance the version and invoke `onSaved`. Pending edits accumulated during a request are sent next using the new version.

On failure, the sent changes are merged back underneath newer pending changes. A 409 blocks the saver. Network errors and HTTP status 500+ schedule a retry after three seconds. Other HTTP errors, including 400, 401, 403, and 404, retain pending input but do not schedule automatic retries. Later edits, explicit flushes, or the online event can attempt another save. There is no exponential backoff or retry limit.

`bindSaver()` supplies a PATCH closure tied to the selected note ID. `onSaved` updates `state.note`, persists a newer pending draft or clears it, updates metadata, and starts independent notes/tags refreshes. Errors in those background refreshes are swallowed. Status callbacks update the save label and preserve drafts on failure/conflict.

`dispose()` does not abort an in-flight request. Normal navigation/mutation avoids this issue by awaiting flush before rebinding; handlers that reload a conflicted note fetch and bind the server version directly.

## 9. Draft persistence and conflict recovery

The localStorage key is `noteit-draft:<user.id>:<note.id>`. Its JSON contains `version`, `title`, `content`, and `tags`. Each editor input persists current fields with the saver version. Storage failures show a warning; invalid/malformed drafts are ignored. Draft loading checks field types but does not independently validate a positive numeric version; any unequal version becomes a conflict.

Successful saving clears the draft only when there is no newer pending snapshot. Opening the note is required to discover/recover its stored draft; there is no draft inbox or account-wide draft scan. Browser storage can be cleared or unavailable, so this is a recovery aid rather than a guaranteed persistence layer.

On conflict, input remains editable locally but saving is blocked. Download constructs a Markdown file with a title heading, current content, and a Tags footer using Blob/object URL APIs; it is not a server export or a full note JSON backup. Load latest GETs the server note, clears the local draft, disposes the old saver, and rerenders. It does not require proof that the draft was downloaded first.

Lifecycle hooks:

- `beforeunload`: preserve drafts and request the browser's leave warning if dirty or blocked. No network save is guaranteed during unload.
- `visibilitychange` to hidden: preserve a dirty draft and attempt flush.
- `online`: attempt flush; a blocked conflict still needs manual resolution.

There is no `storage` listener or BroadcastChannel. Two tabs can write the same local draft key; PostgreSQL versions protect server writes, but the localStorage copies do not provide independent per-tab history. A server write that succeeds after the client times out may cause a later retry to get 409, because the client still holds the previous version.

## 10. Explicit mutations and publishing

`mutate(suffix, method, extra)` guards concurrent explicit actions, disables text fields, flushes edits, and sends a request with `state.saver.version`. It replaces the note/saver, rerenders, and refreshes notes/tags. A 409 blocks the saver and exposes draft recovery. Controls are re-enabled in `finally`.

| UI action | Request |
| --- | --- |
| Pin/unpin | PATCH note with `pinned` |
| Archive/unarchive | PATCH note with `archived` |
| Delete | DELETE note with version |
| Restore | POST note `/restore` with version |
| Apply publication | PUT note `/publication` with visibility and parsed allowlist |
| Revoke link | DELETE note `/publication` with version |

Opening the publishing dialog flushes text first, then fills it from the saved note. Restricted addresses are split on newlines/commas and trimmed. Apply is an explicit submit; changing a dropdown does not publish. An existing URL and Revoke button appear only when the note has `share_url`. Copy first tries the clipboard API, then selects the field for manual copying.

Published pages call `/api/published/{token}` and render title/date/body. A 401 exposes Sign in to view; the current path is stored in sessionStorage before navigating home. A 404 or service error exposes only its error text. There is no polling or live subscription: edits become visible when a page requests the publication again.

## 11. Markdown pipeline

`renderMarkdown(source)` normalizes CRLF/CR to LF and processes lines into HTML. It maintains paragraph, list, and fenced-code state. Inline parsing escapes all text outside deliberately generated tags and limits recursive formatting depth (depth greater than five is escaped).

| Input | Rendered behavior |
| --- | --- |
| Paragraphs | `<p>`; internal line breaks become `<br>` |
| `#` through `######` | Heading levels 1–6 |
| Backtick fences | Escaped `<pre><code>`; language marker is not used for highlighting |
| Bullets/numbered items | Flat `<ul>`/`<ol>`; no nested-list hierarchy |
| `[ ]` / `[x]` list tasks | Disabled checkboxes |
| `>` lines | Individual blockquotes rather than a nested blockquote parser |
| Repeated `-`, `*`, `_` | Horizontal rules |
| Bold/emphasis/strike/code | Generated strong/em/del/code elements |
| Links | Absolute HTTP, HTTPS, or mailto only; open in new tab with noopener/noreferrer |
| Markdown images | Alt text only; media URL is discarded |
| Raw HTML | Escaped as visible text |

Relative links and disallowed schemes render their labels without hyperlinks. This parser omits tables, attachments, embedded media, complete CommonMark nesting/escaping rules, and syntax highlighting. Security relies on escaping and generation rules plus nginx CSP; there is no third-party Markdown library or sanitizer.

## 12. CSS and responsive layout

CSS defines the light theme using custom properties and system-font fallbacks; the name Inter does not download a font. Serif headings use Georgia. Desktop workspace columns are 210 px sidebar, 310 px list, and remaining editor width, with independent scrolling.

At 1100 px and below, sidebar/list widths become 170/255 px and padding/title sizes shrink. At 760 px and below, the workspace stacks vertically, folders/tags become horizontal/wrapped, note cards scroll in a horizontal list, and header/auth/shared page sizes adapt. The application does not use a CSS framework or dark theme.

## 13. Function navigation and verification limits

| Group in app.js | Functions |
| --- | --- |
| Shared UI/request helpers | `notice`, `api`, `action`, `view`, `account`, `authMode` |
| Lists and filters | `enterWorkspace`, `loadNotes`, `renderList`, `loadTags` |
| Draft/editor lifecycle | `draftKey`, `editorData`, `persistDraft`, `clearDraft`, `readDraft`, `flush`, `bindSaver`, `openNote`, `renderEditor`, `updateMeta`, `previewMode` |
| Explicit actions | `mutate`, `publicationFields`, attached click/submit handlers |
| Entry/shared page | `shared`, `boot` |

Autosave unit tests cover merged edits, serialized in-flight edits, conflict preservation, and failure recovery. Markdown tests cover the supported subset, unsafe HTML/media, unsafe links, unclosed fences, and large strings. The whole `app.js` DOM workflow, browser storage recovery, responsive appearance, and actual navigation are not covered by a browser test suite; those require the manual checks in [operations](operations.md).
