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

The first diff view is **side by side**, with the immutable Git base on the left
and the complete current file on the right. The current file remains the only
selectable source of truth. This permits annotations on current text with the
existing anchor model. Direct annotations on base-only text are deferred
because those bytes do not exist in the current file.

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

The Changes view uses a **side-by-side layout**. The immutable base is always on
the left and the complete current worktree file is always on the right. Each
display row aligns corresponding base and current lines; a missing side renders
an empty, non-selectable cell. The right pane is the only annotation surface.

Both panes participate in the same vertical document flow, and every displayed
line is a fixed-height row, so aligned rows cannot drift. Each equal-width pane
has its own horizontal scroll container. Long base text therefore remains
clipped to and scrollable within the left pane, while long current text behaves
the same way on the right; neither may paint across the center divider. Change
backgrounds extend through the complete scrollable width of their line. On
narrow screens the panes remain side by side instead of stacking or swapping
their order. Collapsing either application sidebar gives both panes more room
without changing base-left/current-right semantics.

The Changes view renders the complete current file, not only Git hunks. Current
lines retain their exact source ranges and receive one of these visual states:

| State | Left/base cell | Right/current cell | Selectable |
| --- | --- | --- | --- |
| unchanged | Base line | Matching current line | Right only |
| added | Empty | Added current line | Right only |
| modified | Replaced base line | Replacement current line | Right only |
| deleted | Deleted base line | Empty | No |

The complete current file is therefore visible down the right. The left side
shows the corresponding complete base file for tracked paths. Base text, both
gutters, and empty cells are read-only and use `user-select: none`. A selection
that begins in or crosses the left pane is rejected. Reviewers can annotate a
replacement or neighboring current line, or create a file-level annotation.

```text
 BASE (left, read-only)             CURRENT (right, selectable)
 18  unchanged line                18  unchanged line
 19  old implementation            19  replacement implementation
 20  deleted line                   --  [empty]
 --  [empty]                        20  added line
 21  next unchanged line            21  next unchanged line
```

Within one replacement hunk, base deletions and current additions are paired in
order as `modified` rows. If their counts differ, remaining base lines become
`deleted` rows and remaining current lines become `added` rows. This is a
deterministic line alignment, not a claim that paired lines are semantically
equivalent. More sophisticated intra-line matching is deferred.

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
4. Emits one row per file line in File view and one aligned two-cell row per
   `FileDiff` row in Changes view.
5. Wraps only the visible line content in `.source-text` with exact byte
   offsets; review-mode empty lines receive a zero-length `.source-text`
   anchor, while line terminators remain source gaps between spans.
6. Adds non-selectable base/current line-number gutters and diff markers.
7. Emits escaped base text only in the left cell, without `.source-text`
   metadata; only right/current text receives source offsets.

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

    // Rows is the complete aligned display sequence for both panes.
    Rows []Row
}

