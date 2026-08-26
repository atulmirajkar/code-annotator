# Architecture

## Overview

`code-annotator` is a local command-line application containing an HTTP server. A
user selects a directory at startup, and that directory becomes the immutable
content root for the lifetime of the process. The application indexes Markdown
and explicitly enabled source files, renders requested documents to safe HTML,
and serves referenced local assets.

The MVP is a single Go binary with embedded page templates, generated
TypeScript browser modules, and static styling. It has no database,
client-side framework, or external service.

## System flow

```text
code-annotator <directory>
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
catalog -> safe file lookup -> Markdown or source renderer -> HTML response
                               |
                               v
                         local asset requests
```

The listener is created before opening the browser. This guarantees that the
application knows the final address and avoids opening a URL before the server
can accept connections.

## Proposed package layout

```text
cmd/code-annotator/main.go       command-line parsing, process lifecycle, signals
internal/content/           directory indexing and root-contained file lookup
internal/annotation/        annotation schema, lifecycle, and anchor resolution
internal/annotation/store/  constrained atomic JSON sidecar persistence
internal/render/            goldmark configuration and page rendering
internal/gitdiff/           bounded Git commands, aligned diffs, revision state
internal/highlight/         bounded Tree-sitter grammar selection and syntax ranges
internal/server/            routes, handlers, HTTP server, graceful shutdown
internal/launch/            thin, testable wrapper around pkg/browser
internal/commands/          offline annotation tools and live HTTP agent client
web/                        authored TypeScript/Sass, generated browser assets, HTML, and vendor files
```

Package boundaries should remain small. In particular, `internal/content`
owns filesystem safety, while HTTP handlers consume its API rather than joining
untrusted URL paths themselves.

`internal/highlight` pins the pure-Go `gotreesitter` runtime and maps exact
normalized default extensions to 11 selectively embedded grammars. It returns
validated, presentation-neutral UTF-8 byte ranges and owns no mutable parser
state. Highlight work admits at most 128 KiB per pane, has a 500 ms deadline,
and rejects excessive or invalid output; larger supported source files retain
the existing escaped plain rendering. The server owns a runtime instance, and
submilestone 12.2 maps restored annotation ranges through descendant text nodes
while preserving nested token elements in the fallback highlighter. Submilestone
12.3 feeds validated core-language ranges into File-view `RenderCode`; the
renderer owns escaping and fixed capture-class mapping, while Changes view and
the remaining catalog stay on later review gates.
[`docs/designs/source-syntax-highlighting.md`](designs/source-syntax-highlighting.md).

The browser source is authored under `web/src/` and compiled into the checked-in
`web/generated/` directory with `npm run build:web`. Sass is organized under
`web/src/styles/` and emits `web/generated/styles.css`. The generated modules keep
the existing `/static/*.js` URLs and native ES-module import structure, while
`web/embed.go` embeds only the generated runtime assets. See
[`docs/designs/typescript-migration.md`](designs/typescript-migration.md) for
the frontend dependency graph, typing rules, and compiler configuration.

Review pages load the pinned, embedded HTMX 2.0.10 runtime under the existing
same-origin CSP. The initial page and replaceable annotation panel, card, and
action fragments are parsed from `web/templates/*.html` as one template set.
Presentation-specific Go view models derive active filtering, counts, source
labels, threads, stale-anchor state, and permitted lifecycle actions; action
authorization delegates to `internal/annotation` transition validation.
Typed, transport-neutral application operations now own catalog/source reads,
sidecar loading, anchor resolution, annotation creation, and optimistic saves.
The stable JSON API and active HTML form handlers call those same operations.
HTMX swaps complete authoritative panel fragments after reads and mutations;
expected `409` and `422` responses also swap without automatic retry. A typed
adapter supplies the review token and current strong revision and reruns only
browser-owned selection, highlighting, source navigation, lifecycle-field,
and panel behavior after swaps. The former browser API, card renderer, action
builder, DOM helper, and thread presentation modules have been removed. The
invariants, route plan, test strategy, documentation contract, and remaining
one-commit-at-a-time review gates are defined in
[`docs/designs/server-rendered-review-ui.md`](designs/server-rendered-review-ui.md).
This architecture document must be updated in each implementation commit so it
continues to describe the code that exists rather than the future target.

Application state must not gain new custom HTML `data-*` channels.
`GET /ui/viewer-state?document={path}&mode={file|diff}` is the active boundary
for review-page browser state: a versioned Go response reports document
identity, kind, digest, source-node and diagram ranges, optional review
revision, resolved annotation locations, and lifecycle behavior.
`web/src/viewer-state.ts` accepts the HTTP body as `unknown`, validates every
nested field and enum, rejects duplicate semantic IDs, and returns typed maps.
Source spans, diagrams, annotation cards, and lifecycle forms expose only
semantic IDs; selection, highlighting, navigation, Mermaid interaction, HTMX
revision headers, and lifecycle fields join those IDs to typed state. Authored
browser code has no remaining custom-data state allowlist. HTML retains
presentation, semantic IDs, accessibility
attributes, links, and normal form semantics—not application state encoded in
custom attributes.

