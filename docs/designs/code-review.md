# Code review and Git diff design

**Status:** Proposed

**Target:** Post-Mermaid review milestone, before live reload

**Scope:** Local source-file review with optional Git comparison

## Summary

`md-viewer` can support code review without creating a second annotation
system. Source files are rendered as escaped, line-oriented text whose current
file bytes carry the same source-position metadata used by Markdown review.
Annotations continue to bind to the selected file's SHA-256 digest, exact quote,
UTF-8 byte range, and line range. Existing sidecar persistence, concurrency,
lifecycle, discussion, agent queue, and stale-anchor resolution remain shared.

The first diff view is a **current-file overlay** rather than a fully symmetric
two-revision editor. The complete current file remains the selectable source of
truth. Git adds old/new line-number gutters, change styling, and deleted rows as
read-only context. This permits annotations on current text with the existing
anchor model. Direct annotations on deleted text are deferred because deleted
bytes do not exist in the current file.

## Goals

- Discover an explicit, configurable set of source-file extensions.
- Filter the document sidebar to files changed from the configured Git base.
- Render supported text source safely with stable line numbers and byte ranges.
- Reuse the complete annotation workflow for source files.
- Compare the current worktree with `HEAD`, a local branch, a tag, a commit, or
  a local remote-tracking ref such as `origin/main`.
- Record the immutable Git comparison context seen when an annotation is made.
- Keep Git access read-only, bounded, shell-free, and offline.
- Preserve the current single-binary runtime and embedded browser assets.
- Keep Markdown rendering and annotations behavior unchanged.

## Non-goals

- Editing source files in the browser.
- Applying patches, staging files, committing, fetching, pulling, or pushing.
- Automatically contacting a Git remote.
- Comparing two arbitrary historical trees in the first slice.
- Annotating deleted/base-side lines in the first slice.
- Language-server features, compilation, semantic navigation, or diagnostics.
- Syntax highlighting in the first slice.
- Reviewing binary files, generated archives, or invalid UTF-8 source.
- Replacing dedicated repository review products.

## User experience

### File view

Supported code files appear in the existing document sidebar and lookup. A
small type label distinguishes Markdown and source files. Opening a source file
shows:

- its slash-separated relative path;
- a `File` view tab;
- a `Changes` tab when Git comparison is configured;
- stable line numbers;
- escaped source text in a monospace block;
- existing annotation highlights and the existing review sidebar.

Selections can span any number of adjacent current-file lines. Line numbers and
diff gutters are not selectable. The annotation preview continues to display
the server-verified source quote and byte range.

### Changes view

The first Changes view uses a **unified layout**, not two side-by-side panes.
Deleted base rows and current rows are interleaved in one vertical stream with
old and new line-number gutters. This fits the existing single document column,
remains usable when either sidebar is open, and gives current-side annotations
one unambiguous rendered surface.

A side-by-side layout is deferred. It would require synchronized scrolling,
responsive pane behavior, duplication or mapping of unchanged regions, and a
clear selection policy for the read-only base pane. Adding it later would be a
presentation option over the same parsed `FileDiff`; it would not change the
initial unified diff or current-side anchor model.

The Changes view renders the complete current file, not only Git hunks. Current
lines retain their exact source ranges and receive one of these visual states:

| State | Meaning | Selectable |
| --- | --- | --- |
| unchanged | Current line is unchanged from the base | Yes |
| added | Current line has no corresponding base line | Yes |
| modified | Current line replaces nearby base content | Yes |
| deleted context | Base text was removed before this current position | No |

Deleted rows have an old line number, no new line number, and a clear read-only
appearance. A selection that touches a deleted row is rejected with a concise
message. Reviewers can instead annotate a neighboring current line or create a
file-level annotation.

```text
 old  new
  18   18    unchanged current line
  19    -  - deleted base-side text       (read-only)
   -   19  + replacement current text     (selectable)
  20   20    unchanged current line
```

Annotations created in Changes view appear in File view because both views
anchor to the same current bytes. Existing annotations also appear in Changes
view when their current anchor resolves.

