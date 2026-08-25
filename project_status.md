# Project status

Last updated: 2026-08-24

## Current state

**Phase:** Server-rendered review UI commits 8A, 8B, 9, and 10 are implemented
and ready for the requested combined review.
The approved design and ordered review gates are documented in
[`docs/designs/server-rendered-review-ui.md`](docs/designs/server-rendered-review-ui.md).
HTMX 2.0.10 is now pinned, licensed, embedded, and served from
`/static/htmx.min.js`, with provenance and checksums under `web/vendor/htmx/`.
Review pages load it with evaluation, script insertion, nested out-of-band
swaps, cross-origin requests, and history caching disabled. Review-mode servers
provide the active HTML annotation mutation surface. Typed
application operations share catalog/source reads, sidecar loading, anchor
resolution, creation, replies, lifecycle transitions, reattachment, and
optimistic saves between those routes and the unchanged JSON API. The complete
page and annotation panel, card, and action
fragments form one parsed template set. Presentation view models derive
filtering, counts, threads, source/anchor display, and lifecycle action
availability without request or store dependencies. Initial annotation HTML
and every read or mutation now use those templates through HTMX. TypeScript is
limited to authenticated transport configuration and browser-owned selection,
highlight, navigation, panel, and lifecycle-field adapters; the superseded API,
DOM renderer, action builder, and thread presentation modules are removed.
Commit 7 and review follow-ups A and B are approved. Follow-up B activates
semantic IDs plus runtime-validated TypeScript state for annotation cards,
source spans, Mermaid diagrams, lifecycle behavior, revisions, document digests, selection,
and navigation. `/ui/viewer-state` is fetched during initialization and after
mutations. A shrinking per-file test baseline rejects new `data-*` attributes
or dataset consumers; only document-tree/filter and comparison state remains.
Follow-up C records that DOM structure is not an acceptable replacement state
channel, inventories remaining violations, and splits typed document state from
document-tree activation.
Commit 8A added `/ui/document-state`, runtime validation, pure DOM-free
tree/filter/count rules, focused tests, and an architecture guard. Commit 8B
activates those contracts: Go renders recursive document fragments and counts,
HTMX replaces filtered panels after a 150 ms debounce, and TypeScript no longer
reconstructs catalog state from list nodes. The 5,000-path benchmark measured
4.08 ms/op against a 50 ms threshold. Commit 9 server-renders the bounded
comparison selector and form mutation, keeps the JSON API plus a runtime
validator, and removes the final custom-data state attribute.
Commit 10 replaces import-time IIFEs with named initializers, adds injected
entrypoint and pure-preference tests, and replaces active-link, visible-card,
mutation-class, conflict-text, Mermaid-source, and source-order inference with
typed state or explicit interaction inputs. Its viewer cleanup follow-up moves
every interface and helper to module scope and passes initializer-owned state
to event handlers through explicit typed contexts. A readability follow-up
documents the non-obvious state ownership and interaction invariants inline
and uses an idiomatic observer callback while keeping the update operation
separate. All viewer callback adapters now use the same arrow-function style;
the underlying interaction operations remain named module-level functions.
Directory expansion now has one typed ID-set source of truth that is rendered
onto initial and HTMX-replaced directory elements. Its listener, persistence,
and render lifecycle is isolated and directly tested in `document-tree.ts`.
The remaining viewer behavior is split by responsibility into document-search,
layout, comparison-control, diff-divider, environment, and storage modules;
`viewer.ts` now contains only security configuration and composition.
The review initializer is flattened around a typed context, all review browser
ports are explicit, and callback adapters consistently use arrow functions
without `.bind(...)`.
Every milestone 17 commit must update
the affected README, build, architecture, design, and status documentation in
the same commit, then stop for maintainer approval before the next gate.
Reviewer lifecycle permissions include dismissing an irrelevant or accidental
open annotation directly to `closed`; agents cannot perform that transition.
Annotations and thread entries now use one required `agent` or `reviewer` role
for both attribution and lifecycle permissions. New writes use sidecar schema
version 2. Schema version 1 remains readable and is migrated on the next
successful optimistic save; free-text author controls and CLI flags are gone.

