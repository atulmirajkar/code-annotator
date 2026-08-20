# md-viewer

`md-viewer` is a local Go web application for browsing and reading Markdown
files from a directory. It starts a server on the loopback interface, renders
Markdown as HTML, and opens the viewer in the user's default browser.

The MVP is implemented and ready to run from source or as a compiled binary.

## MVP goals

- Accept a directory as a positional command-line argument.
- Recursively list Markdown files under that directory.
- Render GitHub Flavored Markdown in a browser-friendly page.
- Serve document-relative images and other local assets.
- Open the viewer with `github.com/pkg/browser`.
- Restrict access to the selected directory and bind to localhost by default.
- Shut down cleanly when interrupted.

Editing, uploads, live reload, full-text search, and network sharing are not in
the initial MVP.

## Usage

Run directly from the repository:

```sh
go run ./cmd/md-viewer ./docs
```

Or build and run the binary:

```sh
go build -o bin/md-viewer ./cmd/md-viewer
./bin/md-viewer ./docs
```

The server will listen on an available loopback port and open the resulting URL
in the default browser. If the browser cannot be opened, the URL will remain
available in the terminal.

Available flags:

```text
--port <number>  Use a specific loopback port instead of an OS-selected port
--no-open        Start the server without opening a browser
```

## Documentation

- [MVP design](docs/designs/mvp.md)
- [Architecture](docs/architecture.md)
- [Build and run](docs/build.md)
- [Project status](project_status.md)

## Development status

The manual-refresh MVP is implemented. Saving a Markdown file and refreshing the
browser reads and renders the latest contents from disk. See
[`project_status.md`](project_status.md) for release readiness and the planned
live-reload milestone.
