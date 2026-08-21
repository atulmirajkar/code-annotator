# Design: annotation review and AI-agent handoff

## Status

Draft for review. This design defines milestone 6 in
[`../../project_status.md`](../../project_status.md), ahead of live reload.

## Problem

An AI agent may create a Markdown design, plan, or report that a human wants to
review in context. Editing the document to insert comments mixes feedback with
authored content, and prose feedback sent separately loses its connection to the
exact passage. The viewer needs a review mode that lets a person attach
structured comments to rendered Markdown and hand unresolved actions back to an
AI agent without modifying the source document.

## Goals

- Select rendered text and attach a comment to that exact passage.
- Add document-level comments when no text selection is appropriate.
- Classify feedback so an agent can distinguish questions from requested work.
- Keep annotations separate from Markdown and easy to inspect in Git.
- Preserve anchors when surrounding content moves or line numbers change.
- Detect and clearly display annotations that may be stale after document edits.
- Export unresolved annotations in deterministic JSON or agent-friendly
  Markdown.
- Track an annotation from creation through acknowledgement and resolution.
- Keep inline reviewer and agent replies as a durable thread across repeated
  implementation attempts.
- Keep the existing viewer read-only unless review mode is explicitly enabled.

## Non-goals

- Automatically execute an annotation as an AI action.
- Edit or rewrite the reviewed Markdown in the browser.
- Provide Google Docs-style real-time multi-user collaboration.
- Synchronize annotations through a hosted service.
- Expose writable review APIs on a non-loopback listener.
- Build an MCP server or vendor-specific agent integration in the first slice.

## Proposed user experience

Start an explicitly writable review session:

```sh
md-viewer --review ./docs
```

The default annotation directory is:

```text
<content-root>/.md-viewer/annotations/
```

An alternate location can be selected when the content root should remain
untouched:

```sh
md-viewer --review --annotations-dir ./reviews ./docs
```

Without `--review`, the viewer retains its current read-only behavior and does
not expose mutation endpoints.

### Browser workflow

1. The reviewer selects text in the rendered document.
2. A small `Comment` action appears near the selection.
3. The reviewer chooses an intent and enters feedback.
4. The passage is highlighted and a card appears in a fixed right review panel.
5. Selecting a highlight focuses its card; selecting a card scrolls to its
   passage.
6. The reviewer can filter by status or intent and add document-level comments.
7. After an agent reports an applied change, the reviewer can accept it or reply
   inline with `Needs changes`.
8. `Needs changes` keeps the same annotation and anchor active and returns it to
   the agent queue with the complete discussion and attempt history.

On desktop, the layout becomes document navigation, document content, and review
panel. On narrow screens, the review panel becomes a drawer. Annotation creation
must remain usable with keyboard selection and focus navigation.

## Annotation lifecycle

### Intents

| Intent | Meaning for an agent |
| --- | --- |
| `question` | Explain or clarify; a source change may not be necessary. |
| `suggestion` | Consider an improvement; judgment is expected. |
| `change_request` | Make a concrete source or implementation change. |
| `approval` | This passage or document is accepted as written. |

### Statuses

```text
open -> acknowledged -> applied -> closed
  |          |            |
  |          |            +-> needs_changes -> acknowledged -> applied
  |          |
  +----------+----------------> rejected

closed or rejected -> open
```

`applied` means an agent or author reports that work was performed. `closed`
means the reviewer accepted the outcome. Keeping those states distinct prevents
an agent from closing its own review request implicitly. `needs_changes` means
an attempted resolution was not satisfactory and remains actionable. It is not
the same as `rejected`, which records a decision not to perform the request.

Only a reviewer can transition `applied` to `closed`. When a reviewer selects
`Needs changes`, a reply is required and the annotation transitions from
`applied` to `needs_changes`. The next agent attempt reuses the same annotation
ID rather than creating a replacement annotation.

The domain model validates transitions against the actor role:

| Actor | Allowed transitions |
| --- | --- |
| Agent | `open -> acknowledged`, `open -> rejected` |
| Agent | `acknowledged -> applied`, `acknowledged -> rejected` |
| Agent | `needs_changes -> acknowledged` |
| Reviewer | `applied -> closed`, `applied -> needs_changes` |
| Reviewer | `closed -> open`, `rejected -> open` |

Skipping acknowledgement, repeating the current status, or having an agent close
its own work is invalid. Status-change thread entries record the actor role and
the previous and next statuses so the transition can be validated again when a
sidecar is loaded.

## Storage model

Store one versioned JSON sidecar per Markdown document and mirror its relative
path beneath the annotation directory:

```text
docs/designs/architecture.md
.md-viewer/annotations/designs/architecture.md.json
```

This layout is predictable for humans and agents, produces focused Git diffs,
and avoids one highly contended repository-wide database file.

### Sidecar schema

```json
{
  "schemaVersion": 1,
  "document": "designs/architecture.md",
  "annotations": [
    {
      "id": "ann_01J7Y8Y4T9J2YQ8M5CQ6E3K2P1",
      "intent": "change_request",
      "status": "open",
      "comment": "Make the listen address configurable while preserving loopback as the default.",
      "author": "atul",
      "createdAt": "2026-08-20T19:30:00Z",
      "updatedAt": "2026-08-20T19:30:00Z",
      "source": {
        "sha256": "8cf0...",
        "selector": {
          "exact": "Bind to 127.0.0.1 by default",
          "prefix": "Network and lifecycle ",
          "suffix": " never all interfaces implicitly.",
          "startByte": 2410,
          "endByte": 2444,
          "startLine": 104,
          "endLine": 104
        }
      },
      "thread": []
    }
  ]
}
```

IDs should be sortable, collision-resistant identifiers generated locally.
Timestamps use UTC RFC 3339. Relative document paths always use `/` separators.
Unknown fields must be preserved when rewriting a supported schema version so
future agent metadata is not silently discarded.

### Threaded activity and resolution attempts

The original `comment` remains immutable review context. Follow-up discussion
and resolution attempts are appended to `thread` in chronological order. An
agent reporting an applied change may append:

```json
{
  "id": "msg_01J7Z0A8Q2K4P7M6N5R3T1V9W8",
  "kind": "resolution",
  "summary": "Added --listen while retaining 127.0.0.1 as the default.",
  "commit": "abc1234",
  "author": "codex",
  "createdAt": "2026-08-20T20:15:00Z"
}
```

If the result is unsatisfactory, the reviewer chooses `Needs changes` and
appends a reply:

```json
{
  "id": "msg_01J7Z0N4S8B6H3Q2C9D5F7K1M0",
  "kind": "review",
  "message": "The implementation replaced the loopback default. Keep 127.0.0.1 unless the flag is supplied.",
  "author": "atul",
  "createdAt": "2026-08-20T20:30:00Z"
}
```

Allowed thread kinds are `reply`, `acknowledgement`, `resolution`, `review`, and
`status_change`. Resolution entries may carry a commit reference and summary;
ordinary replies carry a message. Existing thread entries are append-only.
Corrections are represented by another entry so the review history is not
silently rewritten.

Each thread entry has its own `msg_` identifier, author, UTC timestamp, and kind.
`status_change` entries additionally contain `actorRole`, `fromStatus`, and
`toStatus`. Thread entries must be chronological, cannot predate the annotation,
and cannot be newer than the annotation's `updatedAt`. Annotation and thread IDs
must be unique within a sidecar.

The viewer treats an agent resolution as a report, not proof. A human either
transitions it to `closed` or responds with `needs_changes`. Every subsequent
agent export includes the original request, all resolution attempts, and all
reviewer replies.

## Anchoring and stale detection

Line numbers alone are too fragile. Each text annotation records complementary
selectors:

- Exact selected source text provides the primary semantic anchor.
- Prefix and suffix context disambiguate repeated text.
- UTF-8 byte offsets provide a fast exact match against the original revision.
- Line numbers provide understandable diagnostics and agent context.
- A SHA-256 document digest identifies the source revision used at creation.

Selector creation accepts UTF-8 byte offsets with an exclusive end position. It
rejects invalid UTF-8, empty or out-of-range selections, and offsets that split a
multi-byte character. It derives the exact quote, one-based line range, SHA-256
digest, and up to 64 adjacent bytes of prefix and suffix context on rune
boundaries. Digests are emitted as lowercase hexadecimal; comparison accepts
either hexadecimal case because both encodings represent the same digest.

Anchor resolution proceeds in this order:

1. Validate the stored source selector and current document UTF-8.
2. If the digest matches and the stored byte range still contains the exact
   quote, return `exact` with current byte and line positions.
3. Otherwise, find every exact quote occurrence, including overlapping matches.
4. If exactly one occurrence also matches the stored prefix and suffix, return
   `moved`.
5. If no contextual match exists but there is exactly one exact occurrence,
   return `moved` with the weaker unique-quote match.
6. If no occurrence exists, return `stale` with reason `not_found`.
7. If multiple candidates remain, return `stale` with reason `ambiguous` and the
   candidate count; never choose one silently.

Stale resolution is expected data rather than an API error. Invalid selectors or
invalid current UTF-8 are errors. A valid selector with incorrect old offsets can
still be repaired through unique quote/context matching.

Staleness is derived UI state, not an annotation lifecycle status. An open
change request can therefore also be stale.

Rendered DOM selection cannot be assumed to map directly to Markdown offsets.
The renderer should emit stable source-position metadata on eligible text
containers from goldmark AST segments. The browser sends source offsets and a
quoted selector, and the server verifies them against the current file before
persisting.

Review-mode rendering wraps eligible goldmark text segments with their start
and exclusive end byte offsets and exposes the complete document digest. The
browser converts the two DOM endpoints from UTF-16 offsets to UTF-8 byte
positions. The spans between those endpoints may contain formatting elements
and invisible Markdown delimiters. The browser sends only the range and digest;
the server verifies the source revision and derives the exact Markdown quote.
Endpoints whose rendered text cannot be mapped, such as entities, remain
ineligible. Single-line inline-code content is source-backed inside its
`<code>` element, so a selection can begin, end, or pass through it. Multiline
inline code remains ineligible because CommonMark normalizes its source
newlines to displayed spaces.

Each line of a fenced code block is source-backed, including its trailing
newline. A selection may span any number of lines when both endpoints are in
the same fenced block. The fence and info string are excluded, and selections
that cross into or out of the block are rejected. Indented code blocks remain a
follow-up because their rendered indentation does not map one-to-one to source.

A rendered Mermaid SVG is intentionally one annotation region rather than a
collection of selectable labels. In review mode the surrounding diagram stores
the byte range of the complete fenced definition, excluding the opening and
closing fences. Clicking the rendered diagram, or focusing it and pressing
Enter or Space, previews and submits that complete range. Active annotations
outline the whole rendered diagram. The collapsible source retains its ordinary
line-level ranges for reviewers who need to discuss only part of the diagram
definition.

## Persistence and concurrency

### Store behavior

The store opens one absolute, symlink-resolved writable root and mirrors each
validated Markdown path beneath it with a `.json` suffix. It creates directories
with mode `0700` and sidecar files with mode `0600`, subject to the process
umask. A missing sidecar loads as an empty schema for the requested document and
has an empty revision.

Every non-empty revision is the lowercase SHA-256 digest of the exact persisted
JSON bytes, including formatting and the trailing newline. A save supplies the
revision returned by its earlier load. While holding the store mutex, the store
reads the current bytes and rejects a mismatched revision with `ErrConflict`
before writing. The HTTP layer maps that error to `409 Conflict` and returns the
current revision so the caller can reload and reconcile.