The earlier TypeScript frontend migration is implemented and verified.
The authored browser modules now compile under strict TypeScript into
checked-in generated assets while preserving the existing Go `go:embed`
workflow and `/static/*.js` runtime URLs. The full Playwright suite passed with
Microsoft Edge when that phase closed. The long-file navigation regression now
scrolls the desktop document pane, which is the actual scroll container,
instead of asserting on
the window scroll position. The shared sticky topbar now uses an opaque
background, publishes its measured height to responsive sticky offsets, and
the document pane no longer has top padding above its sticky tabs, so document
content cannot appear above them in either File or Changes view.
Sass now owns the stylesheet source and emits the embedded CSS asset through
the same frontend build pipeline.

The code review and Git diff milestone is complete, including
diff-specific narrow-layout and light/dark theme browser coverage. Stopped
for maintainer review before live reload begins.

The manual-refresh MVP, annotation review workflow, and code review with
side-by-side Git diff comparison are implemented, including selection mapping,
highlights, secured browser mutations, live API agent handoff, a
repository-owned agent skill, a re-pinnable revision selector, and a
draggable, keyboard-resizable diff divider. Focused browser interaction
coverage and embedded diagram rendering are complete. User-facing
documentation and distribution binaries are refreshed for the current feature
set. Closing out milestone 10 also fixed three narrow-viewport CSS bugs in the
diff view that had no prior browser coverage: an unshrinkable
revision-selector dropdown, a media-query specificity gap that kept the
desktop multi-column layout active on phone widths whenever the documents
sidebar was visible in review mode, and a missing `min-width: 0` on the
sidebar itself that let a long document list widen the whole page once
enough files were cataloged. Live reload remains deferred until release
verification is reviewed.

## Milestones

### 1. Product and technical design

- [x] Define MVP goals and exclusions.
- [x] Define CLI behavior and browser-launch approach.
- [x] Define HTTP routes and package boundaries.
- [x] Define filesystem containment and Markdown security requirements.
- [x] Document build, run, and verification workflow.

### 2. CLI and process lifecycle

- [x] Add `cmd/code-annotator` entry point.
- [x] Parse the directory argument, `--port`, and `--no-open`.
- [x] Validate and resolve the content root.
- [x] Bind an OS-selected or explicit loopback port.
- [x] Add signal handling and graceful shutdown.

### 3. Content and rendering

- [x] Implement recursive Markdown discovery and stable ordering.
- [x] Implement traversal- and symlink-safe content lookup.
- [x] Configure goldmark with GFM extensions and safe defaults.
- [x] Rewrite document-relative Markdown links and asset references.
- [x] Add embedded HTML templates and responsive styling.

### 4. HTTP server and browser integration

- [x] Implement index, view, asset, and health handlers.
- [x] Add security headers, timeouts, and useful error responses.
- [x] Integrate `github.com/pkg/browser` through a testable launch adapter.
- [x] Keep the server alive and print the URL when browser launch fails.

### 5. Verification and release readiness

- [x] Add unit tests for path containment, indexing, and rendering.
- [x] Add handler and lifecycle integration tests.
- [x] Verify relative links and assets on nested documents.
- [x] Run `go test ./...`, `go vet ./...`, and `go test -race ./...`.
- [x] Verify native, Windows amd64, and Linux amd64 builds.
- [ ] Prepare the first tagged MVP release.

### 6. Annotation review and AI handoff

