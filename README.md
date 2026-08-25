# code-annotator

`code-annotator` is a local Go web application for reviewing Markdown and
source code from a directory. It starts a server on the loopback interface,
renders safe HTML, and opens the viewer in the user's default browser. Beyond
plain Markdown viewing, it supports side-by-side Git diff comparison and a
full annotation review workflow, with browser and offline CLI tooling for
human and AI-agent handoff, including a bundled `code-annotator` agent skill
that teaches AI agents how to discover, interpret, and respond to
annotations.

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

HTMX 2.0.10 is pinned, licensed, embedded, and loaded only on review pages for
the server-rendered annotation UI. Provenance and checksums are recorded in
[`web/vendor/htmx/README.md`](web/vendor/htmx/README.md). The initial page and
every annotation read or mutation render the same escaped panel/card/action Go
templates under `web/templates/`. Small TypeScript adapters retain only native
selection mapping, highlights, source navigation, panel controls, lifecycle
field state, and the authenticated HTMX headers. The stable JSON API remains
available for agents and other automation.

Custom `data-*` attributes are migration debt rather than a browser-state
contract. Review pages now activate the versioned `/ui/viewer-state` boundary:
`web/src/viewer-state.ts` validates unknown wire data before returning strongly
typed source, diagram, annotation, lifecycle, revision, and digest state.
Rendered review content carries semantic IDs only; TypeScript joins those IDs
to typed in-memory maps. The remaining allowlisted attributes belong only to
the document tree and comparison selector, which have separate review gates.
The same migration rule applies when no custom attribute is involved: rendered
nodes, classes, text, links, and visibility are presentation rather than an
application-state store. Remaining document-tree, comparison, and smaller view
adapter violations are explicitly tracked in the milestone design.
An inactive `/ui/document-state` foundation now exposes the complete catalog,
selected document, File/Changes mode, changed state, navigation URLs, and open
comment counts as runtime-validated TypeScript state. Pure DOM-free functions
build and filter that catalog; commit 8B will activate them in the viewer.

## Usage

Run directly from the repository:

```sh
go run ./cmd/code-annotator ./docs
```

Or build and run the binary:

```sh
npm ci
npm run check:web
go build -o bin/code-annotator ./cmd/code-annotator
./bin/code-annotator ./docs
```

`npm run check:web` typechecks the authored TypeScript and compiles the Sass
under `web/src/`, then regenerates the checked-in browser assets under
`web/generated/`. Node.js and
npm are required for source and release builds, but are not runtime
dependencies of the compiled binary.

### Frontend development commands

The repository uses npm for the TypeScript frontend and browser regression
tests:

| Command | Purpose | When to use it |
| --- | --- | --- |
| `npm ci` | Removes `node_modules` and installs the exact versions from `package-lock.json`. | Clean local setup, CI, or before a reproducible build. |
| `npm install` | Resolves dependencies and updates `package-lock.json` when dependencies change. | Adding or intentionally updating a package. |
| `npm run typecheck` | Checks all TypeScript without writing generated files. | Fast feedback while editing frontend code. |
| `npm run test:unit` | Runs co-located `web/src/**/*.test.ts` tests with Vitest. | Fast coverage for pure TypeScript rules and simple DOM adapters. |
| `npm run build:styles` | Compiles `web/src/styles.scss` and its partials into `web/generated/styles.css`. | Regenerating the embedded stylesheet. |
| `npm run build:web` | Compiles TypeScript and Sass into `web/generated/`. | Regenerating all embedded browser assets. |
| `npm run format:styles` | Formats the Sass entrypoint and partials with Prettier. | Normalizing stylesheet source before review. |
| `npm run watch:web` | Continuously compiles TypeScript when source files change. | Frontend development while the Go viewer is running. |
| `npm run watch:styles` | Continuously compiles Sass when stylesheet files change. | Styling development while the Go viewer is running. |
| `npm run check:web` | Runs `typecheck`, `test:unit`, then `build:web`. | Standard frontend validation before Go builds or commits. |
| `npm run test:browser` | Runs the Playwright browser regression suite. | Verifying UI behavior with Microsoft Edge available. |

After changing TypeScript, use:

```sh
npm run check:web
go test ./...
```

`go build` does not invoke npm automatically. The release script runs
`npm run check:web` before testing and building the distribution binaries.

