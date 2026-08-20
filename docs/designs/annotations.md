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

Anchor resolution proceeds in this order:

1. If the digest matches, verify the text at the stored byte range.
2. Otherwise, find an exact text match with matching prefix and suffix.
3. If there is one exact match without full context, attach with a `moved`
   warning.
4. If matches are ambiguous or absent, mark the annotation `stale` and show it
   only in the review panel until the reviewer reattaches it.

Staleness is derived UI state, not an annotation lifecycle status. An open
change request can therefore also be stale.

Rendered DOM selection cannot be assumed to map directly to Markdown offsets.
The renderer should emit stable source-position metadata on eligible text
containers from goldmark AST segments. The browser sends source offsets and a
quoted selector, and the server verifies them against the current file before
persisting.

## Persistence and concurrency

- Serialize writes per sidecar within the process.
- Write a complete sidecar to a temporary file in the destination directory,
  sync it, and atomically rename it over the prior version.
- Create annotation directories with user-only write intent and ordinary
  repository-readable files.
- Include a sidecar revision or ETag on reads and require it on mutations.
- Return `409 Conflict` when a browser attempts to update an old revision.
- Never modify the Markdown document as part of an annotation write.
- Validate all annotation and document paths through a dedicated writable-root
  boundary; do not reuse unchecked URL paths.

## HTTP API

Mutation routes exist only in review mode.

| Method and route | Purpose |
| --- | --- |
| `GET /api/annotations?document={path}` | List annotations and derived anchor state. |
| `POST /api/annotations` | Create a text or document annotation. |
| `PATCH /api/annotations/{id}` | Transition status or update allowed mutable fields. |
| `POST /api/annotations/{id}/replies` | Append an agent or reviewer thread entry. |
| `POST /api/annotations/{id}/reattach` | Attach a stale comment to a new selection. |

The first milestone should not permanently delete annotations. A mistaken
comment can transition to `rejected` with an explanation, preserving review
history and Git traceability.

## Local write security

Loopback does not by itself make mutation endpoints safe: a malicious website
could attempt requests to a predictable local port. Review mode must add:

- A cryptographically random session token generated at startup.
- The token embedded into the served review page and required in a custom header
  for every mutation.
- Strict `Origin` validation against the exact selected loopback origin.
- `application/json` request bodies and bounded request sizes.
- Existing root and symlink containment checks for document reads.
- Separate containment checks for the annotation write root.
- HTML escaping for comments; annotation Markdown rendering is out of scope.
- No permissive CORS headers.

The token should not be printed in terminal output or stored in sidecars.

## AI-agent handoff

Sidecars are the canonical machine-readable interface. Add a CLI surface that
does not require the server to be running:

```sh
md-viewer annotations list --root ./docs --status open,needs_changes --format json
md-viewer annotations export --root ./docs --status open,needs_changes --format markdown
md-viewer annotations resolve --root ./docs --id ann_... \
  --status applied --actor codex --commit abc1234 --summary "Implemented request"
```

JSON output returns the schema objects without presentation text. Markdown
export groups annotations by document and includes intent, status, selected
text, line range, comment, staleness, and annotation ID. Stable IDs allow a
human to tell an agent, “process annotation `ann_...`,” without copying the full
comment.

An agent workflow is:

1. List unresolved `question`, `suggestion`, and `change_request` annotations.
2. Read each current document plus its quoted context.
3. Make in-scope changes and verify them.
4. Mark handled annotations `applied` with a summary and optional commit, or
   `rejected` with a reason when the request cannot or should not be performed.
5. If the reviewer responds with `needs_changes`, read the complete thread and
   make another attempt against the same annotation ID.
6. Leave final closure to the reviewer.

An MCP adapter or automatic watcher can be designed later over the same sidecar
and CLI contracts. The viewer must not execute arbitrary agent commands.

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