- [x] Define the annotation product goals and trust boundary.
- [x] Design the versioned sidecar JSON schema and source anchoring model.
- [x] Design the selection, comment panel, and annotation status workflow.
- [x] Design threaded replies and the `needs_changes` agent retry loop.
- [x] Define CLI export commands for AI-agent consumption.
- [x] Add an explicit writable review mode and annotation directory handling.
- [x] Implement atomic sidecar persistence and stale-anchor detection.
- [x] Implement annotation HTTP APIs with origin and session-token protection.
- [x] Add rendered-text selection, highlights, and the review panel.
- [x] Add annotation list, export, reply, and resolve CLI commands.
- [x] Add actor-aware annotation lifecycle controls to the browser panel.
- [x] Hide closed and rejected cards and highlights behind a history toggle.
- [x] Add ordinary inline discussion replies to annotation cards.
- [x] Add browser controls for reattaching stale text anchors.
- [x] Collapse annotation details and infrequent actions by default.
- [x] Add storage, API, security, and agent-handoff tests.
- [x] Add focused browser UI interaction tests.

### 7. Mermaid and sequence diagrams

- [x] Pin Mermaid Tiny and retain its license in the embedded web assets.
- [x] Bundle Mermaid at build time with no runtime Node.js, Chromium, CDN, or
  network dependency.
- [x] Recognize fenced `mermaid` blocks and render them as client-side SVG.
- [x] Use strict Mermaid security, bounded diagram input, and self-only scripts;
  permit generated inline SVG styles only on pages containing Mermaid.
- [x] Preserve the source code block and show a useful message when diagram
  parsing or rendering fails.
- [x] Add responsive diagram styling and viewer-theme integration.
- [x] Treat a rendered diagram as one source-backed annotation region rather
  than mapping individual SVG labels to Markdown offsets.
- [x] Add sequence-diagram, malformed-input, CSP, offline-asset, and annotation
  integration tests.

### 8. Viewer navigation polish

- [x] Make the document and annotation sidebars independently collapsible.
- [x] Add case-insensitive document-path lookup in the document sidebar.
- [x] Add browser coverage for panel collapse, lookup, and navigation.

### 9. Annotation-to-source navigation

- [x] Scroll exact and moved annotation cards to their resolved highlights.
- [x] Scroll stale annotations to the nearest source-backed position around
  their original byte offset and identify the result as approximate.
- [x] Scroll document-level annotations to the document heading or top.
- [x] Add keyboard activation, focus handling, and temporary target emphasis.
- [x] Keep nested card controls and text selection from triggering navigation.
- [x] Add a reviewer Quick Close button beside Actions for applied annotations.
- [x] Add browser coverage for resolved, stale, collapsed-panel, and
  reduced-motion behavior, plus Quick Close success and conflict handling.

### 10. Code review and Git diff

- [x] Define the proposed current-file code review and diff architecture.
- [x] Approve code discovery extensions and default excluded directories.
- [x] Approve current-side-only annotations and read-only deleted rows.
- [x] Approve immutable startup Git base semantics and annotation context.
- [x] Implement safe code rendering and reuse annotation workflows.
- [x] Implement bounded Git comparison, strict unified-patch alignment, and
  side-by-side current-file diff presentation.
- [x] Add a changed-only document filter backed by the configured Git base.
- [x] Extend agent handoff and browser coverage to code review.
- [x] Keep base and current code in independent horizontal scroll panes so long
  lines cannot cross the divider or obscure change highlighting.
- [x] Cover current-side diff annotation creation, restored highlights, and
  annotation-to-source navigation in the browser.
- [x] Cover current-side multi-line selection and reject base-side and
  cross-pane selections in the browser.
- [x] Exclude line-number gutters and diff markers from multi-line selection
  previews while preserving source blank lines.
- [x] Support selection endpoints on empty current-file lines using zero-length
  source anchors in File and Changes views.
- [x] Preserve the reviewer-selected File/Changes mode across code-document
  navigation without applying diff mode to Markdown documents.
- [x] Preserve collapsed document and annotation sidebars across navigation in
  the current browser tab.
- [x] Display the requested Git base and frozen abbreviated commit beside the
  File/Changes controls, with the full commit available as hover text.
- [x] Complete diff-specific browser coverage for narrow layouts and light/dark
  themes.