Custom attributes are not the only state concern. The document sidebar no
longer treats rendered nodes as catalog state: Go builds the
recursive tree, filters it, renders counts and links, and serves replacement
fragments. Small adapters read only interaction values, focus targets,
semantic IDs, geometry, events, native selection, and rendered selection text.
Application decisions consume runtime-validated state or explicit interaction
values. Directory expansion is a typed browser-owned set of explicitly
collapsed IDs projected onto each replacement fragment; directories absent
from that set default to expanded when a filter first reveals them.

`GET /ui/document-state?document={optional path}&mode={file|diff}` is the active
versioned catalog boundary. An omitted path selects the index default,
matching `/`; its response contains selected identity, normalized mode,
changed-lookup availability/failure, review availability, and ordered document
records with kind, navigation URL, changed membership, and active-comment
count. `web/src/document-state.ts` runtime-validates the entire unknown payload
and indexes it by path. `web/src/document-catalog.ts` contains DOM-free tree,
filter, result-order, status-label, and summary rules.
`GET /ui/review/documents` renders recursive HTML from the same authoritative
inputs; the viewer replaces filtered fragments through HTMX after a 150 ms
debounce.

`viewer.ts`, `review.ts`, and `mermaid.ts` expose named initialization
functions. Browser entrypoints are thin composition roots; pure preference
rules live in `viewer-preferences.ts`. Typed annotation statuses determine the
display/highlight set, HTMX supplies an explicit mutation kind, and typed
diagram definitions plus ordered source maps replace node text/order as
application inputs. `viewer.ts` declares interfaces and helpers at module
scope. Its initializer performs wiring only, with storage, browser services,
and mutable interaction contexts passed explicitly to handlers. Inline
comments in the adapter document state ownership, delegated-event reasons,
request-serialization guarantees, and drag-lifecycle invariants. Browser
callbacks use idiomatic arrow adapters while behavior remains in named,
module-scoped helpers. Explicit directory collapses live in a typed set keyed
by semantic directory IDs; HTMX swaps receive that state through a one-way
render step instead of having classes or ARIA attributes read back as state.
The adapter projects state after both swap and class settlement so HTMX cannot
restore a server default over the tab-local preference. Unknown directories
default to expanded. This adapter lives independently in `document-tree.ts`.
`viewer.ts` composes it
with focused document-search, layout, comparison-control, diff-divider,
diff-overview, environment, and browser-storage modules; it owns no feature
implementation.

`diff-overview-geometry.ts` is the DOM-free rule layer for the diff overview
ruler. It accepts ordered numeric hunk ranges and measured content and track
bounds. It returns proportional marker sizes, collision-packed positions,
device-pixel density groups, a viewport projection, and the current or next
hunk identity. It performs no browser lookup or event binding and is included
in the pure-state architecture guard. `diff-overview.ts` is the focused DOM
adapter: it validates server fragment identities, measures their current-pane
ranges, discovers and tracks the computed vertical scroll owner, coalesces
scroll and resize work through an injected animation-frame port, and projects
the pure results into CSS. Invalid contracts retain ordinary fragment-link
navigation instead of partially activating the ruler.

`review.ts` uses a module-scoped `ReviewContext` rather than initializer-local
helpers. Its panel, selection, highlight, navigation, and HTMX controllers
receive browser services explicitly. Controller startup uses `start()`; browser
callbacks use arrow adapters, and no TypeScript callback uses `.bind(...)`.

Vitest runs co-located `web/src/**/*.test.ts` files and is deliberately scoped
away from the CommonJS Playwright suite in `browser-tests/`. The production
TypeScript configuration excludes test files from `web/generated/`, while
`tsconfig.test.json` typechecks both production and test sources. Pure browser
rules use the Node test environment; `happy-dom` is available only for simple
DOM adapter contracts and does not replace real-browser selection or layout
coverage.

`internal/server` owns the concurrency-safe active Git comparison base: a
single explicit commit behind a mutex, seeded at startup from the resolved
`--diff-base` commit. It never moves on its own. A browser selection request
validates its commit against a freshly listed bounded `internal/gitdiff`
option set, then swaps the base server-wide; handlers never pass browser
strings to Git. Every changed-path or file-diff operation reads one value copy
of the active base under the read lock, so a concurrent selection cannot alter
a base mid-request.

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

