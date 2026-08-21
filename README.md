# code-annotator

`code-annotator` is a local Go web application for reviewing Markdown and
source code from a directory. It starts a server on the loopback interface,
renders safe HTML, and opens the viewer in the user's default browser. Beyond
plain Markdown viewing, it supports side-by-side Git diff comparison and a
full annotation review workflow, with browser and offline CLI tooling for
human and AI-agent handoff.

The MVP is implemented and ready to run from source or as a compiled binary.

## MVP goals

These were the original MVP goals; annotation review and code review with Git
diff comparison, described later in this document, were added afterward.

- Accept a directory as a positional command-line argument.
- Recursively list Markdown files under that directory.
- Render GitHub Flavored Markdown in a browser-friendly page.
- Serve document-relative images and other local assets.
- Open the viewer with `github.com/pkg/browser`.
- Restrict access to the selected directory and bind to localhost by default.
- Shut down cleanly when interrupted.

Editing, uploads, live reload, full-text search, and network sharing are not in
the initial MVP.

Fenced `mermaid` blocks render as embedded, client-side SVG diagrams without a
CDN or runtime network dependency. In review mode, clicking a rendered diagram
selects its complete source definition for annotation; its collapsible source
remains available for line-level comments.

## Usage

Run directly from the repository:

```sh
go run ./cmd/code-annotator ./docs
```

Or build and run the binary:

```sh
go build -o bin/code-annotator ./cmd/code-annotator
./bin/code-annotator ./docs
```

Prebuilt binaries are available under [`dist/`](dist/):

| Platform | Architecture | Binary |
| --- | --- | --- |
| macOS | arm64 (Apple silicon) | [`dist/darwin-arm64/code-annotator`](dist/darwin-arm64/code-annotator) |
| macOS | amd64 (Intel) | [`dist/darwin-amd64/code-annotator`](dist/darwin-amd64/code-annotator) |
| Linux | arm64 | [`dist/linux-arm64/code-annotator`](dist/linux-arm64/code-annotator) |
| Linux | amd64 | [`dist/linux-amd64/code-annotator`](dist/linux-amd64/code-annotator) |
| Windows | arm64 | [`dist/windows-arm64/code-annotator.exe`](dist/windows-arm64/code-annotator.exe) |
| Windows | amd64 | [`dist/windows-amd64/code-annotator.exe`](dist/windows-amd64/code-annotator.exe) |

On macOS or Linux, make the selected binary executable if needed, then run it:

```sh
chmod +x dist/darwin-arm64/code-annotator
./dist/darwin-arm64/code-annotator ./docs
```

Regenerate every platform binary after a source update with:

```sh
./scripts/build-dist.sh
```

See [`dist/README.md`](dist/README.md) for the release layout and build details.

The server will listen on an available loopback port and open the resulting URL
in the default browser. If the browser cannot be opened, the URL will remain
available in the terminal.

The document and annotation sidebars can be collapsed independently to give the
rendered document more room. The document sidebar starts visible and the
annotation sidebar starts collapsed; either default is overridden for the rest
of the browser tab once toggled. Use the document sidebar's **Find document**
field to filter by any case-insensitive portion of a relative path. Press `/`
from the document view to focus lookup, Enter to open the first match, or
Escape to clear the filter.

Available flags:

```text
--port <number>             Use a specific loopback port instead of an OS-selected port
--no-open                   Start the server without opening a browser
--review                    Enable writable annotation review mode
--annotations-dir <path>    Store annotations at a custom path; requires --review
--include-code              Include the default supported source extensions
--code-extensions <csv>     Replace the source extension set; implies --include-code
--exclude-dirs <csv>        Add directory base names to exclude from discovery
--diff-base <revision>      Start Git comparison at a locally available commit; the
                             browser can re-pin it to another commit afterward
```

For example, `--include-code` adds escaped, line-numbered Go, C#, JavaScript,
TypeScript, JSON, and `.csproj` files. With `--review`, these source files use
the same selection, annotation, thread, lifecycle, and agent handoff workflows
as Markdown.

`--diff-base` accepts a locally resolvable commit, branch, tag, or
remote-tracking ref such as `HEAD~1` or `origin/main`. It never fetches. The
revision is resolved to an immutable full commit at startup, and startup fails
if the content root is outside its Git worktree or the revision is unavailable.
Cataloged source files then offer a **File** and **Changes** toggle. Changes
renders the base and current text side by side in independently scrollable
panes with a draggable, keyboard-resizable divider, and the toolbar stays
visible while scrolling a long file. A revision selector next to the toggle
re-pins the comparison base to any other locally available commit; selecting
one reloads the page against the new base. The base is always one explicit
commit and never moves on its own, so it keeps showing an agent's committed
change until a reviewer chooses a different base.