- [x] Design a bounded revision selector that re-pins an always-explicit
  server-wide comparison base validated against a live commit listing.
- [x] Implement bounded recent-commit discovery.
- [x] Implement the authenticated comparison state and select API routes.
- [x] Add the revision selector with abbreviated commit and truncated subject.
- [x] Add unit, handler, concurrency, and browser coverage for selection,
  rejection, cross-tab base changes, and failure behavior.
- [x] Update user-facing build/run documentation and refresh distributions
  after the code-review milestone receives maintainer approval.

### 11. Live reload

- [ ] Watch the selected content directory for Markdown and asset changes.
- [ ] Notify connected browser sessions using Server-Sent Events.
- [ ] Refresh the active document when its Markdown source changes.
- [ ] Refresh navigation when Markdown files are created, removed, or renamed.
- [ ] Refresh changed images and assets without stale browser-cache results.
- [ ] Debounce duplicate filesystem events and recover when the watcher fails.
- [ ] Add watcher, SSE, reconnection, and end-to-end live-reload tests.

### 12. Source syntax highlighting

- [ ] Select an offline, embedded highlighter with Go, C#, JavaScript, and
  TypeScript support.
- [ ] Preserve source-backed outer spans and exact annotation byte ranges.
- [ ] Support nested token elements in selection and annotation highlights.
- [ ] Add light/dark themes without runtime network dependencies.
- [ ] Add rendering, selection, CSP, and browser regression coverage.

### 13. Agent server discovery

See [`docs/designs/server-discovery.md`](docs/designs/server-discovery.md).

- [x] Define the discovery registry format, location, and security boundary.
- [x] Implement the discovery package (register, list, remove, state-dir
  resolution, `CODE_ANNOTATOR_STATE_DIR` override).
- [x] Register review-mode servers on startup and deregister on clean
  shutdown.
- [x] Add the `agent discover` CLI subcommand with `/healthz` liveness
  verification and `--root` disambiguation.
- [x] Self-heal stale registry entries left behind by an unclean exit.
- [x] Update the agent skill to try discovery before asking a human for a URL.
- [x] Add discovery, command, and end-to-end lifecycle tests.
- [x] Update user-facing documentation.

### 14. TypeScript frontend migration

Design: [`docs/designs/typescript-migration.md`](docs/designs/typescript-migration.md).

- [x] Add TypeScript compiler configuration and npm scripts.
- [x] Add Sass source partials, compile, and watch commands for the stylesheet.
- [x] Add a TypeScript watch command for save-time frontend development.
- [x] Add the `web/src` authored-source and `web/generated` generated-asset
  layout.
- [x] Define shared browser API and annotation types.
- [x] Convert review leaf modules and the API boundary.
- [x] Convert review controllers, rendering, selection, and navigation.
- [x] Convert standalone viewer and Mermaid integration scripts.
- [x] Embed generated assets while preserving existing `/static/*.js` routes.
- [x] Keep generated assets reproducible and verify them locally with a clean
  generated-output check.
- [ ] Add a CI workflow check for generated-asset reproducibility.
- [x] Run typecheck, Go tests, vet, and race checks.
- [x] Run browser regression tests with a working Edge process.
- [x] Update build and architecture documentation.

### 15. Cross-document open-comment status

Design: [`docs/designs/cross-document-comment-status.md`](docs/designs/cross-document-comment-status.md).

- [x] Replace the flat document list with an expandable file tree containing
  explicit directory rows, preserving existing catalog ordering and
  navigation.
- [x] Define the document-level active-comment summary from the existing
  annotation queue response.
- [x] Add mutually exclusive `Changed only` and `Open comments` scope toggles
  to the file tree.
- [x] Display accessible per-document active-comment counts and a matching
  document total.
- [x] Preserve document hierarchy, navigation, lookup, collapsed-sidebar
  state, and File/Changes mode while filtering.
- [x] Refresh counts after annotation creation, replies, lifecycle changes,
  reattachment, and revision-conflict reloads.
