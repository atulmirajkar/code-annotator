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

Prebuilt binaries are available under [`dist/`](dist/):

| Platform | Architecture | Binary |
| --- | --- | --- |
| macOS | arm64 (Apple silicon) | [`dist/darwin-arm64/md-viewer`](dist/darwin-arm64/md-viewer) |
| macOS | amd64 (Intel) | [`dist/darwin-amd64/md-viewer`](dist/darwin-amd64/md-viewer) |
| Linux | arm64 | [`dist/linux-arm64/md-viewer`](dist/linux-arm64/md-viewer) |
| Linux | amd64 | [`dist/linux-amd64/md-viewer`](dist/linux-amd64/md-viewer) |
| Windows | arm64 | [`dist/windows-arm64/md-viewer.exe`](dist/windows-arm64/md-viewer.exe) |
| Windows | amd64 | [`dist/windows-amd64/md-viewer.exe`](dist/windows-amd64/md-viewer.exe) |

On macOS or Linux, make the selected binary executable if needed, then run it:

```sh
chmod +x dist/darwin-arm64/md-viewer
./dist/darwin-arm64/md-viewer ./docs
```

Regenerate every platform binary after a source update with:

```sh
./scripts/build-dist.sh
```

See [`dist/README.md`](dist/README.md) for the release layout and build details.

The server will listen on an available loopback port and open the resulting URL
in the default browser. If the browser cannot be opened, the URL will remain
available in the terminal.

Available flags:

```text
--port <number>             Use a specific loopback port instead of an OS-selected port
--no-open                   Start the server without opening a browser
--review                    Enable writable annotation review mode
--annotations-dir <path>    Store annotations at a custom path; requires --review
```

Review mode establishes the annotation storage boundary and enables the
annotation read API plus secured annotation creation. The browser review
interface and remaining mutation routes are being delivered in subsequent
milestone commits. By default, sidecars are stored under
`<content-root>/.md-viewer/annotations/`. To select another location:

```sh
md-viewer --review --annotations-dir ./reviews ./docs
```

## Documentation

- [MVP design](docs/designs/mvp.md)
- [Annotation review design](docs/designs/annotations.md)
- [Architecture](docs/architecture.md)
- [Build and run](docs/build.md)
- [Project status](project_status.md)
- [Prebuilt binaries](dist/README.md)

## Development status

The manual-refresh MVP is implemented, and annotation storage and anchoring are
under active development. Saving a Markdown file and refreshing the browser
reads and renders the latest contents from disk. See
[`project_status.md`](project_status.md) for release readiness and the planned
live-reload milestone.
