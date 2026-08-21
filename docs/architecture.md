# Architecture

## Overview

`md-viewer` is a local command-line application containing an HTTP server. A
user selects a directory at startup, and that directory becomes the immutable
content root for the lifetime of the process. The application indexes Markdown
files, renders requested documents to HTML, and serves referenced local assets.

The MVP is a single Go binary with embedded page templates and static styling.
It has no database, client-side framework, or external service.

## System flow

```text
md-viewer <directory>
        |
        v
validate and resolve content root
        |
        v
listen on 127.0.0.1:<selected port>
        |------------------------------|
        v                              v
serve HTTP requests          browser.OpenURL(server URL)
        |
        v
index -> safe file lookup -> Markdown renderer -> HTML response
                               |
                               v
                         local asset requests
```

The listener is created before opening the browser. This guarantees that the
application knows the final address and avoids opening a URL before the server
can accept connections.

## Proposed package layout

```text
cmd/md-viewer/main.go       command-line parsing, process lifecycle, signals
internal/content/           directory indexing and root-contained file lookup
internal/annotation/        annotation schema, lifecycle, and anchor resolution
internal/annotation/store/  constrained atomic JSON sidecar persistence
internal/render/            goldmark configuration and page rendering
internal/server/            routes, handlers, HTTP server, graceful shutdown
internal/launch/            thin, testable wrapper around pkg/browser
internal/commands/          offline annotation list and export workflows
web/                        embedded HTML, CSS, and review-panel JavaScript
```

Package boundaries should remain small. In particular, `internal/content`
owns filesystem safety, while HTTP handlers consume its API rather than joining
untrusted URL paths themselves.

The `annotations` command family is dispatched before server flag parsing and
never binds a listener or launches a browser. `annotations list` walks the
content index in stable order, loads matching sidecars directly, derives current
anchor state, and writes deterministic JSON. It checks for storage before
opening the store so a read against a missing annotation root creates nothing.
`annotations export` uses the same collected model and status filtering, then
formats it as deterministic Markdown. Arbitrary source and comment text is
placed in dynamically sized code fences so embedded backticks cannot corrupt
the handoff structure.
`annotations reply` is the first offline mutation. It searches the stable
content index for one globally unique annotation ID, appends a validated
ordinary reply, validates the complete sidecar, and saves against the revision
loaded during lookup. Missing storage is an error rather than an instruction to
create a new annotation root.
Transition-entry construction lives in `internal/annotation` and is shared by
the HTTP handler and `annotations resolve`. The offline command locates the same
stable ID, applies actor-role validation, appends activity plus status change,
validates the complete sidecar, and saves optimistically.

Read-only mode never opens or creates annotation storage. With `--review`, the
application opens a separate symlink-resolved writable root. The default is
`<content-root>/.md-viewer/annotations/`; `--annotations-dir` selects an
alternate location and is invalid unless review mode is explicit. Mutation
routes will receive this store rather than constructing writable paths in HTTP
handlers.

After binding the loopback listener, review mode generates a 256-bit random
session token and binds it to the exact selected HTTP origin. The token is
embedded in review pages but is never printed or persisted. The shared mutation
guard requires the exact `Origin`, the token in `X-MD-Viewer-Token`, an
`application/json` content type, and a body no larger than 64 KiB. Mutation
routes are not registered until their handlers use this guard.

## HTTP routes

| Route | Purpose |
| --- | --- |
| `GET /` | Display a recursive, sorted tree of Markdown documents. |
| `GET /view/{path}` | Safely load and render a `.md` file. |
| `GET /asset/{path}` | Serve a document-relative local asset. |
| `GET /healthz` | Return a minimal readiness response for tests and tooling. |
| `GET /api/annotations?status={states}` | Return the cross-document agent queue with a revision per sidecar. |
| `GET /api/annotations?document={path}` | In review mode, return annotations, current anchor state, and revision. |
| `POST /api/annotations` | In a secured review session, create a verified text or document annotation. |
| `PATCH /api/annotations/{id}` | Atomically transition lifecycle state and append its structured activity. |
| `POST /api/annotations/{id}/replies` | Append an ordinary discussion reply without changing lifecycle state. |
| `POST /api/annotations/{id}/reattach` | Replace a stale text anchor with a server-verified current selection. |

Unknown resources return `404`. Unsupported methods return `405`. Internal
filesystem paths and raw errors are not returned to the browser.

The annotation read route is registered only when `--review` supplies a store.
Without a `document` query it traverses the stable content index, applies an
optional status filter, and returns only documents with matching annotations.
Each document carries its own sidecar revision for subsequent mutations.
It verifies that the requested Markdown document exists under the content root,
loads its sidecar from the separate writable root, resolves text anchors against
the current file bytes, and returns the revision in both JSON and `ETag` form.
Review-mode rendering adds source byte ranges to eligible goldmark text
segments and binds them to the document digest. The browser maps endpoints from
DOM UTF-16 offsets to Markdown UTF-8 byte offsets, including across formatting
elements; normal viewer output is unchanged.
Single-line inline code receives the same endpoint metadata around its content;
its backtick delimiters remain an intervening source gap derived by the server.
Fenced code receives one source-backed span per content line. Browser mapping
requires both endpoints to share that block and never includes its fences.
The creation route requires the session security checks and a strong `If-Match`
sidecar ETag. It recreates source selectors from current Markdown bytes rather
than trusting hashes or context supplied by the browser.
The browser creation form uses the revision from its latest annotation read,
preserves a captured selection while focus moves into the panel, and reloads
the authoritative list after a successful write. Conflicts retain the draft
comment and require a refreshed document selection.
Annotation cards render the persisted selector quote and line range with DOM
text nodes. This preserves the original review context even when the separately
derived current anchor state is stale.
The same API response drives document highlights: exact and moved anchor byte
ranges are converted back to DOM boundaries, with overlapping fallback ranges
merged before markup is introduced. Stale and document-level records remain
panel-only.