For frontend development, run the TypeScript and Sass watchers in separate
terminals, with the Go viewer in a third:

```sh
npm run watch:web
npm run watch:styles
go run ./cmd/code-annotator ./docs
```

The Go server reads generated assets at startup, so restart the Go process after
a frontend change before refreshing the browser.

Prebuilt binaries are available under [`dist/`](dist/):

| Platform | Architecture          | Binary                                                                           |
| -------- | --------------------- | -------------------------------------------------------------------------------- |
| macOS    | arm64 (Apple silicon) | [`dist/darwin-arm64/code-annotator`](dist/darwin-arm64/code-annotator)           |
| macOS    | amd64 (Intel)         | [`dist/darwin-amd64/code-annotator`](dist/darwin-amd64/code-annotator)           |
| Linux    | arm64                 | [`dist/linux-arm64/code-annotator`](dist/linux-arm64/code-annotator)             |
| Linux    | amd64                 | [`dist/linux-amd64/code-annotator`](dist/linux-amd64/code-annotator)             |
| Windows  | arm64                 | [`dist/windows-arm64/code-annotator.exe`](dist/windows-arm64/code-annotator.exe) |
| Windows  | amd64                 | [`dist/windows-amd64/code-annotator.exe`](dist/windows-amd64/code-annotator.exe) |

On macOS or Linux, make the selected binary executable if needed, then run it
with a directory argument exactly like the source and `bin/` examples above.
The example below uses `darwin-arm64`; substitute the path for the row that
matches your platform from the table:

```sh
chmod +x dist/darwin-arm64/code-annotator
./dist/darwin-arm64/code-annotator .
```

Regenerate every platform binary after a source update with:

```sh
./scripts/build-dist.sh
```

See [`dist/README.md`](dist/README.md) for the release layout and build details.

To install a single binary directly from the remote repository without a full
checkout, download it from `dist/` on GitHub and make it executable. Use the
`raw.githubusercontent.com` host, not a `github.com/.../blob/...` page: the
`blob` URL is GitHub's HTML file viewer, not the raw binary, and downloading
it produces an unrunnable HTML file instead of an executable.

```sh
curl -LO https://raw.githubusercontent.com/atulmirajkar/code-annotator/main/dist/darwin-arm64/code-annotator
chmod +x code-annotator
./code-annotator .
```

Again, substitute the platform path for your own from the table above.

Verify the download against [`dist/SHA256SUMS`](dist/SHA256SUMS) before
running it:

```sh
curl -s https://raw.githubusercontent.com/atulmirajkar/code-annotator/main/dist/SHA256SUMS | grep darwin-arm64/code-annotator
shasum -a 256 code-annotator
```

The two hashes must match. If they do not, or if the download is small and
`file code-annotator` reports `HTML document` instead of an executable
format, the `blob` URL was used by mistake; re-download with the `curl`
command above.

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
reviewers dismiss open work, close applied work, reopen, or request more
changes. Each card also accepts inline
discussion replies without changing its lifecycle status. The creation form can attach
a new annotation to the current selection or the whole document, and annotation
cards preview their selected source and line range. If the document changes
while a selected comment is being submitted, the comment is still saved and
marked for reattachment instead of being lost. Resolved selections are highlighted in the document;
stale and document-level annotations remain panel-only. Closed and rejected
annotations and their highlights are hidden by default and can be restored with
the panel's history toggle. A stale text annotation can be reattached by
selecting its replacement text and using the action on its card. By default, sidecars are stored under
`<content-root>/.code-annotator/annotations/`. To select another location:

```sh
code-annotator --review --annotations-dir ./reviews ./docs
```

Internally, creation, replies, lifecycle changes, and stale-anchor reattachment
are shared by the agent-facing JSON API and the server-rendered review
fragments, so both transports enforce the same roles, transitions, selection
verification, optimistic revision checks, and append-only history.

List annotations for agents or local tooling without starting the server:

```sh
code-annotator annotations list --root ./docs --status open,needs_changes
code-annotator annotations export --root ./docs --status open,needs_changes
code-annotator annotations reply --root ./docs --id ann_... \
  --role reviewer --message "Please retain the default."
code-annotator annotations resolve --root ./docs --id ann_... \
  --status applied --role agent \
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
`resolve` performs the same role-validated lifecycle transitions as the HTTP
API and records both structured activity and status history atomically.

When the review server is running, agents use the live HTTP client instead of
the offline commands so browser and agent writes share one revision authority:

```sh
code-annotator agent queue --url http://127.0.0.1:54321
code-annotator agent resolve --url http://127.0.0.1:54321 \
  --document README.md --revision <revision> --id ann_... \
  --status applied --role agent --summary "Implemented request"
```

The client accepts only a loopback viewer URL, obtains the temporary review
token from that viewer, sends the required `If-Match` revision, and never opens
annotation sidecars.

An agent that does not already have a URL can discover a running `--review`
server instead of asking a human for one:

```sh
code-annotator agent discover
code-annotator agent discover --root ./docs
```

A single running, matching server prints its URL as JSON and exits `0`. If
multiple servers are running and `--root` was not given, discovery prefers
whichever one's content root contains the caller's own working directory;
zero matches, or multiple matches working directory can't resolve, exit
non-zero with a message explaining why, so the caller can retry with `--root`
or fall back to asking. Discovery verifies each candidate against its own
`/healthz` route rather than scanning ports, and prunes entries left behind
by a server that exited without cleaning up after itself (for example, a hard
kill). Only `--review` servers register.

A `--review` server advertises itself by writing one file per running
instance at `<state-dir>/servers/<pid>.json`, removed on clean shutdown.
`<state-dir>` defaults to a per-user configuration directory and can be
overridden with `CODE_ANNOTATOR_STATE_DIR`. The registry file records only
the URL, content root, process ID, and start time; it never contains the
review mutation token, which remains readable only from the served loopback
page. See [`docs/designs/server-discovery.md`](docs/designs/server-discovery.md)
for the full design.

### Browser regression tests

Install the pinned development dependencies, compile the frontend, and run the
real-browser suite with Microsoft Edge:

```sh
npm ci
npm run check:web
npm run test:browser
```

The suite defaults to the `msedge` Playwright channel. Override it with
`CODE_ANNOTATOR_BROWSER_CHANNEL` when testing another installed browser.

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

The normal skill workflow processes the current queue once. If an agent runtime
supports waiting and the user asks it to monitor for later comments, the skill
also includes this optional read-only queue watcher:

```sh
.agents/skills/code-annotator/scripts/poll-agent-queue.sh \
  --root . --interval 30
```

It discovers the loopback server once and prints only changed queue JSON on
stdout. The installed `code-annotator` executable and `jq` must be on `PATH`.
Use `--url` to skip discovery or `--once` when an external scheduler owns the
cadence; the watcher never processes or mutates annotations.

The repository also includes a developer-only regression test for this helper:

```sh
./scripts/test-poll-agent-queue.sh
```

The test puts a fake `code-annotator` on `PATH` and verifies discovery, retry
behavior, ETag carry-forward, unchanged-poll suppression, and `--once`. It is
not needed to run the skill or the watcher in production; run it after editing
the polling script or its CLI contract.

After the repository is published, the same convention supports installation
without a checkout:

```sh
npx skills add atulmirajkar/code-annotator --skill code-annotator -g -y
```

The Skills CLI discovers the skill directly from `.agents/skills/`.

## Documentation

- [MVP design](docs/designs/mvp.md)
- [Annotation review design](docs/designs/annotations.md)
- [Code review and Git diff design](docs/designs/code-review.md)
- [Server-rendered review UI and testable TypeScript design](docs/designs/server-rendered-review-ui.md)
- [Agent server discovery design](docs/designs/server-discovery.md)
- [Architecture](docs/architecture.md)
- [Build and run](docs/build.md)
- [Project status](project_status.md)
- [Prebuilt binaries](dist/README.md)

## Development status

The manual-refresh MVP, annotation review workflow, agent handoff, embedded
Mermaid rendering, and code review with side-by-side Git diff comparison are
implemented and browser-tested. The annotation panel now uses server-rendered
HTML fragments and HTMX instead of reconstructing cards and mutation forms in
the browser. The remaining staged frontend simplification is proceeding one
reviewed commit at a time. See the
[approved design](docs/designs/server-rendered-review-ui.md) and
[`project_status.md`](project_status.md) for the exact reviewed commit gate,
release readiness, and subsequent milestones.