- [x] Add unit and browser coverage for filtering, counts, empty results,
  keyboard access, and narrow layouts.
- [x] Update user-facing documentation and verify the complete milestone.

Cheap queue polling follow-up, see
[`docs/designs/queue-etag.md`](docs/designs/queue-etag.md):

- [x] Add `ETag`/`If-None-Match` support to `GET /api/annotations`, splitting
  the handler into a cheap candidate-collection phase and an expensive
  anchor-resolution phase so a matching poll skips the latter entirely.
- [x] Add `--etag` to `agent queue`, with output unchanged when omitted and a
  small `{"etag","modified","queue"}` envelope when passed.
- [x] Add server-side ETag/304 tests and CLI `--etag` tests.
- [x] Update the agent skill and `server-discovery.md` to explain that
  establishing a polling loop is the caller's own runtime/orchestration
  concern (a scheduled wakeup, `/loop`, a cron job); this project only makes
  each individual poll cheap.

### 16. Skill-managed agent queue polling

Design: [`docs/designs/agent-queue-polling.md`](docs/designs/agent-queue-polling.md).

- [x] Add a skill-bundled polling helper with URL discovery and configurable
  interval/status options.
- [x] Carry the queue ETag between polls and emit only changed queue payloads
  on stdout.
- [x] Keep processing and annotation mutations in the existing live workflow;
  make the helper read-only and signal-safe.
- [x] Add fake-CLI shell tests for discovery, retries, ETag carry-forward,
  unchanged polls, and `--once`.
- [x] Document the helper in the skill and user-facing agent handoff docs.
- [x] Run the complete Go, vet, race, and browser checks and close the
  milestone. The full matrix passed on 2026-08-24.

### 17. Server-rendered review UI and testable TypeScript

Design:
[`docs/designs/server-rendered-review-ui.md`](docs/designs/server-rendered-review-ui.md).

Each unchecked item is one maintainer review gate. The implementing agent must
complete and commit only one item, report its checks and commit hash, and wait
for explicit approval before starting the next item.

- [x] Commit 0: approve the architecture, documentation authority, milestones,
  acceptance criteria, and one-commit-at-a-time review protocol.
- [x] Commit 1: add Vitest/`happy-dom`, `test:unit`, and the first surviving
  pure TypeScript extraction without changing runtime behavior.
- [x] Commit 2: vendor, license, embed, and serve HTMX 2.0.10 without loading it
  in the page.
- [x] Commit 3: add inactive annotation fragment templates, presentation view
  models, and escaping/action-availability tests.
- [x] Role-only compatibility commit: remove free-text authorship, use one role
  across storage/API/CLI/browser contracts, and migrate schema v1 sidecars on
  their next successful save.
- [x] Commit 4: share annotation read/create application operations and add
  compatible HTML handlers while preserving JSON behavior.
- [x] Commit 4 review follow-up: preserve comments when a captured selection's
  document digest is stale, expose the result as requiring reattachment, and
  use anchor terminology consistently across shared operations.
- [x] Commit 5: share reply/transition operations and add 422/409 HTML
  responses while preserving JSON behavior.
- [x] Commit 6: share reattach operations and complete the inactive HTML
  mutation surface.
- [x] Commit 7: activate the HTMX annotation panel, retain browser-only
  selection/highlight/navigation adapters, and remove superseded DOM renderers.
- [x] Commit 7 review follow-up A: establish the inactive versioned viewer-state
  endpoint, runtime-validated TypeScript contract, and shrinking `data-*`
  enforcement baseline.
- [x] Commit 7 review follow-up B: migrate annotation, source, Mermaid,
  lifecycle, revision, digest, selection, and navigation state from custom
  attributes to semantic IDs and typed in-memory state.
- [x] Commit 7 review follow-up C: define and inventory the DOM/state boundary,
  revise the remaining gates, and stop before implementation.
