# Build and run

## Requirements

- Go `1.26.5` or a compatible newer Go release, matching `go.mod`.
- A graphical environment with a default browser, unless using `--no-open`.

The optional browser regression suite additionally requires Node.js, npm, and
Google Chrome. These tools are used only for development tests and are not
runtime dependencies of `md-viewer`.

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

Include the default Go, C#, JavaScript, TypeScript, JSON, and `.csproj` source
files as escaped, line-numbered documents:

```sh
./bin/md-viewer --include-code ./repository
```

Start an optional local Git comparison base at startup:

```sh
./bin/md-viewer --include-code --diff-base origin/main ./repository
```

The viewer does not fetch remotes; remote-tracking names must already exist in
the local repository. The revision is resolved to an immutable commit at
startup and pinned as the initial comparison base.

Replace that source extension set or add directory exclusions:

```sh
./bin/md-viewer --code-extensions .go,.cs \
  --exclude-dirs generated,tmp ./repository
```

`--code-extensions` implies `--include-code`. Source discovery excludes
`node_modules`, `vendor`, `bin`, and `obj` by default.

With `--diff-base` configured, a cataloged source file offers **File** and
**Changes** links in its toolbar. Changes renders the base and current text
side by side in independently scrollable panes with a draggable,
keyboard-resizable divider; the toolbar and revision selector stay visible
while scrolling. The revision selector re-pins the comparison base to any
other locally available commit through `POST /api/git-comparison`, which is
guarded by the exact loopback origin, a distinct comparison control token, and
a bounded, freshly listed set of selectable commits. The base is always one
explicit commit and never moves on its own; a reviewer re-pins it explicitly
to see an agent's next committed change against the same starting point. The
document sidebar's **Changed only** filter defaults on when a changed document
exists and no explicit choice has been made yet in the tab, and it composes
with **Find document**. Code review annotations reuse the same selection,
lifecycle, and agent-handoff workflow as Markdown, current-side only.

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
reports the writable store and enables annotation reads and secured mutations.
The browser includes an annotation panel, previews and highlights selections
across formatting elements, and creates selection- or document-level
annotations. Annotation cards expose only lifecycle transitions valid for their
current status and collect the required resolution summary or review message.
Inline replies append discussion without changing lifecycle state. Stale-anchor
reattachment uses the current document selection from the stale annotation's
card and is also available through the API.

With no fixed port, the server binds to `127.0.0.1:0` and reports the port chosen
by the operating system.

List annotations without running the server:

```sh
./bin/md-viewer annotations list --root ./docs \
  --status open,needs_changes --format json

./bin/md-viewer annotations export --root ./docs \
  --status open,needs_changes --format markdown

./bin/md-viewer annotations reply --root ./docs --id ann_... \
  --author reviewer --message "Please retain the default."

./bin/md-viewer annotations resolve --root ./docs --id ann_... \
  --status applied --role agent --author codex \
  --summary "Implemented request" --commit abc1234
```

Use `--annotations-dir ./reviews` when review mode used an external sidecar
root. A missing annotation directory returns an empty JSON document list and is
not created by these read-only commands.
The `list` and `export` commands are read-only. Markdown export includes stable IDs, original
source selections, current anchor state, reviewer comments, and thread history.
The `reply` command is a mutation: it requires an existing annotation directory,
appends one ordinary thread entry, and uses the loaded revision for optimistic
concurrency. It never changes lifecycle status.
The `resolve` command applies actor-controlled transitions and their required
activity. For example, `applied` requires `--summary`, while `needs_changes` and
`rejected` require `--message`.

These commands are offline tooling. While a review server is live, agents use
its annotation HTTP API so browser and agent mutations share the same revision
coordinator:

```sh
./bin/md-viewer agent queue --url http://127.0.0.1:54321
./bin/md-viewer agent resolve --url http://127.0.0.1:54321 \
  --document README.md --revision <revision> --id ann_... \
  --status acknowledged --role agent --author codex
```

The agent client obtains the ephemeral review token from the supplied loopback
viewer page and never accesses sidecar storage directly.

## Verify

Format and test the code:

```sh
go fmt ./...
go test ./...
go vet ./...
go test -race ./...
```

Install the pinned browser-test dependency and run the Playwright suite:

```sh
npm ci
npm run test:browser
```

The suite starts an isolated review server, writes annotations only beneath a
temporary directory, and uses the installed Chrome browser in headless mode.
Set `MD_VIEWER_BROWSER_CHANNEL` to another Playwright browser channel when
needed. Browser traces and reports are ignored build artifacts.

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
