# Project status

Last updated: 2026-08-20

## Current state

**Phase:** Annotation review implementation; release tag pending.

The manual-refresh MVP and secured annotation APIs are implemented. Review-mode
pages include selection mapping, annotation display, and secured creation;
highlights and remaining browser mutation controls remain. Embedded diagram
rendering and live reload follow annotation review.

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
- [ ] Add annotation list, export, reply, and resolve CLI commands.
- [ ] Add storage, API, UI, security, and agent-handoff tests.

### 7. Mermaid and sequence diagrams

- [ ] Pin Mermaid Tiny and retain its license in the embedded web assets.
- [ ] Bundle Mermaid at build time with no runtime Node.js, Chromium, CDN, or
  network dependency.
- [ ] Recognize fenced `mermaid` blocks and render them as client-side SVG.
- [ ] Use strict Mermaid security, bounded diagram input, and a self-only script
  Content Security Policy without `unsafe-inline`.
- [ ] Preserve the source code block and show a useful message when diagram
  parsing or rendering fails.
- [ ] Add responsive diagram styling and viewer-theme integration.
- [ ] Treat a rendered diagram as one source-backed annotation region rather
  than mapping individual SVG labels to Markdown offsets.
- [ ] Add sequence-diagram, malformed-input, CSP, offline-asset, and annotation
  integration tests.

### 8. Live reload

- [ ] Watch the selected content directory for Markdown and asset changes.
- [ ] Notify connected browser sessions using Server-Sent Events.
- [ ] Refresh the active document when its Markdown source changes.
- [ ] Refresh navigation when Markdown files are created, removed, or renamed.
- [ ] Refresh changed images and assets without stale browser-cache results.
- [ ] Debounce duplicate filesystem events and recover when the watcher fails.
- [ ] Add watcher, SSE, reconnection, and end-to-end live-reload tests.

## Decisions

| Decision | Status |
| --- | --- |
| Local HTTP server with embedded frontend assets | Approved |
| Loopback-only binding with OS-selected port by default | Approved |
| `github.com/yuin/goldmark` with GFM and raw HTML disabled | Approved |
| `github.com/pkg/browser` for default-browser launch | Approved |
| Browser-launch failure is non-fatal | Approved |
| Annotations stored separately from Markdown in versioned JSON sidecars | Proposed |
| Annotation review scheduled before live reload | Approved |
| Mermaid rendered client-side from an embedded pinned bundle | Proposed |
| Mermaid uses strict security and no runtime CDN or browser automation | Proposed |
| Live reload deferred until after annotation and diagram milestones | Approved |
| Editing and network sharing deferred | Approved |

## Known risks

- Filesystem containment must handle symlinks as well as lexical traversal.
- Relative-link rewriting needs coverage for nested paths, fragments, queries,
  and URL-encoded filenames.
- Browser opening varies in graphical, SSH, container, and WSL environments;
  `--no-open` and a printed URL provide the fallback.
- The current `go.mod` declares Go `1.26.5`; implementation should confirm that
  this is the intended minimum toolchain before the first release.

## Next milestone

Add agent-friendly Markdown export on top of the offline annotation list model
as the next independently reviewable commit.