- [x] Commit 8A: add runtime-validated document catalog state and pure DOM-free
  tree/filter/count logic without activating it.
- [x] Commit 8B: server-render the document tree and counts; activate server
  filtering only if the documented 5,000-path benchmark passes.
- [x] Commit 9: server-render comparison selection while retaining the JSON
  comparison API.
- [x] Commit 10: replace remaining import-time IIFEs with explicit,
  dependency-injected initializers and finish TypeScript unit/lint coverage.
- [ ] Commit 11: run the complete verification matrix, reconcile all docs,
  remove dead assets/tests, and refresh distributions after source approval.

## Decisions

| Decision | Status |
| --- | --- |
| Local HTTP server with embedded frontend assets | Approved |
| Loopback-only binding with OS-selected port by default | Approved |
| `github.com/yuin/goldmark` with GFM and raw HTML disabled | Approved |
| `github.com/pkg/browser` for default-browser launch | Approved |
| Browser-launch failure is non-fatal | Approved |
| Annotations stored separately from Markdown in versioned JSON sidecars | Approved |
| Annotation review scheduled before live reload | Approved |
| Mermaid rendered client-side from an embedded pinned bundle | Approved |
| Mermaid uses strict security and no runtime CDN or browser automation | Approved |
| Live reload deferred until after annotation and diagram milestones | Approved |
| Editing and network sharing deferred | Approved |
| Agent discovery registers via a cooperative per-user registry file, never a port scan | Approved |
| Discovery registry never stores the review mutation token | Approved |
| TypeScript source compiles to checked-in generated browser assets | Approved |
| Existing browser module boundaries and `/static/*.js` URLs remain stable | Approved |
| Go templates own authoritative review HTML; TypeScript owns browser-only interaction | Approved |
| Existing `/api/*` JSON contracts remain stable; browser fragments use separate `/ui/*` routes | Approved |
| HTMX 2.0.10 is pinned, vendored, licensed, embedded, and loaded only from the viewer origin | Approved |
| Milestone 17 advances one reviewed commit at a time and stops after every commit | Approved |
| Repository documentation is updated with each slice and is the cross-session handoff contract | Approved |
| Annotation role is both attribution and permission; free-text authorship is not stored | Approved |
| DOM may provide interaction and layout input but never document, annotation, comparison, or workflow state | Approved |

## Known risks

- Filesystem containment must handle symlinks as well as lexical traversal.
- Relative-link rewriting needs coverage for nested paths, fragments, queries,
  and URL-encoded filenames.
- Browser opening varies in graphical, SSH, container, and WSL environments;
  `--no-open` and a printed URL provide the fallback.
- The first release tag remains a deliberate maintainer action after review of
  the verified release candidate.

## Next milestone

Milestone 17 (server-rendered review UI and testable TypeScript) is the active
milestone. Commit 7 and review follow-ups A and B are approved. Follow-up B
activates the versioned viewer-state endpoint and validates its unknown JSON
payload into strongly typed TypeScript maps, and removes annotation/source/Mermaid/lifecycle
application state from HTML. Follow-up C is the current documentation review
gate and broadens the invariant from custom attributes to all DOM-derived
application state. Commits 8A and 8B implement and activate the typed document
catalog, recursive server-rendered tree, counts, and server filtering. The
commits 8A, 8B, 9, and 10 are implemented and ready for the maintainer's
combined review. Commit 11 remains gated on that source review.

Milestone 15 (cross-document open-comment status) is implemented and verified.
Milestone 16 (skill-managed agent queue polling) is implemented and verified.
Milestone 14
(TypeScript frontend migration) is implemented and verified.
Typecheck, Go tests, vet, race checks, generated-output reproducibility, and all
previously available Playwright Edge tests passed when milestone 14 closed. The
current suite's 40 browser cases passed on 2026-08-24. The only unchecked item
in milestone 14 is a CI workflow check for generated-asset reproducibility;
this repository has no existing CI workflow yet. Live reload and source syntax
highlighting remain separately scoped milestones.
