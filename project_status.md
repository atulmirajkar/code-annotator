# Project status

Last updated: 2026-08-20

## Current state

**Phase:** Manual-refresh MVP implemented; release tag pending.

The MVP scope is implemented with automated unit, handler, lifecycle, race, and
cross-platform build verification. Markdown and asset updates are read from disk
on browser refresh. Annotation review is the next product milestone, followed by
live reload.

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
- [x] Define CLI export commands for AI-agent consumption.
- [ ] Add an explicit writable review mode and annotation directory handling.
- [ ] Implement atomic sidecar persistence and stale-anchor detection.
- [ ] Implement annotation HTTP APIs with origin and session-token protection.
- [ ] Add rendered-text selection, highlights, and the review panel.
- [ ] Add annotation list, export, and resolve CLI commands.
- [ ] Add storage, API, UI, security, and agent-handoff tests.

### 7. Live reload

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
| Live reload deferred to post-annotation milestone 7 | Approved |
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

Review and approve the annotation design, then break milestone 6 into small
implementation commits.