type Row struct {
    // Kind identifies unchanged, added, modified, or deleted context.
    Kind RowKind

    // OldLine is the one-based base line, or zero when no base line exists.
    OldLine int

    // NewLine is the one-based current line, or zero when the right cell is
    // empty for a deletion.
    NewLine int

    // CurrentStart and CurrentEnd are byte offsets into current source. Both
    // are zero when the right/current cell is empty.
    CurrentStart int
    CurrentEnd   int

    // BaseText contains the left-pane base line when OldLine is nonzero.
    // Rendering must escape it before inserting it into HTML.
    BaseText string
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
  --no-color --no-ext-diff --no-textconv --unified=0 \
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

### File diff evaluation and row alignment

A file Changes request has three authoritative inputs:

1. `base`: the blob at `<BaseCommit>:<repository-relative-path>`, read through
   a bounded Git command;
2. `current`: the same cataloged file read through `content.Root`, preserving
   its exact current bytes;
3. `patch`: a bounded, zero-context textual Git diff from `BaseCommit` to the
   combined current index/worktree result for that literal path.

If the path has no blob at `BaseCommit`, but it is a current catalog member, the
base is empty and the entire current file is treated as added. This covers both
untracked files and tracked files introduced after the frozen base. A missing
current file never reaches this flow because it is absent from the current
catalog. Base, current, and patch must be UTF-8 text without NUL bytes; binary
markers and oversized inputs make Changes unavailable while File view remains
usable.

Git emits the patch in unified diff format. File metadata precedes a sequence
of hunk headers and line records:

```diff
diff --git a/main.go b/main.go
index 12ab34c..56de78f 100644
--- a/main.go
+++ b/main.go
@@ -2 +2 @@
-old value
+new value
@@ -10,0 +11,2 @@
+first added line
+second added line
```

The patch contains every textual hunk for the selected file relative to
`BaseCommit`. Changes separated by unchanged lines normally produce separate
hunks. It does not contain the whole file: with `--unified=0`, unchanged regions
are omitted and reconstructed from the separately loaded base and current
sources. File headers such as `diff --git`, `index`, `---`, and `+++` describe
the file but do not consume source lines.

Each hunk begins with:

```text
@@ -oldStart,oldCount +newStart,newCount @@ optional section text
```

Within a hunk, a leading space is unchanged context, `-` consumes one base
line, and `+` consumes one current line. Counts omitted from the header default
to one. A zero count describes an insertion or deletion boundary. Git may add a
`\ No newline at end of file` marker after an affected line; the marker is
metadata and consumes neither source.

The parser does not use a general-purpose line-diff algorithm after Git has
already produced hunks. It treats hunk coordinates as the change boundaries,
verifies hunk text against both source snapshots, and reconstructs the complete
display sequence as follows:

1. Split base and current bytes into logical lines without normalizing the
   source. Visible line text excludes LF and an optional preceding CR. A final
   line terminator does not create an extra display row.
2. Parse each header of the form
   `@@ -oldStart,oldCount +newStart,newCount @@`. An omitted count means one.
   For a zero-count range, `start` is the insertion position; otherwise the
   zero-based source index is `start - 1`.
3. Require hunks to be ordered, non-overlapping, and within both source line
   arrays. The number of old and new hunk records must exactly match the header.
4. Reconstruct the region omitted before each zero-context hunk. The omitted
   base and current regions must have equal line counts and identical text;
   each pair becomes an `unchanged` row.
5. Verify every `-` record against the next base line, every `+` record against
   the next current line, and any context record against both. Git's
   `\ No newline at end of file` marker affects neither text nor row count.
6. Within each contiguous change group, pair deleted and added lines by their
   order. A pair becomes `modified`; an unpaired added line becomes `added`;
   an unpaired deleted line becomes `deleted`. This is line alignment, not an
   assertion that the paired lines are semantically related or a character-level
   diff.
7. Verify and append the unchanged suffix after the final hunk. Every base and
   current line must be consumed exactly once.

For example:

```text
base                 current
1  one               1  one
2  old-a             2  new
3  old-b             3  tail
4  tail
```

produces:

| Kind | Base line/text | Current line/text |
| --- | --- | --- |
| unchanged | 1 `one` | 1 `one` |
| modified | 2 `old-a` | 2 `new` |
| deleted | 3 `old-b` | — |
| unchanged | 4 `tail` | 3 `tail` |

The row model stores base text because the left pane renders the immutable
blob. Current text is not duplicated in `Row`; the renderer slices it from the
already-loaded current source using `CurrentStart` and `CurrentEnd`. These are
byte offsets, not rune or browser UTF-16 offsets. For each non-deleted row they
cover visible content only, leaving CR/LF terminators as gaps just like File
view. Deleted rows have `NewLine == 0` and zero current offsets, making them
structurally non-selectable. Empty current lines may also have equal start/end
offsets, but remain distinguishable by their nonzero `NewLine`.

The parser rejects rather than approximates when a header is malformed, a hunk
overlaps another hunk, line counts disagree, hunk text does not match either
snapshot, omitted unchanged text differs, a textual difference has no hunk, or
the patch contains unsupported/binary data. This strict source consistency is
what allows current-side annotation byte ranges to retain the same validation
contract in File and Changes views.

### Tracked and untracked files

Changed-file discovery evaluates the current worktree against the immutable
`BaseCommit`; it does not evaluate only `HEAD` against the working directory.
This distinction means a clean file committed after an older configured base
is still changed for this review.

Git requires two bounded repository-level queries because a normal diff never
reports untracked files:

```text
# Tracked paths whose current index/worktree result differs from BaseCommit.
git -C <repository> diff \
  --name-only -z --no-renames --no-ext-diff --no-textconv \
  <base-commit> -- [<literal-content-prefix>]

# Untracked paths, excluding files ignored by normal Git rules.
git -C <repository> ls-files \
  --others --exclude-standard -z -- [<literal-content-prefix>]
```

The first query observes the combined current result: commits after the frozen
base, staged changes, unstaged changes, additions, deletions, type changes, and
conflicts. The second adds only untracked, non-ignored paths. Neither query
changes the index, obtains remote data, invokes text conversion, or executes an
external diff driver. `-z` makes paths unambiguous even when a valid filename
contains whitespace or a newline.

The content prefix is derived once from canonical roots. If the repository is
`/work/repository` and the viewer root is `/work/repository/docs`, Git path
`docs/design/view.go` becomes viewer path `design/view.go`; `internal/app.go`
is outside the content root and is discarded. When the viewer root equals the
repository root, the pathspec is omitted. Otherwise literal pathspec magic is
used so brackets, asterisks, and other characters in directory names cannot be
interpreted as patterns.

The evaluation pipeline is:

```text
tracked paths from BaseCommit
        + untracked non-ignored paths
        -> validate NUL-terminated repository paths
        -> remove the canonical content prefix
        -> normalize, deduplicate, and sort
        -> intersect with the current safe reviewable catalog
        -> mark matching sidebar entries as changed
```

Catalog intersection is an authorization and presentation boundary, not only
a file-extension filter. It removes unsupported assets, default-excluded or
hidden directories, unsafe symlinks, and paths that no longer resolve to a
regular current file. Consequently, a deleted path is detected by Git but does
not appear in the first-version sidebar because there is no current document to
open or annotate. An untracked supported source file does appear and its diff
is represented as entirely added against an empty base.

`--no-renames` deliberately makes the initial result deterministic without a
similarity threshold. A rename is evaluated as deletion of the old path plus
addition of the new path; catalog intersection removes the missing old path and
retains the current new path. Copy and richer rename metadata can be introduced
later without changing the definition of “changed against BaseCommit.”

Changed paths are evaluated once for each fresh catalog/page render, never once
per catalog entry. A browser refresh therefore observes new worktree changes;
live updates wait for the live-reload milestone. The path lookup and Changed
only toggle compose entirely over that one snapshot. Filtering never changes
the selected document or the frozen base.

Each Git query has its own three-second deadline and 64 KiB output limit in the
initial implementation. A timeout, oversized result, malformed path stream, or
Git failure makes changed-file discovery unavailable for that response. The
normal catalog and File view remain usable; the server must not silently label
every file unchanged, because that would misrepresent an incomplete query.
Browser-facing errors remain generic while terminal diagnostics may identify
the failed operation without exposing file contents.

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

No endpoint accepts an arbitrary Git revision string. The comparison API may
select only a full commit ID from a server-issued bounded option list, or
re-resolve the single revision supplied by `--diff-base`. This avoids turning
the browser into a general Git command surface.

The File/Changes links preserve the selected path. Annotation mutations retain
the existing origin token, review token, content type, body limit, document
digest, and `If-Match` sidecar revision requirements.

The browser stores the reviewer-selected File or Changes mode in session
storage and rewrites only code-document sidebar links accordingly. Selecting a
tab changes the preference; ordinary document rendering does not invent a new
mode. Collapsed document and annotation panels are stored independently in the
same tab-scoped storage and restored after navigation. Markdown links never
receive `mode=diff`.

The source toolbar visibly identifies the active comparison. Reloading the
browser alone does not re-resolve a moving name such as `HEAD`; only the
explicit Git refresh action below can change the active base.

## Revision selector and Git refresh

### User experience

When Git comparison is configured, the source toolbar contains a revision
selector, the active 12-character commit ID and truncated subject, and a
`Refresh Git diff` button. Hover text exposes the complete commit ID and
subject.

The selector contains two kinds of server-issued options:

1. The configured revision, such as `HEAD` or `origin/main`, which may move.
2. At most 50 recent local repository commits, identified by full object ID and
   labeled with an abbreviated ID plus a subject truncated to 72 characters.

Subjects are display-only untrusted text and remain HTML-escaped. Selecting a
listed commit changes the server-wide comparison base and reloads the current
page in its existing File/Changes mode. Markdown links remain unaffected.

`Refresh Git diff` performs a fresh bounded option lookup. If the configured
moving revision is active, it also re-resolves that revision and atomically
adopts its new commit. If an explicit commit is active, refresh retains that
exact commit and only refreshes the option list.

This distinction is intentional: refreshing active `HEAD` after an agent has
committed a clean worktree makes the diff empty, because the new base and
current file are identical. Selecting the previous commit displays the agent's
committed change again.

### Comparison state and concurrency

The server owns concurrency-safe comparison state rather than mutating a shared
`gitdiff.Config` pointer. Every changed-path or file-diff request takes one
immutable snapshot and uses it for the complete operation. A refresh or
selection builds and validates a replacement before acquiring the write lock,
then swaps it atomically. Failure leaves the previous snapshot usable.

Each snapshot has an opaque state revision. The selection endpoint accepts only
a full commit ID present in that snapshot's option list. A stale `If-Match`
revision returns `409`, preventing one browser tab from silently overwriting
another tab's selection.

### HTTP contract and security

| Route | Behavior |
| --- | --- |
| `GET /api/git-comparison` | Return active identity, state revision, and bounded selector options. |
| `POST /api/git-comparison` with `{"action":"refresh"}` | Refresh options and re-resolve the configured revision when active. |
| `POST /api/git-comparison` with `{"action":"select","commit":"<full SHA>"}` | Select a commit from the current option snapshot. |

Mutations require JSON, exact loopback `Origin`, a per-process comparison
control token, and quoted `If-Match` state revision. The token is distinct from
the agent annotation token and is exposed only to the browser page when Git
comparison is enabled. These controls work with or without annotation mode.

Selector options use a no-shell, no-prompt, bounded `git log --all --date-order
--max-count=50` invocation. NUL-framed object IDs and subjects make embedded
newlines unambiguous. Output remains bounded to 64 KiB and execution to three
seconds. Refresh resolves only the startup-configured revision using the
existing `--end-of-options` validation and never contacts a remote.

### Failure behavior

- Lookup or configured-ref resolution failure retains the old snapshot and
  shows a non-sensitive inline error.
- A commit absent from the current option snapshot returns `400`.
- An `If-Match` conflict returns `409` and reloads current state.
- An unreadable selected commit makes that file's Changes view unavailable
  without breaking File view.
- Browser refresh remains a page reload; only the explicit button runs Git
  refresh behavior.

## Browser selection and highlighting

Current code lines use the existing `.source-text` contract. The browser needs
three contained changes:

- permit a selection whose endpoints are right-pane current lines;
- reject endpoints or cloned contents from the left/base pane;
- map highlights across multiple line spans and source terminator gaps.

The selection preview labels diff-created ranges as “current file against
<requested base>.” The payload adds only the optional review context; byte
positions and document digest remain unchanged.

For line-oriented source, the browser constructs preview text exclusively from
current-side `.source-text` spans and inserts newlines for intervening display
rows. It must not use the raw DOM range text, because that includes line-number
and diff-marker siblings between multi-line endpoints. The server remains
authoritative and derives the stored quote from submitted current-file byte
offsets. A native boundary on an empty current row maps to that row's
zero-length source anchor, allowing a selection to end at the empty line's byte
position without treating its gutter as source.

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
| Changed-path output per Git query | 64 KiB |
| Changed-path Git command duration | 3 seconds |
| Git patch output per request | 8 MiB |
| Git patch command duration | 5 seconds |
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
- Changed-path discovery for committed-after-base, staged, unstaged, deleted,
  untracked, ignored, nested-content, malformed-output, and bounded-failure
  cases.
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
- Added/modified/deleted row alignment, styling, and line-number mapping.
- Left-pane and cross-pane selection rejection.
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
6. Parse Git patches into validated aligned base/current row metadata.
7. Add the side-by-side File/Changes UI with base left, current right, and
   current-side annotation selection.
8. Persist and expose optional immutable diff review context.
9. Add Go and browser integration coverage.
10. Update documentation, run release verification, and refresh distributions.
11. Stop for maintainer review before live reload.
12. Add bounded revision discovery and atomic comparison snapshots.
13. Add protected comparison state, selection, and refresh routes.
14. Add the selector/refresh UI and browser coverage.

## Decisions requested before implementation

1. Approve current-side annotations only; the left/base pane is read-only.
2. Approve one active server-wide base against the current worktree, selected
   only from bounded server-issued commits or the configured revision.
3. Approve opt-in code discovery with the proposed default extensions.
4. Approve default exclusions for `node_modules`, `vendor`, `bin`, and `obj`.
5. Approve plain escaped source first and defer syntax highlighting.
6. Approve optional immutable Git review context without a schema-version bump.
7. Approve a changed-only sidebar toggle when Git comparison is configured.
8. Approve side-by-side Changes view with base always left and current right.

These boundaries produce a useful code-review system while preserving the
existing annotation model and keeping Git exposure narrow.

## Implementation status and session handoff

Status as of 2026-08-21:

- Code discovery, source rendering, source annotations, agent handoff metadata,
  immutable Git-base resolution, changed-path discovery, and Changed-only
  filtering are complete.
- Bounded base-blob and patch retrieval, strict unified-patch parsing, complete
  aligned row reconstruction, and renderer validation are complete.
- `GET /view/{path}?mode=diff`, File/Changes tabs, base-left/current-right
  rendering, failure fallback, light/dark change colors, and independent
  horizontal pane scrolling are complete.
- Browser regressions verify that long lines stay inside their pane, both panes
  scroll independently, and the current-side change background covers its
  complete line. They also verify current-side annotation creation, restored
  highlights after reload, annotation-to-source navigation, multi-line current
  selection, and rejection of base-side or cross-pane ranges.

The next implementation should continue with small commits in this order:

1. Add bounded, table-tested recent-commit discovery.
2. Add concurrency-tested comparison snapshots and atomic selection/refresh.
3. Add protected state/select/refresh handlers with conflict coverage.
4. Add the selector, subject display, refresh control, and browser coverage.
5. Finish narrow viewport and color-scheme coverage, then reconcile user docs
   and rebuild distributions.
6. Stop for maintainer review. Do not begin live reload until that review is
   explicitly approved.

The relevant implementation commits immediately preceding this handoff are
`12990aa` (bounded retrieval), `bdac319` (side-by-side renderer), and `fe2db24`
(Changes route and UI). The pane-overflow correction and its browser regression
belong to the handoff commit containing this section.
