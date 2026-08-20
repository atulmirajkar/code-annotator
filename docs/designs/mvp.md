# MVP design: local Markdown viewer

## Status

Approved for implementation. This document defines the first usable release of
`md-viewer`. Milestone progress is tracked in
[`../../project_status.md`](../../project_status.md).

## Problem

Markdown files are easy to author but less comfortable to read as raw text. A
user should be able to point one command at a folder and immediately browse its
Markdown documents in a normal web browser without copying files, running a
general-purpose static-site generator, or exposing the folder on the network.

## Product goal

Given a readable directory, start a local web server that:

1. Discovers Markdown files below that directory.
2. Presents them in navigable form.
3. Renders a selected document as safe HTML.
4. Serves its relative local assets.
5. Opens the viewer in the user's default browser.

## User experience

The primary workflow is one command:

```sh
md-viewer ./notes
```

Expected terminal output resembles:

```text
Serving /absolute/path/to/notes at http://127.0.0.1:54321
Opened in the default browser
Press Ctrl-C to stop
```

If browser launch fails:

```text
Serving /absolute/path/to/notes at http://127.0.0.1:54321
Could not open a browser; open the URL above manually
Press Ctrl-C to stop
```

The failure does not stop the server.

## Command-line contract

```text
Usage: md-viewer [options] <directory>

Options:
  --port <number>  loopback port; default 0 selects an available port
  --no-open        do not open the default browser
  -h, --help       display usage
```

Startup fails with a non-zero exit status if the directory is missing,
unreadable, or not a directory, or if the requested port cannot be bound.

## Functional requirements

### Document discovery

- Recursively discover files with a case-insensitive `.md` extension.
- Present a deterministic, case-insensitive sort order.
- Preserve directory hierarchy in navigation.
- Prefer a root-level `README.md` as the initial document.
- Show a useful empty state when no Markdown files exist.

### Markdown rendering

- Render CommonMark plus GitHub Flavored Markdown tables, task lists,
  strikethrough, and autolinks.
- Do not render raw HTML in the MVP.
- Re-read a document on each request so browser refresh reflects disk changes.
- Wrap output in an embedded, responsive HTML template.
- Return a useful `404` when a document disappears after indexing.

### Links and assets

- Map relative links ending in `.md` to the corresponding viewer route.
- Serve relative images and downloadable assets from the selected content root.
- Resolve relative references against the current document's directory.
- Preserve anchors and query strings when rewriting local links.
- Leave `http`, `https`, and `mailto` links external.
- Never expose automatic directory listings.

### Browser launch

- Use `github.com/pkg/browser` rather than application-owned OS command logic.
- Call `browser.OpenURL` only after the HTTP listener is ready.
- Pass the complete loopback URL selected by the listener.
- Treat launch errors as warnings, not fatal server errors.
- Skip launch entirely when `--no-open` is present.

### Shutdown

- Handle `Ctrl-C`, `SIGINT`, and `SIGTERM`.
- Stop accepting new requests and allow active requests a short bounded period
  to finish.
- Exit successfully after an ordinary user-initiated shutdown.

## Security and privacy requirements

- Listen on `127.0.0.1` only for the MVP.
- Treat URL paths and Markdown content as untrusted input.
- Reject absolute paths, traversal paths, and symlinks resolving outside the
  selected content root.
- Use path-aware containment checks rather than string prefixes.
- Restrict the render route to Markdown files.
- Cap Markdown input size before reading it fully into memory.
- Keep goldmark raw HTML rendering disabled.
- Avoid returning absolute filesystem paths or detailed internal errors in HTTP
  responses.
- Set `X-Content-Type-Options: nosniff` and a restrictive Content Security Policy
  suitable for the embedded page assets.

## Non-functional requirements

- Ship as a single Go binary with templates and styles embedded via `go:embed`.
- Support macOS, Windows, and mainstream desktop Linux environments supported by
  `github.com/pkg/browser`.
- Have no required runtime configuration or network dependency.
- Start quickly for directories containing ordinary documentation collections.
- Produce actionable terminal errors and concise HTTP error pages.

## Proposed internal design

The command validates arguments, resolves the root, and creates a TCP listener
on `127.0.0.1`. Once the listener exists, it starts the HTTP server and invokes a
launch adapter backed by `browser.OpenURL`. Requests pass through security-header
middleware to handlers that rely on a root-safe content service. Markdown is
rendered by goldmark into an embedded page template.

See [`../architecture.md`](../architecture.md) for package boundaries, routes,
and lifecycle details.

## Error behavior

| Condition | Result |
| --- | --- |
| Missing or invalid root | Print error and usage; exit non-zero. |
| Port unavailable | Print bind error; exit non-zero. |
| Browser launch failure | Print warning and URL; continue serving. |
| Missing document or asset | Return `404`. |
| Escaping path or symlink | Return `404` without revealing why. |
| Oversized Markdown file | Return `413 Request Entity Too Large`. |
| Render or read failure | Return a generic `500` and log local details. |

## Acceptance criteria

- `md-viewer ./docs` starts on loopback, prints its URL, and requests browser
  launch with that exact URL.
- The index lists nested Markdown files and selects `README.md` by default.
- GFM tables, task lists, strikethrough, and autolinks render correctly.
- Relative Markdown links and images work from nested directories.
- Editing a file and refreshing displays the new content.
- Traversal attempts and symlinks outside the root cannot retrieve files.
- Browser launch failure leaves a usable server and visible URL.
- `--no-open` never attempts browser launch.
- `Ctrl-C` performs a bounded graceful shutdown.
- Unit, handler, integration, race, and vet checks pass.

## Out of scope

- Editing or uploading files.
- Live reload or filesystem watching.
- Full-text search and generated tables of contents.
- Syntax highlighting beyond basic CSS presentation.
- Rendering raw HTML from Markdown.
- Authentication or access from other machines.
- Configuration files, plugins, and user-defined themes.

## Follow-up candidates

After the MVP is stable, consider file-watcher-driven reload using Server-Sent
Events, theme selection, syntax highlighting, search, and an explicit
network-sharing mode. These should be separate designs rather than implicit MVP
expansion.
