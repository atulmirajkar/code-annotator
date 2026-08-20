# Project status

Last updated: 2026-08-20

## Current state

**Phase:** MVP design complete; implementation not started.

The MVP scope and architecture are documented. No application source or tests
exist yet, so the build and run commands are a target contract rather than a
verified workflow.

## Milestones

### 1. Product and technical design

- [x] Define MVP goals and exclusions.
- [x] Define CLI behavior and browser-launch approach.
- [x] Define HTTP routes and package boundaries.
- [x] Define filesystem containment and Markdown security requirements.
- [x] Document build, run, and verification workflow.

### 2. CLI and process lifecycle

- [ ] Add `cmd/md-viewer` entry point.
- [ ] Parse the directory argument, `--port`, and `--no-open`.
- [ ] Validate and resolve the content root.
- [ ] Bind an OS-selected or explicit loopback port.
- [ ] Add signal handling and graceful shutdown.

### 3. Content and rendering

- [ ] Implement recursive Markdown discovery and stable ordering.
- [ ] Implement traversal- and symlink-safe content lookup.
- [ ] Configure goldmark with GFM extensions and safe defaults.
- [ ] Rewrite document-relative Markdown links and asset references.
- [ ] Add embedded HTML templates and responsive styling.

### 4. HTTP server and browser integration

- [ ] Implement index, view, asset, and health handlers.
- [ ] Add security headers, timeouts, and useful error responses.
- [ ] Integrate `github.com/pkg/browser` through a testable launch adapter.
- [ ] Keep the server alive and print the URL when browser launch fails.

### 5. Verification and release readiness

- [ ] Add unit tests for path containment, indexing, and rendering.
- [ ] Add handler and lifecycle integration tests.
- [ ] Verify relative links and assets on nested documents.
- [ ] Run `go test ./...`, `go vet ./...`, and `go test -race ./...`.
- [ ] Verify build and manual run instructions on supported platforms.
- [ ] Prepare the first tagged MVP release.

### 6. Live reload

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
| Live reload deferred to post-MVP milestone 6 | Approved |
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

Implement milestone 2: the CLI, validated content root, loopback listener, and
graceful process lifecycle.