The `agent` command family is separate from offline `annotations` commands. It
accepts only a loopback HTTP viewer origin, reads the ephemeral review token
from the served page, and calls the live queue, reply, and transition endpoints.
It never opens content or sidecar storage. Mutation requests carry the exact
origin, token, and quoted document revision; `409` instructs the caller to
reload rather than retrying stale state.
Transition-entry construction lives in `internal/annotation` and is shared by
the HTTP handler and `annotations resolve`. The offline command locates the same
stable ID, applies actor-role validation, appends activity plus status change,
validates the complete sidecar, and saves optimistically.

Read-only mode never opens or creates annotation storage. With `--review`, the
application opens a separate symlink-resolved writable root. The default is
`<content-root>/.code-annotator/annotations/`; `--annotations-dir` selects an
alternate location and is invalid unless review mode is explicit. Mutation
routes will receive this store rather than constructing writable paths in HTTP
handlers.

After binding the loopback listener, review mode generates a 256-bit random
session token and binds it to the exact selected HTTP origin. The token is
embedded in review pages but is never printed or persisted. The shared mutation
guard requires the exact `Origin`, the token in `X-Code-Annotator-Token`, an
`application/json` content type, and a body no larger than 64 KiB. Mutation
routes are not registered until their handlers use this guard.

When `--diff-base` is configured, startup asks `internal/gitdiff` to discover
the containing worktree and resolve the requested local revision to a full
commit ID, and the server pins that commit as the initial active comparison
base. The server-rendered revision selector lets a reviewer re-pin the base
through form `POST /ui/review/git-comparison`; compatible automation can use
JSON `POST /api/git-comparison`. Both are guarded by the exact loopback
`Origin` and a distinct token in `X-Code-Annotator-Comparison-Token`; each accepts
only a commit present in a freshly listed bounded option set, never an
arbitrary revision string. Each page render obtains one value copy of the
active base, uses it for changed-path and file-diff operations, and exposes
the active name and commit as read-only page metadata. The document sidebar's
Changed-only filter composes with the path lookup and, before the reviewer
makes an explicit choice in the tab, defaults on when a changed document
exists. Discovery failure leaves the full catalog usable and is presented as
unavailable rather than as an empty changed set.

`internal/render` derives request-local overview hunks from the validated,
complete aligned row sequence. Each contiguous changed run becomes an added,
deleted, or modified hunk. The current pane identifies the range through its
existing first and last cells, and the renderer emits an ordered, accessible
link plus a non-focusable end reference for every hunk. This adds no persisted
diff state, JSON field, custom `data-*` channel, or row wrapper. The overview
navigation is a progressive-enhancement fallback. The browser adapter activates
sticky proportional positioning only after validating every semantic target;
the fourth grid column leaves both diff panes' independent horizontal scrolling
and the shared vertical scroll policy unchanged.

## HTTP routes

| Route | Purpose |
| --- | --- |
| `GET /` | Display the recursive, sorted reviewable document catalog. |
| `GET /view/{path}` | Safely load and render a cataloged Markdown or source file. |
| `GET /view/{path}?mode=diff` | Render a cataloged source file's side-by-side Changes view against the active comparison base. |
| `GET /asset/{path}` | Serve a document-relative local asset. |
| `GET /healthz` | Return a minimal readiness response for tests and tooling. |
| `GET /api/annotations?status={states}` | Return the cross-document agent queue with a revision per sidecar. |
| `GET /api/annotations?document={path}` | In review mode, return annotations, current anchor state, and revision. |
| `POST /api/annotations` | In a secured review session, create a verified text or document annotation. |
| `PATCH /api/annotations/{id}` | Atomically transition lifecycle state and append its structured activity. |
| `POST /api/annotations/{id}/replies` | Append an ordinary discussion reply without changing lifecycle state. |
| `POST /api/annotations/{id}/reattach` | Replace a stale text anchor with a server-verified current selection. |
| `GET /api/git-comparison` | When Git comparison is configured, return the active base identity and freshly listed bounded selector options. |
| `POST /api/git-comparison` | Re-pin the active base to a commit from the current bounded option listing. |

Unknown resources return `404`. Unsupported methods return `405`. Internal
filesystem paths and raw errors are not returned to the browser.

