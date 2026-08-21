# Build and run

## Requirements

- Go `1.26.5` or a compatible newer Go release, matching `go.mod`.
- A graphical environment with a default browser, unless using `--no-open`.

The Go toolchain downloads declared module dependencies during the first build.

## Run from source

From the repository root:

```sh
go run ./cmd/md-viewer ./path/to/markdown-folder
```

For example:

```sh
go run ./cmd/md-viewer ./docs
```

The application prints the selected content root and local URL. By default, it
opens that URL through `github.com/pkg/browser`.

## Build

```sh
go build -o bin/md-viewer ./cmd/md-viewer
```

Run the resulting binary:

```sh
./bin/md-viewer ./path/to/markdown-folder
```

On Windows, use an `.exe` output name:

```powershell
go build -o bin/md-viewer.exe ./cmd/md-viewer
./bin/md-viewer.exe ./docs
```

## Command-line options

Use a fixed loopback port:

```sh
./bin/md-viewer --port 8080 ./docs
```

Start without opening a browser:

```sh
./bin/md-viewer --no-open ./docs
```

Enable the annotation review storage boundary:

```sh
./bin/md-viewer --review ./docs
```

The default sidecar directory is `<content-root>/.md-viewer/annotations/`. Use
an alternate location when the content root should remain untouched:

```sh
./bin/md-viewer --review --annotations-dir ./reviews ./docs
```

`--annotations-dir` is rejected without `--review`. Review mode initializes and
reports the writable store and enables annotation reads and secured creation.
The secured API also supports ordinary discussion replies. Browser controls
plus lifecycle and reattachment routes remain pending.

With no fixed port, the server binds to `127.0.0.1:0` and reports the port chosen
by the operating system.

## Verify

Format and test the code:

```sh
go fmt ./...
go test ./...
go vet ./...
go test -race ./...
```

A minimal manual check is:

1. Create a directory containing a Markdown file and a relative image.
2. Start `md-viewer` with that directory.
3. Confirm the browser opens and the document and image render.
4. Edit the Markdown and refresh the page to see the update.
5. Stop the server with `Ctrl-C` and confirm it exits cleanly.

## Install locally

Install it into the active Go binary directory:

```sh
go install ./cmd/md-viewer
```

Ensure the directory reported by `go env GOBIN`, or `$(go env GOPATH)/bin` when
`GOBIN` is empty, is on your shell's `PATH`.