Supported-schema rewrites preserve JSON fields unknown to this version. Unknown
fields are merged only into retained objects and list entries with matching
stable IDs. Known fields use the submitted values, and removed annotations,
thread entries, or optional objects are not resurrected. This merge protects
forward-compatible metadata; it is not a concurrent-edit merge. Revision
checking protects known fields and annotations added by another writer.

Writes use a temporary file in the destination directory. The store sets mode
`0600`, writes and syncs the complete JSON, closes it, and renames it over the
target so readers do not observe a partial document. On supported systems it
also syncs the parent directory to persist the rename. Document paths are
canonical slash-separated relative Markdown paths. Every existing directory
component and final sidecar is inspected without following symlinks, and the
resolved result must remain under the writable root.

The mutex coordinates operations through the same in-process `Store` instance.
It does not lock an external process that writes sidecars directly. Agents and
the browser should therefore mutate annotations through the webserver API or
future CLI store commands. Direct multi-process writes would require a shared
cross-process locking protocol; otherwise an external write can occur after the
revision check and before the rename.

### Optimistic-concurrency interaction

```text
Reviewer browser       Review API          Sidecar store          AI agent
       |                    |                    |                    |
       |-- GET annotations->|                    |                    |
       |                    |-- Load(document) ->|                    |
       |                    |<- sidecar + R1 ----|                    |
       |<- data + ETag R1 --|                    |                    |
       |                    |                    |                    |
       |                    |<---------- GET annotations -------------|
       |                    |-- Load(document) ->|                    |
       |                    |<- sidecar + R1 ----|                    |
       |                    |----------- data + ETag R1 ------------>|
       |                    |<---------- Save update with R1 ---------|
       |                    |-- Save(expected R1)->                   |
       |                    |                    | read current R1    |
       |                    |                    | atomic write       |
       |                    |<- revision R2 -----|                    |
       |                    |---------------- saved with R2 --------->|
       |                    |                    |                    |
       |-- Save with R1 --->|                    |                    |
       |                    |-- Save(expected R1)->                   |
       |                    |                    | read current R2    |
       |                    |<- ErrConflict + R2-|                    |
       |<- 409 + ETag R2 ---|                    |                    |
       |                    |                    |                    |
       |-- Reload --------->|-- Load(document) ->|                    |
       |                    |<- agent data + R2 -|                    |
       |<- data + ETag R2 --|                    |                    |
       |                    |                    |                    |
       |-- Save reconciled ->|                    |                    |
       |                    |-- Save(expected R2)->                   |
       |                    |                    | atomic write       |
       |                    |<- revision R3 -----|                    |
       |<- saved + ETag R3 -|                    |                    |
```

A client must never retry the same stale payload automatically because doing so
could discard the intervening agent change.

- Never modify the Markdown document as part of an annotation write.
- Never reuse unchecked URL paths at the annotation writable-root boundary.
- Never treat unknown-field preservation as resolution of a revision conflict.

## HTTP API

Mutation routes exist only in review mode.

| Method and route | Purpose |
| --- | --- |
| `GET /api/annotations?status={states}` | List the cross-document agent queue with one revision per sidecar. |
| `GET /api/annotations?document={path}` | List annotations and derived anchor state. |
| `POST /api/annotations` | Create a text or document annotation. |
| `PATCH /api/annotations/{id}` | Transition status or update allowed mutable fields. |
| `POST /api/annotations/{id}/replies` | Append an agent or reviewer thread entry. |
| `POST /api/annotations/{id}/reattach` | Attach a stale comment to a new selection. |

The first milestone should not permanently delete annotations. A mistaken
comment can transition to `rejected` with an explanation, preserving review
history and Git traceability.

Annotation creation requires the sidecar ETag returned by the preceding `GET`
in a strong `If-Match` header. The empty ETag (`If-Match: ""`) represents a
sidecar that does not exist yet. Missing preconditions return `428`; a stale
revision returns `409` with the current ETag so the client can reload instead of
overwriting another writer's annotations.