### Changed-files filter

When `--diff-base` is configured, the document sidebar adds a `Changed only`
toggle next to the existing path lookup. Enabling it shows reviewable files
whose current worktree content differs from the frozen base commit, including
untracked reviewable files. Disabling it restores the complete catalog without
discarding the path lookup text. The two filters compose: a file must match the
path lookup and the changed-files filter when both are active.

The toggle is hidden when Git comparison is not configured. An empty result
shows a clear message instead of an empty navigation region. The active file
remains visible until the reviewer opens another result; filtering the list
must not unexpectedly navigate away from the document being reviewed.

## Command-line contract

Code discovery is opt-in so the existing Markdown-only behavior remains the
default.

```text
--include-code
    Include the default supported source extensions.

--code-extensions <csv>
    Replace the default source extension set. Values require a leading dot and
    are matched case-insensitively.

--exclude-dirs <csv>
    Add directory base names excluded from recursive discovery.

--diff-base <revision>
    Enable Changes view against one locally available Git revision. The
    revision is resolved to an immutable commit at startup.
```

Initial default source extensions:

```text
.go,.cs,.js,.jsx,.mjs,.cjs,.ts,.tsx,.json,.csproj
```

Initial default excluded directories when code discovery is enabled:

```text
node_modules,vendor,bin,obj
```

Hidden directories retain the current exclusion behavior. User-provided
exclusions extend, rather than replace, the defaults. A later option may permit
explicitly re-including a default exclusion if a real use case requires it.

Examples:

```sh
# Review source without Git decorations.
md-viewer --review --include-code .

# Review the worktree against the last commit.
md-viewer --review --include-code --diff-base HEAD .

# Review the worktree against the parent of the current commit.
md-viewer --review --include-code --diff-base 'HEAD~1' .

# Review the worktree against a local branch.
md-viewer --review --include-code --diff-base main .

# Review against an already-fetched remote-tracking branch.
md-viewer --review --include-code --diff-base origin/main .

# Limit discovery to Go and C# and add a generated-code exclusion.
md-viewer --review --include-code \
  --code-extensions .go,.cs --exclude-dirs generated \
  --diff-base origin/main .
```

`--code-extensions` implies `--include-code`. The revision may be a full commit
ID or any locally resolvable Git revision expression, including `HEAD~1`, a
branch, a tag, or a remote-tracking ref. `--diff-base` requires a Git worktree
containing the selected content root. It never runs `git fetch`.

The first version always compares the configured base commit with the current
worktree. Comparing two historical targets, the index alone, or staged changes
alone is deferred until concrete workflows justify additional flags.

## Content catalog

The current `content.Index` is Markdown-specific. It becomes a catalog of
reviewable files while retaining the existing safe-root boundary:

```go
type Kind string

const (
    KindMarkdown Kind = "markdown"
    KindCode     Kind = "code"
)

type Document struct {
    Path      string
    Name      string
    Directory string
    Kind      Kind
    Language  string
}
```

The catalog owns the configured extension set and excluded directory names.
HTTP and annotation handlers ask the catalog whether a path is reviewable
instead of repeating extension checks. `Root.ReadFile` remains the common
containment- and size-checked reader.

Markdown remains preferred for the default route: root `README.md`, then the
first Markdown file, then the first supported source file.

The sidebar and agent queue consume the same catalog snapshot. Unsupported
files remain available only through the existing safe asset route and cannot
acquire annotation sidecars.

## Source rendering

The renderer dispatches by catalog kind:

```go
type RenderRequest struct {
    Path   string
    Kind   content.Kind
    Source []byte
    Review bool
    Diff   *gitdiff.FileDiff
}
```

Markdown continues through goldmark. The initial code renderer:

1. Rejects invalid UTF-8 and NUL-containing input.
2. Splits source into logical lines without normalizing the original bytes.
3. HTML-escapes every source character.
4. Emits one row per current line.
5. Wraps only the visible line content in `.source-text` with exact byte
   offsets; line terminators remain source gaps between spans.
