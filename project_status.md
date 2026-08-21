# Project status

Last updated: 2026-08-21

## Current state

**Phase:** Viewer navigation polish verified; review required before live reload.

The manual-refresh MVP and annotation review workflow are implemented, including
selection mapping, highlights, secured browser mutations, live API agent handoff,
and a repository-owned agent skill. Focused browser interaction coverage and
embedded diagram rendering are complete. Live reload remains deferred until
release verification is reviewed.

## Milestones

### 1. Product and technical design

- [x] Define MVP goals and exclusions.
- [x] Define CLI behavior and browser-launch approach.
- [x] Define HTTP routes and package boundaries.
- [x] Define filesystem containment and Markdown security requirements.
- [x] Document build, run, and verification workflow.

### 2. CLI and process lifecycle

- [x] Add `cmd/md-viewer` entry point.
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
- [ ] Implement bounded Git comparison and current-file diff presentation.
- [ ] Add a changed-only document filter backed by the configured Git base.
- [x] Extend agent handoff and browser coverage to code review.

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

## Known risks

- Filesystem containment must handle symlinks as well as lexical traversal.
- Relative-link rewriting needs coverage for nested paths, fragments, queries,
  and URL-encoded filenames.
- Browser opening varies in graphical, SSH, container, and WSL environments;
  `--no-open` and a printed URL provide the fallback.
- The first release tag remains a deliberate maintainer action after review of
  the verified release candidate.

## Next milestone

Review and approve the decisions in `docs/designs/code-review.md`, then implement
that milestone in small commits. Live reload remains deferred until code review
is complete and separately approved.