Each annotation card derives its available lifecycle controls from the current
status. The chosen transition supplies the required actor role and determines
whether the form must collect a resolution summary or review message. Mutations
send the review token and the latest sidecar revision to the shared transition
endpoint. On a revision conflict, the browser reloads the authoritative list
and asks the user to review the new state before retrying.

The browser treats `closed` and `rejected` as inactive presentation states.
They remain in the API response and sidecar, but the default panel filters out
their cards before rendering and passes only active annotations to the highlight
renderer. A history toggle rerenders the same authoritative payload with all
annotations, preserving access to audit history and reopen transitions.

Annotation cards use native disclosure controls: the default summary contains
status badges and a two-line comment preview, while source context, discussion,
and author details appear after expansion. Mutation forms live under a second
collapsed `Actions` disclosure so infrequent agent and lifecycle operations do
not dominate routine document review.

The reply route uses the same security and concurrency checks. It accepts only
ordinary reply content; the server owns thread IDs, timestamps, and kinds, and
preserves existing entries. Structured lifecycle activity is reserved for the
transition route so discussion cannot bypass status validation. The browser
presents replies as a separate form on each card, sends the latest sidecar
revision, and reloads the authoritative thread after a successful append. A
conflict reloads the latest thread before another attempt.

Lifecycle transitions are actor-controlled by the annotation domain model. The
handler creates any required acknowledgement, resolution, review, or rejection
activity and then a `status_change` entry before one atomic save. In particular,
returning applied work to `needs_changes` cannot change status without retaining
the reviewer's required message.

Reattachment first derives the old anchor against the current document and
accepts only `stale` text annotations. It verifies the replacement document
digest and byte range, regenerates the quote and context, and changes no review content,
thread history, or lifecycle state. In the browser, every visible stale card
shares the current verified document selection but has its own reattach action,
making the target annotation explicit. Success clears the selection and reloads
the authoritative annotation view; conflicts require a fresh selection.

## Key dependencies

### Markdown rendering

Use `github.com/yuin/goldmark` with GitHub Flavored Markdown extensions for
tables, task lists, strikethrough, and autolinks. Use version `v1.7.17` or newer
and retain its safe defaults:

- Raw HTML is disabled in the MVP.
- Potentially dangerous link destinations are not rendered.
- Markdown output is inserted only into the trusted application template.

### Browser launch

Use `github.com/pkg/browser` and call `browser.OpenURL` only after the listener
has been created. A thin wrapper allows tests to substitute the launch function.

Browser-launch failure is non-fatal: the server continues running and prints
the URL with a useful warning. The `--no-open` flag bypasses browser launch for
headless environments, SSH sessions, and scripts.

## Filesystem security

Every content or asset request must remain inside the startup directory.

1. Convert the supplied root to an absolute, symlink-resolved directory during
   startup.
2. URL-decode and clean requested relative paths.
3. Reject absolute paths and any lexical traversal outside the root.
4. Resolve existing symlinks and verify the result is still under the root.
5. Restrict rendered documents to the `.md` extension.
6. Apply a configurable internal file-size limit before reading a document into
   memory.

Containment should be checked with path-aware operations such as `filepath.Rel`,
not string-prefix comparison. Asset handlers must not expose directory listings.

## Network and lifecycle

- Bind to `127.0.0.1` by default, never all interfaces implicitly.
- Use port `0` by default so the operating system selects an available port.
- Permit a fixed port through `--port`.
- Configure HTTP read-header and idle timeouts.
- Handle `SIGINT` and `SIGTERM` with bounded graceful shutdown.
- Log the selected content root and URL to the terminal.

## Rendering and navigation

The index prefers `README.md` as the initial document when it exists. Other
Markdown files are presented in a stable, case-insensitive sort order. Hidden
files and directories are omitted. The page template provides a navigation
sidebar and a readable document pane; on desktop, the sidebar remains visible
and scrolls independently from the document.

Relative Markdown links to other `.md` files should resolve to `/view/` routes.
Relative image and asset references should resolve to `/asset/` routes relative
to the current document's directory. External `http` and `https` links remain
external.

## Testing strategy

- Unit-test root containment, including traversal, URL-encoded traversal, and
  symlinks escaping the root.
- Unit-test Markdown rendering and relative-link rewriting.
- Test handlers with `httptest` and temporary content directories.
- Inject browser launch to verify the final URL and non-fatal failure behavior.
- Run an integration test against an OS-selected loopback port.
- Run `go test ./...`, `go vet ./...`, and `go test -race ./...` before release.

## Deferred capabilities

Annotation review and AI handoff are designed as the next milestone. Live
reload, syntax highlighting, search, table-of-contents generation, raw HTML, and
non-loopback listening remain deferred. Any future network-sharing option must
be explicit and should include a separate security review.