6. Adds non-selectable old/new line-number gutters and diff markers.
7. Inserts escaped deleted rows without `.source-text` metadata.

Keeping CRLF or LF terminators outside source spans prevents browser newline
normalization from corrupting byte calculations. A multi-line selection may
cross those invisible source gaps; the server derives the authoritative quote
from the submitted byte endpoints exactly as it does for Markdown formatting
delimiters.

Syntax highlighting is deliberately deferred. A future highlighter must retain
one outer source-backed line span even if it adds token elements beneath it, and
the browser highlight implementation must be updated to handle nested text
nodes before highlighting is enabled.

The existing 4 MiB document limit applies initially. Diff output receives a
separate bounded allowance because a patch may contain both deleted and current
text.

## Git comparison model

A new `internal/gitdiff` package owns all Git interaction. Server and renderer
packages consume typed results and never parse Git output themselves.

```go
type Config struct {
    // RepositoryRoot is the absolute root returned by Git for the worktree.
    RepositoryRoot string

    // ContentPrefix locates the viewer content root relative to RepositoryRoot
    // using slash-separated Git paths.
    ContentPrefix string

    // RequestedBase preserves the entered revision for display, such as
    // "HEAD~1" or "origin/main".
    RequestedBase string

    // BaseCommit is the immutable full commit SHA resolved at startup.
    BaseCommit string
}

type FileDiff struct {
    // Path is the current file path relative to the viewer content root.
    Path string

    // BasePath is the corresponding base path. It may differ once rename
    // support is introduced.
    BasePath string

    // BaseCommit identifies the exact snapshot used by this comparison.
    BaseCommit string

    // Rows is the complete display sequence, including unchanged current
    // lines and inserted read-only deleted context.
    Rows []Row
}

type Row struct {
    // Kind identifies unchanged, added, modified, or deleted context.
    Kind RowKind

    // OldLine is the one-based base line, or zero when no base line exists.
    OldLine int

    // NewLine is the one-based current line, or zero for deleted context.
    NewLine int

    // CurrentStart and CurrentEnd are byte offsets into current source. Both
    // are zero for a deleted-context row.
    CurrentStart int
    CurrentEnd   int

    // DeletedText contains base text only for deleted context. Rendering must
    // escape it before inserting it into HTML.
    DeletedText string
}
```

At startup the package:

1. Finds the containing worktree with `git rev-parse --show-toplevel`.
2. Verifies that the selected content root is inside that worktree.
3. Rejects a revision beginning with `-` or containing NUL/newline characters.
4. Resolves the requested base with `git rev-parse --verify --end-of-options
   <revision>^{commit}`.
5. Stores the resulting full commit SHA for the server lifetime.

Freezing the base commit prevents a moving branch or remote-tracking name from
silently changing the review comparison while the server is running. Restarting
the viewer intentionally refreshes that base.

For each Changes request, the package obtains a zero-context patch equivalent
to:

```text
git -C <repository> diff \
  --no-ext-diff --no-textconv --unified=0 \
  <base-commit> -- :(literal)<relative-path>
```

Commands use `exec.CommandContext` with individual arguments, never a shell.
Literal pathspec magic prevents wildcard interpretation. Environment variables
that enable external diff drivers are not honored. Output and execution time
are bounded. Git stderr is logged in a sanitized terminal diagnostic but is not
returned to the browser.

The parser accepts ordinary textual unified patches and validates every hunk
range against the current source line count. Malformed, oversized, binary, or
unsupported patches produce an unavailable Changes view while File view remains
usable.

### Tracked and untracked files

`git diff <base>` covers staged and unstaged changes to tracked files. Git does
not include untracked files. The package checks tracking status separately; an
untracked source file is represented as entirely added against an empty base.

The changed-files sidebar filter uses one repository-level status query rather
than executing Git once per catalog entry. Its result is intersected with the
safe reviewable catalog and content prefix. Modified, added, deleted, renamed,
copied, type-changed, conflicted, and untracked paths count as changed. A
deleted path with no current file is not reviewable in the first version and
therefore does not appear. Ignored files remain excluded.

