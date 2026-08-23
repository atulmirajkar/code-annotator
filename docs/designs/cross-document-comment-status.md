# Design: cross-document open-comment status

## Status

Implemented. This design defines milestone 15 in
[`../../project_status.md`](../../project_status.md).

## Problem

The document sidebar lets a reviewer move between files, but it does not
preserve a useful answer to a cross-document review question: which files
still need attention? Once a reviewer navigates away from a document, its
annotation cards are no longer visible. The reviewer must remember which
files were visited or reopen documents one by one to find comments that are
still active.

This becomes increasingly costly when a review contains many files or when
comments change while the reviewer is working in another document.

## Goals

- Make documents with active comments discoverable from the left document
  sidebar regardless of the currently selected document.
- Let a reviewer toggle the document list between all documents and only
  documents with active comments, alongside the existing Changed only filter.
- Show a compact per-document count so the reviewer can distinguish one
  comment from many without opening the document first.
- Keep the current document selected and navigable when the filter changes.
- Update the status after annotation creation, reply, lifecycle transition,
  reattachment, and navigation to another document.
- Preserve the existing document ordering, path lookup, and collapsed-sidebar
  behavior.

## Non-goals

- Replacing the annotation sidebar or its card-level status filters.
- Adding a second review queue or changing annotation lifecycle semantics.
- Showing closed or rejected annotations in the active-comment view.
- Synchronizing comment status between separate running viewer processes.
- Automatically opening the first document with an active comment when the
  filter is enabled.
- Combining Changed only and Open comments into an intersection filter.

## Prerequisite: an actual file tree

The current document sidebar is a flat list of file rows. This milestone will
first change it to a real file tree with explicit directory rows; the
open-comments toggle is built on that tree rather than adding filtering rules
to the current flat markup.

The tree is derived from the existing catalog paths in the browser. Each path
is split into directory segments and a file leaf. Directory nodes are rendered
as expandable rows with a disclosure control; file nodes retain the existing
navigation link, document kind, changed marker, and path-search behavior. The
tree model is deterministic and preserves the catalog's existing path order.
Directory expansion state is local to the current tab and is independent from
the selected document, sidebar collapse state, File/Changes mode, and filter
scope.

The server does not need a new directory API. It continues to provide the
flat reviewable-document catalog, with stable document paths; the client owns
the tree projection and rendering. This keeps directory presentation separate
from content discovery and lets the same catalog support Markdown-only and
code-review sessions.

## Proposed user experience

After the file-tree prerequisite is complete, the left sidebar keeps the
existing `Changed only` toggle and adds an `Open comments` toggle beside it,
near the document search and navigation controls:

```text
Documents                              [Open comments]
[Filter documents...]                  7 documents
[ ] Changed only   [ ] Open comments

▾ src/
  review-panel.ts                         2
  review.ts                               1
▾ docs/
  design.md                               4
```

The new control is a toggle button, not a permanent mode. Its accessible name is
`Show documents with open comments`; when active it changes to
`Show all documents`. The active state is exposed with `aria-pressed` and a
visible selected style. The total number of matching documents is shown next
to the control. Each matching document displays its active-comment count as a
badge; documents without active comments have no badge in the all-documents
view.

The two sidebar filters are mutually exclusive. Selecting `Open comments`
clears `Changed only`, and selecting `Changed only` clears `Open comments`.
They are two simple scopes over the same document list, not a compound filter
with intersection behavior. The existing path search continues to compose
with whichever scope is selected. If neither toggle is selected, the complete
catalog is shown.

The active scope is stored as one tab-local value (`all`, `changed`, or
`open-comments`). Existing tabs that have the current boolean Changed only
preference migrate `true` to `changed` and `false` to `all`. The existing
Changed only default-on behavior for a non-clean diff remains unchanged; an
explicit reviewer choice of either scope wins for the rest of the tab.

When the filter is active:

- Only documents with at least one active annotation remain in the list.
- Directory rows remain only when they contain a matching descendant, so the
  existing hierarchy remains understandable.
- Selecting a document navigates normally and leaves the filter enabled.
- If the selected document has no active comments after a mutation, it is
  removed from the filtered list after the current operation completes. The
  current document remains rendered until navigation or refresh makes the
  removal visible, avoiding an abrupt replacement during a reply or status
  change.
- If the filter has no matches, the sidebar shows an empty state with the
  message `No documents with open comments` and a control to show all
  documents.

The filter preference is local to the current browser tab, like the existing
sidebar collapse and File/Changes view preferences. It is not persisted in
annotation sidecars or sent to the server.

## Definition of an open comment

For this feature, an open comment is any annotation whose status is not
terminal:

| Status | Included | Reason |
| --- | --- | --- |
| `open` | Yes | New reviewer work has not been acknowledged. |
| `acknowledged` | Yes | An agent or author has started work, but review is not complete. |
| `needs_changes` | Yes | The reviewer has returned the request for another attempt. |
| `applied` | Yes | A change was reported, but the reviewer has not closed it. |
| `closed` | No | The review thread is complete. |
| `rejected` | No | The request was explicitly declined. |

The count is the number of matching annotations in the document, not the
number of threads or replies. A document-level annotation counts toward the
same document total.

## Data and rendering design

The document sidebar needs an aggregate status map keyed by the catalog's
stable document path:

```text
document path -> { active count, total count by status }
```

The implementation will derive this map from the existing annotation queue
response, which already loads actionable annotations across documents and
identifies each document path. The client combines that response with the
catalog so documents with zero matches remain visible when the scope is `all`
or `changed`. No new endpoint is planned for this milestone, and the sidebar
will not couple itself to annotation-card rendering or expose mutation
capabilities through a new path.

Filtering should happen in the client-side document tree model, before DOM
rendering. The unfiltered catalog remains the source of truth. The filtered
tree is a derived view that retains only matching files and their ancestor
directories. Counts must be rendered as text as well as color or icons so the
signal remains available to keyboard and screen-reader users.

## Refresh and consistency

The aggregate status map is refreshed when:

1. the viewer loads or changes documents;
2. an annotation is created, replied to, acknowledged, applied, rejected,
   reattached, or otherwise changes lifecycle state; or
3. the annotation sidebar performs its existing refresh after a revision
   conflict.

The refresh may reuse the current annotation fetch and should avoid a second
request when the active document operation already returned authoritative
annotation data. Stale counts are preferable to blocking document navigation,
but a failed refresh must not clear the last known map; show the existing
document list and retry on the next qualifying event.

The Changed only filter continues to use the existing Git comparison state.
Switching between scopes recomputes the visible tree from the same unfiltered
catalog and does not change the selected File/Changes mode or frozen
comparison base.

## Accessibility and responsive behavior

- The toggle is a real button with an accessible name, pressed state, and
  keyboard activation.
- Count badges have an accessible label such as `2 open comments`.
- The empty state is announced through the sidebar's existing status region.
- On narrow layouts the toggle and count may wrap below the search field, but
  must remain inside the left sidebar and must not widen the page.
- Filtering must not change focus unexpectedly. After a mutation removes the
  current item from the filtered tree, focus remains in the active control or
  annotation form until the reviewer chooses another document.

## Implementation slices

1. Add the client-side file-tree model and render explicit expandable
   directory rows from the existing flat catalog paths. Preserve current
   links, search, changed markers, and keyboard navigation.
2. Define the client-side aggregate summary type from the existing annotation
   queue response and map counts by stable document path.
3. Replace the standalone Changed only preference with a backward-compatible
   tab-local document-scope value and make the two visible filters mutually
   exclusive.
4. Build filtered document-tree derivation and count rendering, including the
   empty state and accessible toggle semantics.
5. Wire summary refreshes into annotation mutations, revision-conflict
   reloads, and document navigation while preserving tab-local filter state.
6. Add unit coverage for tree construction, expansion state, status inclusion,
   counts, directory pruning, empty results, and stable ordering.
7. Add browser coverage for the file tree, switching between all, Changed
   only, and Open comments scopes; navigating between matching documents; count
   updates after a lifecycle transition; empty results; keyboard access; and
   narrow layouts.
8. Update user-facing documentation and mark milestone 15 complete.

## Acceptance criteria

- A reviewer can enable one left-sidebar toggle and see only documents with
  active comments across the full catalog.
- The sidebar displays explicit expandable directory rows before the
  open-comments filter is enabled.
- `Changed only` and `Open comments` are displayed together, but never active
  together; selecting one clears the other.
- Existing Changed only behavior and its tab-local default/migration remain
  intact.
- Every filtered document shows the correct active-comment count, including
  document-level annotations.
- The filter includes `open`, `acknowledged`, `needs_changes`, and `applied`,
  and excludes `closed` and `rejected`.
- Creating or changing a comment updates the relevant count without requiring
  a full-page reload.
- Directory hierarchy, path lookup, selection, collapsed sidebars, and
  File/Changes mode continue to work with the filter enabled.
- Filtering prunes empty directories while preserving the tree path to every
  matching file.
- The control and counts are keyboard accessible and remain usable at narrow
  viewport widths.
- Existing annotation lifecycle, storage, and agent handoff behavior is
  unchanged.

## Risks and follow-ups

- If the queue response resolves anchors for every annotation, refreshing it
  after every mutation may be more work than the sidebar needs. Measure this
  before introducing a dedicated summary endpoint.
- Multiple browser tabs may display different in-memory filters and counts;
  cross-tab synchronization is intentionally deferred.
- A future review dashboard could reuse the aggregate summary for filters by
  status, author, or stale-anchor state, but those controls are outside this
  milestone.