The create body contains `document`, `intent`, `comment`, `author`, and an
optional `selection` with `startByte`, exclusive `endByte`, and
`documentSHA256`. The server reads the current Markdown, rejects a stale digest,
then derives the exact source quote, context, and line range from those offsets.
Without `selection`, the
annotation applies to the document. The server owns the initial `open` status,
UTC timestamps, empty thread, and sortable collision-resistant `ann_` ID.
Unknown request fields and multiple JSON values are rejected.

The reply route also requires `If-Match` and accepts only `document`, `message`,
and `author`. The server assigns the `msg_` ID, UTC timestamp, and `reply` kind,
then appends the entry without changing the annotation status or any earlier
thread entry. Clients cannot use this route to manufacture `resolution`,
`review`, or `status_change` events; lifecycle endpoints create those events
atomically with their corresponding validated transition.

The transition route accepts `document`, target `status`, `actorRole`, `author`,
and transition-specific activity. It validates the actor and current state,
then appends the activity event followed by a `status_change` event in the same
optimistic sidecar save:

| Target status | Required activity |
| --- | --- |
| `acknowledged` | Server-owned `acknowledgement`; no message or summary. |
| `applied` | `resolution` with required `summary` and optional `commit`. |
| `needs_changes` | `review` with required reviewer `message`. |
| `rejected` | `reply` with a required rejection reason in `message`. |
| `closed` or reopened `open` | Status-change event only. |

This makes `applied -> needs_changes` atomic: the annotation remains active, the
reviewer's unsatisfied feedback stays inline, and an agent sees the complete
prior attempt on the next acknowledgement. Invalid actors, skipped states, and
activity fields that do not belong to the target status are rejected without a
write. Like other mutations, the request requires the current strong ETag.

Reattachment is available only for a text annotation whose selector currently
resolves to `stale`. The request supplies `document` and a replacement
`selection` with byte offsets and the rendered document digest. The server verifies the old stale
state, recreates the full selector from current Markdown, and atomically replaces
only `source` and `updatedAt`. The annotation ID, original comment, lifecycle
status, and complete thread remain unchanged. Document-level annotations and
selectors that still resolve as `exact` or `moved` return `409` rather than being
silently relocated. The prior selector remains available through repository
history rather than becoming a discussion event.

## Local write security

Loopback does not by itself make mutation endpoints safe: a malicious website
open in the same browser could attempt requests to a predictable local port.
The browser's same-origin policy generally prevents that site from reading the
viewer response, but it does not make every cross-origin write attempt
impossible. Review mode therefore uses two independent request checks.

### Origin and review token

There is no “origin token.” These are separate values with different purposes:

- The **origin** is the viewer's exact scheme, host, and selected port, such as
  `http://127.0.0.1:54321`. It is not secret. Browsers automatically attach it
  to a cross-origin mutation request in the `Origin` header. The server accepts
  only the origin captured after its loopback listener is bound.
- The **review token** is a secret containing 256 random bits, generated anew
  for every review-mode process. The server embeds its URL-safe representation
  in a `<meta name="md-viewer-review-token">` element on viewer pages. Trusted
  viewer JavaScript reads it and sends it in the `X-MD-Viewer-Token` header on
  every mutation.

The origin check rejects a request initiated by another website, even if that
site guesses the loopback port. The token proves that the caller could read the
viewer page; merely knowing the URL is insufficient. Requiring a custom header
also makes a cross-origin browser request non-simple and triggers a CORS
preflight. The viewer exposes neither permissive CORS headers nor mutation
`OPTIONS` routes, so an unrelated origin cannot obtain permission to send it.

The review token exists only in process memory and the generated page. It is
never printed, written to a sidecar, placed in the viewer URL, or reused after a
restart. Normal read-only mode creates no token and embeds no token metadata.
The annotation `GET` API does not require the token; it performs no write, and
the absence of CORS still prevents unrelated browser origins from reading it.

### Mutation request flow