Rename-aware base paths are useful but not required in the first slice. A
renamed current path may initially appear as a delete/add comparison. Full
rename metadata can be added after the basic anchor and diff workflows are
verified.

## Annotation model

The existing `Source` is already generic at the JSON level: it identifies a
revision digest and a selected UTF-8 range. Comments and validators referring
specifically to Markdown are generalized to “reviewable document.”

`ValidateDocumentPath` stops enforcing `.md`; handlers instead require the path
to be present in the configured reviewable catalog. Sidecars continue to mirror
the complete relative path:

```text
internal/server/server.go
-> .md-viewer/annotations/internal/server/server.go.json
```

Diff-created annotations receive optional context:

```json
{
  "reviewContext": {
    "kind": "git_diff",
    "requestedBase": "origin/main",
    "baseCommit": "0123456789abcdef0123456789abcdef01234567",
    "target": "worktree"
  }
}
```

This field is annotation metadata, not part of anchor resolution. The source
digest still binds the selection to the exact current file. `baseCommit` is the
authoritative comparison identity; `requestedBase` is retained only for human
context.

The optional field is backward-compatible with schema version 1. Older writers
already preserve unknown fields during supported-schema rewrites. The typed
model, validator, merge field sets, API responses, CLI export, and agent skill
must all be updated together before the browser can create this metadata.

### Deleted-line annotations

Deleted rows contain bytes from a Git base blob, not the current file. Reusing
`Source` for them would make digest validation and stale resolution incorrect.
They are therefore non-selectable in the first version.

A future deleted-line anchor requires a distinct model containing at least:

- base commit SHA;
- base path, including rename handling;
- base blob digest;
- base byte and line range;
- exact quote and context.

That extension should be separately designed and schema-versioned.

## HTTP behavior

The existing routes remain stable:

| Route | Code-review behavior |
| --- | --- |
| `GET /` | Catalog Markdown and configured source files. |
| `GET /view/{path}` | Dispatch Markdown or code rendering by catalog kind. |
| `GET /view/{path}?mode=diff` | Render current-file Changes view when configured. |
| annotation APIs | Accept only paths in the reviewable catalog. |
| agent queue | Traverse Markdown and code sidecars in catalog order. |

No endpoint accepts an arbitrary Git revision. The base revision is startup
configuration, resolved once, and exposed read-only in page metadata. This
avoids turning the browser into a general Git command surface.

The File/Changes links preserve the selected path. Annotation mutations retain
the existing origin token, review token, content type, body limit, document
digest, and `If-Match` sidecar revision requirements.

## Browser selection and highlighting

Current code lines use the existing `.source-text` contract. The browser needs
three contained changes:

- permit a selection whose endpoints are current lines in the same code view;
- reject a range whose cloned contents contain `.diff-deleted`;
- map highlights across multiple line spans and source terminator gaps.

The selection preview labels diff-created ranges as “current file against
<requested base>.” The payload adds only the optional review context; byte
positions and document digest remain unchanged.

Deleted text, line numbers, diff markers, toolbar controls, and syntax chrome
use `user-select: none`. Keyboard selection remains available within current
source content.

## Agent handoff

The queue and document endpoints include code annotations without a new API.
Agent output adds:

- document kind and language;
- optional Git base name and immutable commit;
- current anchor state against current worktree bytes.

The agent skill instructs agents to treat `baseCommit` as comparison context,
modify only the current file, and use the live API while the server runs. An
agent never invokes Git through the viewer API and never edits sidecars.

## Security and operational limits

- Code source is always escaped; raw HTML remains disabled.
- Only configured extensions in the safe content catalog are reviewable.
- Existing lexical and symlink containment checks remain mandatory.
- Default dependency/build directory exclusions prevent accidental traversal
  of very large trees.
- Git is read-only and invoked without a shell or external diff/textconv tools.
- Base revisions are validated and resolved to commits at startup.
- Remote-tracking refs are local data; the viewer performs no network fetch.
- Repository, content root, patch output, execution time, file size, and UTF-8
  validity are checked before rendering.
