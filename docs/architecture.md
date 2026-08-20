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
internal/render/            goldmark configuration and page rendering
internal/server/            routes, handlers, HTTP server, graceful shutdown
internal/launch/            thin, testable wrapper around pkg/browser
web/                        embedded HTML templates and CSS
```

Package boundaries should remain small. In particular, `internal/content`
owns filesystem safety, while HTTP handlers consume its API rather than joining
untrusted URL paths themselves.

## HTTP routes

| Route | Purpose |
| --- | --- |
| `GET /` | Display a recursive, sorted tree of Markdown documents. |
| `GET /view/{path}` | Safely load and render a `.md` file. |
| `GET /asset/{path}` | Serve a document-relative local asset. |
| `GET /healthz` | Return a minimal readiness response for tests and tooling. |

Unknown resources return `404`. Unsupported methods return `405`. Internal
filesystem paths and raw errors are not returned to the browser.

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

Live reload, syntax highlighting, search, table-of-contents generation, raw HTML,
and non-loopback listening are deferred. Any future network-sharing option must
be explicit and should include a separate security review.