```text
md-viewer process        Viewer page JavaScript        Mutation guard
        |                          |                          |
        |-- bind loopback port ----|                          |
        |-- generate review token  |                          |
        |-- serve page + token --->|                          |
        |                          |                          |
        |                          |-- POST/PATCH ----------->|
        |                          |   Origin: exact origin   |
        |                          |   X-MD-Viewer-Token: ... |
        |                          |   Content-Type: JSON     |
        |                          |                          |
        |                          |                    verify origin
        |                          |                    compare token
        |                          |                    enforce JSON
        |                          |                    limit to 64 KiB
        |                          |                          |
        |                          |<-- handler or rejection -|
```

The token comparison is constant-time. A missing or incorrect origin or token
returns `403 Forbidden`; a non-JSON content type returns `415 Unsupported Media
Type`. Mutation handlers must report an oversized body as `413 Request Entity
Too Large`. These checks happen before parsing annotation data or touching the
writable store.

The complete local-write boundary is:

- A cryptographically random session token generated at startup.
- The token embedded into the served review page and required in a custom header
  for every mutation.
- Strict `Origin` validation against the exact selected loopback origin.
- `application/json` request bodies and bounded request sizes.
- Existing root and symlink containment checks for document reads.
- Separate containment checks for the annotation write root.
- HTML escaping for comments; annotation Markdown rendering is out of scope.
- No permissive CORS headers.

Browser and live-agent mutations use the same guarded HTTP API. A local agent is
given the current viewer URL, reads the per-process review token from the served
page, and sends the exact origin, token, and sidecar revision on every mutation.
The token is never logged. Offline CLI commands remain available only when the
server is not coordinating an active review session.

## Offline annotation tooling

The CLI provides maintenance, inspection, and agent handoff only when the
review server is not running. Agents must not use offline reads or mutations
while a live server is coordinating browser review; doing so bypasses the one
HTTP revision flow intended to order browser and agent decisions. If server
state is unknown, the agent must ask before choosing this mode.

```sh
md-viewer annotations list --root ./docs --status open,needs_changes --format json
md-viewer annotations export --root ./docs --status open,needs_changes --format markdown
md-viewer annotations resolve --root ./docs --id ann_... \
  --status applied --role agent --author codex \
  --commit abc1234 --summary "Implemented request"
```

JSON output returns the schema objects without presentation text. Markdown
export groups annotations by document and includes intent, status, selected
text, line range, comment, staleness, and annotation ID. Stable IDs allow a
human to tell an agent, “process annotation `ann_...`,” without copying the full
comment.

The JSON `list` command is implemented first. It emits documents in content
index order, omits documents with no matching annotations, includes derived
anchor state, and supports comma-separated status filtering. It is read-only:
a missing annotation directory produces an empty list without creating storage.

Markdown `export` reuses that collected model and adds stable IDs, original
selector text and lines, derived anchor location or stale reason, comments, and
the complete thread. Source and comment blocks choose a backtick fence longer
than any run in their content, preserving arbitrary Markdown as data.

Offline `reply` locates one globally unique annotation ID across the current
content index, assigns a `msg_` ID and timestamp, and appends only an ordinary
discussion event. It preserves lifecycle status and saves with the revision
loaded during lookup so a concurrent sidecar edit is rejected rather than
overwritten.

Offline `resolve` uses the same domain transition builder as the HTTP API. It
requires an explicit `agent` or `reviewer` role plus author name, creates the
target-specific activity and status-change entries together, validates the
complete sidecar, and saves against the loaded revision.

## Live AI-agent handoff

The running webserver is the sole write coordinator for a live review. An agent
uses `GET /api/annotations?status=open,needs_changes` to discover work. Every
returned document includes its own `revision`; there is intentionally no queue
ETag spanning multiple sidecars. The agent uses the document revision in a
strong `If-Match` header for replies and transitions, alongside the exact
viewer origin and review token.

A live agent workflow is:

1. List unresolved `question`, `suggestion`, and `change_request` annotations.
2. Read each current document plus its quoted context.
3. Make in-scope changes and verify them.
4. Mark handled annotations `applied` with a summary and optional commit, or
   `rejected` with a reason when the request cannot or should not be performed.
5. If the reviewer responds with `needs_changes`, read the complete thread and
   make another attempt against the same annotation ID.
6. Leave final closure to the reviewer.
7. On `409`, reload the API queue and reconsider the action against the latest
   status and complete thread. Never retry a stale mutation blindly.

The Go application provides an `agent` HTTP-client command family so agents do
not reproduce authentication headers or JSON shapes by hand. It discovers the
token from the supplied loopback viewer URL and never opens or writes sidecars.
The repository skill instructs agents to use this client. The viewer must not
execute arbitrary agent commands.

## Proposed packages

```text
internal/annotation/       schema, validation, anchors, status transitions
internal/annotation/store/ sidecar paths, atomic persistence, concurrency
internal/server/           review APIs and origin/token middleware
internal/app/              --review and --annotations-dir configuration
internal/commands/         offline annotation list/export/resolve commands
web/                       selection, highlights, review panel, filters
```

## Implementation slices

1. Schema, validation, status transitions, and JSON fixtures.
2. Root-safe atomic sidecar store with optimistic concurrency.
3. Read-only annotation API and review-panel display.
4. Secure review mode and annotation creation from verified selections.
5. Threaded replies, `needs_changes`, stale-anchor display, and reattachment.
6. Offline CLI list and Markdown/JSON export.
7. Agent resolution command, integration tests, and documentation.

Each slice should be a separately reviewable commit with unit tests and a green
`go test ./...` result.

The initial panel is deliberately read-only: it fetches the active document's
annotation view, displays lifecycle and stale-anchor state, and renders all
user-controlled strings through DOM text nodes. Selection mapping, highlights,
and mutation controls follow as independent reviewable changes.

The creation form supports document-level and currently captured text
annotations. It submits the review token, the latest sidecar revision as a
strong `If-Match`, and the digest-bound selection when selected scope is active.
After a successful create it reloads the authoritative list and revision. A
document or sidecar conflict keeps the comment in the form and asks the reviewer
to refresh and select again.

Each annotation card previews its persisted exact source quote and original
line range. Stale annotations retain that original quote so the reviewer can see
what disappeared or became ambiguous. Document-level annotations show a
distinct whole-document label instead of an empty quote.

Exact and moved anchors are highlighted at their current derived byte ranges.
The browser converts UTF-8 source offsets back to DOM UTF-16 boundaries and uses
the CSS Highlight API when available. A merged `<mark>` fallback provides the
same visual result without creating invalid overlaps. Stale and document-level
annotations have no current text range and therefore are not highlighted.

### Annotation-to-source navigation

Selecting an annotation card navigates the document to the location associated
with that annotation. This remains useful after an agent marks a change
`applied`, when the reviewer needs to inspect the corresponding document edit:

1. For an `exact` or `moved` anchor, scroll the resolved highlight into the
   center of the document viewport and briefly emphasize it.
2. For a `stale` anchor, do not select an ambiguous quote or silently reattach
   the annotation. Use the original byte offset to find the nearest current
   source-backed element, scroll it into view, and label the result as an
   approximate location.
3. For a document-level annotation, scroll to the document heading or top; do
   not display source-target emphasis.
4. If the rendered document has no source-backed element, leave the scroll
   position unchanged and explain that no approximate location is available.

The card is keyboard activatable with Enter or Space. Pointer events from
buttons, links, form fields, status controls, and text selection inside the card
retain their existing behavior and do not trigger navigation. Navigation also
expands the selected card, applies programmatic focus without losing the
reviewer's place in the sidebar, and respects reduced-motion preferences.

The temporary emphasis is presentation only. It does not change annotation
state, persist a new anchor, or imply that an approximate stale location is
correct. Browser coverage includes exact, moved, not-found, ambiguous,
keyboard, nested-control, collapsed-sidebar, and reduced-motion cases.