- Absolute repository paths and raw Git errors are not exposed over HTTP.
- CSP, loopback binding, review tokens, origin checks, and optimistic sidecar
  concurrency remain unchanged.

Initial limits proposed for implementation:

| Resource | Limit |
| --- | --- |
| Current source file | 4 MiB |
| Git patch output per request | 8 MiB |
| Git command duration | 5 seconds |
| Configured source extensions | 64 |
| Configured excluded directory names | 128 |

## Failure behavior

- Outside a Git worktree plus `--diff-base`: startup fails with a concise error.
- Invalid or missing base revision: startup fails before opening the browser.
- Git comparison failure for one file: File view remains available and Changes
  view shows a non-sensitive diagnostic.
- Binary or invalid UTF-8 source: return an unsupported-text response; never
  place raw bytes into HTML.
- File changes between render and annotation submission: existing document
  digest validation returns `409` and requires reselection.
- Sidecar changes concurrently: existing `If-Match` handling returns `409` and
  reloads authoritative annotations.

## Package changes

```text
internal/content/          configurable reviewable-file catalog and kinds
internal/render/           Markdown dispatcher plus escaped code renderer
internal/gitdiff/          Git discovery, revision resolution, execution, parser
internal/annotation/       generalized paths and optional review context
internal/server/           kind dispatch and fixed-base Changes mode
internal/commands/         code-aware queue/export output
web/                       code rows, diff toolbar, selection rules, styling
browser-tests/             code and diff interaction coverage
```

## Test strategy

All Go cases remain table-driven.

### Unit tests

- Extension normalization, default exclusions, and default-document choice.
- Code escaping, UTF-8 byte ranges, LF/CRLF gaps, tabs, and final lines without
  terminators.
- Git revision validation and immutable SHA resolution.
- Unified-hunk parsing for additions, deletions, replacements, empty files,
  untracked files, malformed ranges, oversized output, and timeouts.
- Catalog-based annotation path validation and sidecar mapping.
- Optional review-context validation and unknown-field preservation.

### Handler and command tests

- Markdown behavior remains unchanged when code discovery is disabled.
- Code routes reject unsupported paths and binary/invalid UTF-8 content.
- Diff mode is unavailable without startup configuration.
- Agent queue/export includes code kind and immutable base context.
- Document and sidecar conflicts retain current `409` behavior.

### Browser tests

- Source-file lookup, line numbers, safe escaping, and multi-line selection.
- Changed-only filtering, composition with path lookup, untracked files, and
  empty-result behavior.
- Existing annotation creation, highlighting, replies, lifecycle, and filtering
  on code files.
- Added/modified/deleted styling and line-number mapping.
- Deleted-row selection rejection.
- Diff annotation context sent and rendered correctly.
- File/Changes navigation, narrow layouts, sidebars, and dark mode.
- No external requests or CSP violations.

## Implementation slices

Each slice should be independently reviewable:

1. Generalize product terminology and add configurable code catalog options.
2. Render safe plain source with line numbers, without annotations.
3. Generalize annotation path validation and enable code-file annotations.
4. Extend queue, CLI export, and agent skill for code documents.
5. Add bounded Git repository discovery, immutable base resolution, and the
   changed-file catalog filter.
6. Parse Git patches into validated current/deleted row metadata.
7. Add File/Changes UI with current-side annotation selection.
8. Persist and expose optional immutable diff review context.
9. Add Go and browser integration coverage.
10. Update documentation, run release verification, and refresh distributions.
11. Stop for maintainer review before live reload.

## Decisions requested before implementation

1. Approve current-side annotations only; deleted rows are read-only context.
2. Approve comparison of one startup base commit to the current worktree only.
3. Approve opt-in code discovery with the proposed default extensions.
4. Approve default exclusions for `node_modules`, `vendor`, `bin`, and `obj`.
5. Approve plain escaped source first and defer syntax highlighting.
6. Approve optional immutable Git review context without a schema-version bump.
7. Approve a changed-only sidebar toggle when Git comparison is configured.

These boundaries produce a useful code-review system while preserving the
existing annotation model and keeping Git exposure narrow.