The annotation JSON read route is registered whenever annotation storage is
configured. The HTML read/create routes require a writable review session.
Without a `document` query it traverses the stable content index, applies an
optional status filter, and returns only documents with matching annotations.
Each document carries its own sidecar revision for subsequent mutations.
It verifies that the requested document is in the configured reviewable catalog,
loads its sidecar from the separate writable root, resolves text anchors against
the current file bytes, and returns the revision in both JSON and `ETag` form.
Review-mode rendering adds source byte ranges to eligible goldmark text
segments and binds them to the document digest. The browser maps endpoints from
DOM UTF-16 offsets to Markdown UTF-8 byte offsets, including across formatting
elements; normal viewer output is unchanged.
Source rendering uses the same contract: escaped line content is wrapped in
source-backed spans, while line terminators remain byte gaps. This permits exact
code selections without allowing annotation APIs to address excluded files or
non-document assets.
Single-line inline code receives the same endpoint metadata around its content;
its backtick delimiters remain an intervening source gap derived by the server.
Fenced code receives one source-backed span per content line. Browser mapping
requires both endpoints to share that block and never includes its fences.
The creation route requires the session security checks and a strong `If-Match`
sidecar ETag. It recreates source selectors from current document bytes rather
than trusting hashes or context supplied by the browser. JSON creation retains
its `201`, `Location`, JSON body, and ETag contract. The active form route
accepts only URL-encoded bodies under the same 64 KiB limit and returns the
complete annotation panel with `200` after the shared operation succeeds.
The browser creation form uses the revision from its latest annotation read,
preserves a captured selection while focus moves into the panel, and reloads
the authoritative list after a successful write. If the document changes
before creation, the server saves the comment as a stale selection awaiting
reattachment without trusting its old offsets. Sidecar revision conflicts
retain the draft comment and require the latest annotation state.
Annotation cards render the persisted selector quote and line range with DOM
text nodes. This preserves the original review context even when the separately
derived current anchor state is stale.
The same API response drives document highlights: exact and moved anchor byte
ranges are converted back to DOM boundaries, with overlapping fallback ranges
merged before markup is introduced. Stale and document-level records remain
panel-only.

Each annotation card derives its available lifecycle controls from the current
status. The chosen transition supplies the required role and determines
whether the form must collect a resolution summary or review message. Mutations
send the review token and the latest sidecar revision to the shared transition
endpoint. On a revision conflict, the browser reloads the authoritative list
and asks the user to review the new state before retrying.

Reply and lifecycle mutations now have transport-neutral application
operations shared by the stable JSON API and active server-rendered form
routes. The form routes return the complete authoritative annotation panel:
successful mutations use `200`, domain validation uses `422`, and revision
conflicts use `409` with the current ETag. Expected error fragments preserve
submitted reply or transition values through escaped template fields and never
retry a stale mutation automatically. HTMX swaps these fragments in place.

Reattachment uses the same transport-neutral pattern. The shared operation
accepts only typed current-document byte offsets, verifies the document digest,
requires an existing stale or pending selector, rebuilds the source, and saves
optimistically. The active form route safely parses its hidden selection
fields and returns the authoritative panel for success, `422` range errors,
semantic `409` state/document conflicts, and sidecar revision conflicts.
Obsolete selections are cleared after semantic conflicts; a selection that is
still valid after only a sidecar revision conflict remains escaped in the form
for an explicit retry. Creation errors now follow the same fragment contract.

The server treats `closed` and `rejected` as inactive presentation states.
They remain in the API response and sidecar, but the default panel fragment
filters out their cards. A history-toggle HTMX request renders all annotations,
preserving access to audit history and reopen transitions; the browser
highlighter consumes only location metadata on the returned visible cards.

Annotation cards use native disclosure controls: the default summary contains
status badges and a two-line comment preview, while source context, discussion,
and role attribution appear after expansion. Mutation forms live under a second
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

Both sidebars are independently collapsible, and the CSS grid assigns their
released width to the document rather than retaining empty columns. The
document sidebar starts visible and the annotation sidebar starts collapsed;
either default is overridden for the rest of the tab once the reviewer
explicitly toggles it. Document lookup filters the server-rendered relative
paths in the browser, so it adds no filesystem or HTTP search surface.
Matching is case-insensitive; keyboard controls focus lookup, move to results,
clear a query, and open the first match.

A cataloged source file's Changes view renders the base and current text in
independently horizontally scrollable panes, with a draggable, keyboard-
resizable divider between them; the chosen split is a tab-scoped preference.
The File/Changes toolbar and revision selector are sticky beneath the topbar
so they remain visible while scrolling a long file.

Fenced `mermaid` blocks load the embedded Mermaid Tiny bundle only on pages that
need it. Mermaid runs with strict security and a bounded input size, and no
diagram asset is fetched at runtime. The default Content Security Policy allows
styles and scripts only from the viewer origin. Because Mermaid emits scoped
SVG style elements and style attributes, diagram pages additionally permit
inline CSS; scripts remain self-only and Markdown raw HTML remains disabled.

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

Annotation review and AI handoff, Mermaid diagram rendering, and code review
with Git diff comparison are implemented. Live reload, syntax highlighting,
search, table-of-contents generation, raw HTML, and non-loopback listening
remain deferred. Any future network-sharing option must be explicit and should
include a separate security review.