Lifecycle controls appear within each annotation card and expose only valid
next states. The selected action determines the actor role sent to the server
and whether a message or resolution summary is required; an applied action may
also include a commit reference. Every update carries the review token and the
latest sidecar revision. The server still owns validation and atomic thread
creation. If another browser or agent writes first, the stale request receives
`409`; the browser reloads the authoritative annotations and requires the user
to review the latest state before retrying. Ordinary inline discussion replies
remain a separate control because they do not change lifecycle state.

The reply form records an author and message and posts them to the dedicated
reply endpoint with the review token and current sidecar revision. A successful
append reloads the complete authoritative thread. A concurrent-write conflict
reloads the latest thread before the reviewer retries, preventing an older
browser view from overwriting agent activity.

A stale text annotation displays a reattachment action that is disabled until
the browser has captured a valid current-document selection. All stale cards
share that selection, and the button on the chosen card identifies which
annotation to reattach. The request contains only the verified byte range and
document digest; the server reconstructs the selector and preserves the ID,
comment, thread, and lifecycle status. Success clears the captured selection.
A document or sidecar conflict reloads annotations and requires a fresh
selection so an outdated range cannot be applied silently.

Cards are collapsed by default to a status-and-comment summary and expand on
click to show source context, authorship, and thread history. Reply,
reattachment, and lifecycle forms sit inside a nested collapsed `Actions`
section. This keeps long threads and primarily agent-oriented operations out of
the scanning path without removing access to them.

An `applied` annotation is the exception to hiding lifecycle operations. Because
`Close` is the reviewer's common acceptance action, a compact `Close` button
appears beside the `Actions` toggle whenever `applied -> closed` is valid. It
does not require expanding the actions section. The expanded form retains
`Needs changes` and any contextual controls; it must not render a second Close
button when the quick action is present.

Quick Close uses the same reviewer transition endpoint, review token, current
sidecar revision, status-change thread entry, and `409` reload behavior as the
expanded lifecycle form. The button is disabled while its request is pending
and its accessible name includes enough annotation context to distinguish it
from other Close buttons. It is absent for every status where the transition is
invalid, including annotations already closed or rejected.

Closed and rejected annotations are complete or declined work, so the default
document view excludes both their cards and their source highlights. The panel
reports active and total counts and offers an explicit history toggle that
restores the inactive cards and highlights. This keeps completed work available
for inspection and reopening without implying that its source is still under
review.

## Acceptance criteria

- Normal viewer mode creates no files and exposes no mutation routes.
- Review mode can create a document-level or selected-text annotation without
  changing the Markdown file.
- Refreshing the browser restores highlights and comments from the sidecar.
- Moving selected text preserves the anchor when quote context is unique.
- Removed or ambiguous text produces an explicit stale annotation.
- Concurrent stale updates receive `409` instead of overwriting newer feedback.
- Requests without the correct origin and session token cannot mutate data.
- An agent can list and export open actions without starting the web server.
- An agent can report an applied change with a summary and commit reference.
- A reviewer can respond inline to an applied attempt, set `needs_changes`, and
  return the same annotation to the agent queue without losing prior attempts.
- Agent exports include the original request, every resolution attempt, and the
  latest reviewer reply.
- A reviewer, not the agent, controls final closure.
- Storage, API, UI, security, and CLI behavior have automated coverage.

## Risks and follow-ups

- Mapping rendered selections to Markdown source offsets is the highest-risk
  technical area and should be prototyped before polishing the panel.
- Quote selectors can expose reviewed text in committed sidecars; repositories
  containing secrets should keep annotation directories untracked or external.
- Git merges can conflict when two reviewers edit the same document sidecar.
  Per-annotation files could be reconsidered if this becomes common.
- Live reload should eventually refresh annotation anchor state as well as
  document content, which is why annotation semantics are designed first.