When comparison is configured, the document sidebar also offers **Changed
only**. It includes supported tracked changes against the active base and
untracked, non-ignored files, and composes with **Find document**. Before a
reviewer makes an explicit choice in the tab, it defaults on whenever a
changed document exists. Refresh the browser to evaluate a new worktree
snapshot; re-pinning the base picks up new commits without restarting the
server.

Review mode establishes the annotation storage boundary and enables the
annotation APIs. Its browser panel displays comments, lifecycle state, threads,
and stale-anchor warnings. Selections across formatting elements show their
Markdown byte range and are bound to the rendered document revision. Highlights
and lifecycle controls let agents acknowledge, apply, or reject work and let
reviewers close, reopen, or request more changes. Each card also accepts inline
discussion replies without changing its lifecycle status. The creation form can attach
a new annotation to the current selection or the whole document, and annotation
cards preview their selected source and line range. Resolved selections are highlighted in the document;
stale and document-level annotations remain panel-only. Closed and rejected
annotations and their highlights are hidden by default and can be restored with
the panel's history toggle. A stale text annotation can be reattached by
selecting its replacement text and using the action on its card. By default, sidecars are stored under
`<content-root>/.code-annotator/annotations/`. To select another location:

```sh
code-annotator --review --annotations-dir ./reviews ./docs
```

List annotations for agents or local tooling without starting the server:

```sh
code-annotator annotations list --root ./docs --status open,needs_changes
code-annotator annotations export --root ./docs --status open,needs_changes
code-annotator annotations reply --root ./docs --id ann_... \
  --author reviewer --message "Please retain the default."
code-annotator annotations resolve --root ./docs --id ann_... \
  --status applied --role agent --author codex \
  --summary "Implemented request" --commit abc1234
```

`list` emits deterministic JSON. `export` produces an agent-friendly Markdown
handoff containing source quotes, current anchor state, comments, and complete
threads, plus each document's kind and language. Both accept `--include-code`,
`--code-extensions`, and `--exclude-dirs` to mirror a source-enabled viewer,
and accept `--annotations-dir` when sidecars are stored outside the content
root. Repeat the catalog flags on `reply` and `resolve` when processing a source
file. Omitting `--status` includes every lifecycle state.
`reply` appends an ordinary discussion entry directly to the matching sidecar
and returns the updated annotation and revision as JSON. It does not change the
annotation lifecycle state.
`resolve` performs the same actor-validated lifecycle transitions as the HTTP
API and records both structured activity and status history atomically.

When the review server is running, agents use the live HTTP client instead of
the offline commands so browser and agent writes share one revision authority:

```sh
code-annotator agent queue --url http://127.0.0.1:54321
code-annotator agent resolve --url http://127.0.0.1:54321 \
  --document README.md --revision <revision> --id ann_... \
  --status applied --role agent --author codex --summary "Implemented request"
```

The client accepts only a loopback viewer URL, obtains the temporary review
token from that viewer, sends the required `If-Match` revision, and never opens
annotation sidecars.

### Browser regression tests

Install the pinned development dependency and run the real-browser suite with
Google Chrome:

```sh
npm ci
npm run test:browser
```

These tests start an isolated review server and cover Mermaid rendering,
diagram selection, annotation mutation workflows, stale-anchor reattachment,
and optimistic-concurrency conflicts. Node.js and Playwright are development
dependencies only; released `code-annotator` binaries remain self-contained.

### Agent skill

The executable and agent skill are separate artifacts. Installing the skill
does not install or bundle the `code-annotator` binary. A working agent handoff has
two setup steps:

1. Make the API client available by installing or building `code-annotator` and
   placing it on `PATH`. When working in this source repository, agents can use
   `go run ./cmd/code-annotator` instead.
2. Make the `code-annotator` skill available to the agent, either through
   repository-local discovery or installation with the Skills CLI.

The repository includes the `code-annotator` skill under
`.agents/skills/`. Agents working from this repository can discover it without
installation. Use the Skills CLI to install it from a local checkout for the
current project:

```sh
npx skills add . --skill code-annotator -y
```

Install it for personal use across projects with:

```sh
npx skills add . --skill code-annotator -g -y
```

After the repository is published, the same convention supports installation
without a checkout:

```sh
npx skills add <owner>/<repository> --skill code-annotator -g -y
```

The Skills CLI discovers the skill directly from `.agents/skills/`.

## Documentation

- [MVP design](docs/designs/mvp.md)
- [Annotation review design](docs/designs/annotations.md)
- [Code review and Git diff design](docs/designs/code-review.md)
- [Architecture](docs/architecture.md)
- [Build and run](docs/build.md)
- [Project status](project_status.md)
- [Prebuilt binaries](dist/README.md)

## Development status

The manual-refresh MVP, annotation review workflow, agent handoff, embedded
Mermaid rendering, and code review with side-by-side Git diff comparison are
implemented and browser-tested. Saving a Markdown file and refreshing the
browser reads and renders the latest contents from disk. See
[`project_status.md`](project_status.md) for release readiness and the planned
live-reload milestone.
