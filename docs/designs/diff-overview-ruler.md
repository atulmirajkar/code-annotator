# Design: diff overview ruler

## Status

Approved and implemented on 2026-08-25. All six review gates are complete. The
server derives request-local hunks and renders semantic targets and
progressive-enhancement overview links. DOM-free
TypeScript owns ruler geometry, while a focused browser adapter validates those
targets, discovers the active scroll owner, coalesces measurement updates,
projects marker and viewport styles, and intercepts unmodified activation.
The fourth diff grid column, sticky track, marker kinds, current/next treatment,
and accessible focus states are active. A dedicated long-file Playwright
fixture now verifies five separated hunks plus an 800-hunk density stress case,
navigation, scroll ownership, responsive layout, themes, focus, and horizontal
pane independence. README, code-review, architecture, design, and project-status
documentation describe the implemented behavior; `docs/build.md` remains
unchanged because the contributor and release workflow did not change.

## Problem

Changes view renders the complete base and current files. That completeness is
important for review context, but it makes a long file expensive to scan: the
reviewer cannot tell where later change hunks are, how far away the next hunk
is, or whether the remainder of the file contains any changes without moving
through the document.

The diff panes already share one vertical flow and use the document pane as the
vertical scroll owner on desktop. Each diff pane owns only horizontal
scrolling. The overview must preserve that layout rather than introduce a
second vertical scrollbar or independently scrollable diff panes.

## Goals

- Show every change hunk as a marker in a compact vertical ruler at the
  trailing edge of Changes view, immediately beside its vertical scroll edge.
- Scale marker positions across the complete diff so the reviewer can see the
  distribution of changes at a glance.
- Distinguish added, deleted, and mixed/replacement hunks using the existing
  diff color language.
- Indicate which hunk is current or next for the visible scroll position.
- Let pointer and keyboard users navigate directly to any hunk.
- Keep base and current lines aligned, preserve independent horizontal pane
  scrolling, and avoid page-level horizontal overflow.
- Work with the desktop document scroll container and the responsive page
  scroll without duplicating scroll policy in the diff feature.
- Preserve the server-rendered state boundary: Go identifies change hunks;
  TypeScript owns only browser geometry, scrolling, focus, and resize updates.

## Non-goals

- Collapsing unchanged lines or rendering only Git context hunks.
- Adding a second vertical scrollbar to either diff pane.
- Replacing the draggable base/current divider.
- Navigating between changed files; the ruler is scoped to the open file.
- Adding diff minimap text, syntax tokens, diagnostics, annotations, or comment
  locations to the ruler.
- Performing intra-line diffing or changing the existing row-alignment rules.
- Persisting ruler position or navigation state between page loads.
- Assigning a new global keyboard shortcut in the first slice.

## User experience

Changes view gains a narrow fourth column after the current pane:

```text
 BASE                   CURRENT                  OVERVIEW
 unchanged              unchanged                 │
 old value              new value                 ├ modified hunk
 unchanged              unchanged                 │
 ...                    ...                       │
 [empty]                added value               ├ added hunk, next
 ...                    ...                       │
 deleted value          [empty]                   ├ deleted hunk
```

The overview track stays visible while the diff scrolls. Its marker positions
are proportional to the complete aligned diff, not to the currently visible
rows. A subtle viewport indicator shows which portion of the diff is visible.
Markers remain visually above that indicator.

The track always represents the whole file. It does not scroll with the rows.
For example, if a hunk starts at aligned row 750 of a 1,000-row diff, its marker
appears about three quarters of the way down the visible track even when row
750 is far below the viewport. Scrolling moves the viewport indicator, not the
hunk marker.

A change hunk is one maximal contiguous run of non-unchanged aligned rows. One
or more unchanged rows end the hunk. This uses the renderer's existing aligned
row sequence and does not reinterpret Git patch text in the browser.

Each hunk receives one marker:

| Hunk contents                                            | Marker treatment                                                    |
| -------------------------------------------------------- | ------------------------------------------------------------------- |
| Added rows only                                          | Existing added/green color                                          |
| Deleted rows only                                        | Existing deleted/red color                                          |
| Any modified row, or a mixture of added and deleted rows | Split or neutral modified treatment using both existing diff colors |

The first hunk at or below the leading edge of the visible diff is the **next
change**. If the viewport intersects a hunk, that hunk is current and takes
precedence over a later hunk. The corresponding marker receives a stronger
outline and contrast. This state is visual only and is not announced on every
scroll event.

Selecting a marker scrolls its first aligned row near the center of the actual
vertical scroll owner. Focus remains on the marker so a keyboard user can
continue through the ruler. If TypeScript does not run, each marker remains a
normal same-page link to the hunk's first current-side cell and therefore
retains basic navigation.

### Marker spacing

Each marker first receives an ideal start and height from its hunk's existing
row range:

```text
start  = hunk StartRow / total aligned rows
height = (hunk EndRow - hunk StartRow) / total aligned rows
```

Those fractions are mapped onto the ruler's visible height. A long hunk
therefore produces a longer mark than a one-row hunk. A minimum visual height
keeps a short hunk visible. The final start is clamped so that minimum-height
markers for the first and last rows remain inside the track instead of being
clipped at its edges. Empty diffs have no ruler, so the calculation never
divides by zero.

When two ideal positions would overlap, the browser preserves their order and
moves them the smallest distance needed to leave a visible gap. A forward pass
pushes later markers down; a backward pass pulls an end group back inside the
track. This adjustment affects only marker presentation. Selecting either
marker still navigates to its exact server-rendered hunk target.

An extreme diff can contain more hunks than the ruler has device pixels. In
that case, one pixel represents a **density group** of nearby hunks. No hunk is
dropped: all server-rendered links remain available to keyboard and assistive
technology users, while the shared visible mark communicates that several
changes occupy that part of the file. Hover or focus expands the group enough
to distinguish its individual links. This density fallback is a required part
of the first implementation, not deferred work.

## Layout and responsive behavior

### What changes in the grid

The current diff has three columns: base pane, draggable divider, and current
pane. The ruler adds one fixed-width column on the right:

```text
base pane | divider | current pane | ruler
```

The heading row reserves the same ruler width, which keeps the Base and Current
labels aligned with their panes. The ruler takes space from the diff instead of
covering source text. It does not cover the current pane's horizontal
scrollbar.

The existing divider still controls the relative width of the base and current
panes. Its saved 20–80 percent range does not change.

### What scrolls

The ruler does not introduce a scrollbar. It follows whichever element already
scrolls the document:

| Viewer layout            | Existing vertical scroll owner |
| ------------------------ | ------------------------------ |
| Desktop                  | `.document`                    |
| Narrow or stacked layout | Browser window                 |

The browser component finds the nearest vertical scroll owner from computed
layout. If there is none, it uses `Window`. This keeps the component consistent
with responsive CSS without copying the `1100px` breakpoint into TypeScript.

The base and current panes keep their existing, independent horizontal
scrollbars. Moving either pane horizontally does not move the ruler.

### What stays visible

The ruler track is sticky, so it remains visible while the reviewer moves
through a long diff. Its boundaries are simple:

- its top sits below the sticky source tabs and diff headings;
- its bottom cannot extend past the end of the diff; and
- its height fills the remaining visible part of the document scroll area.

Markers use that visible track to represent positions across the complete
diff. The viewport indicator shows which part of the complete diff is currently
on screen.

### When positions are recalculated

The component recalculates marker and viewport positions when:

- the vertical scroll owner scrolls;
- the window resizes;
- a `ResizeObserver` reports a size change to the diff, ruler, or scroll owner;
- the document or annotations sidebar is collapsed, expanded, or resized.

Repeated events are combined into one `requestAnimationFrame` update. The
component writes the resulting positions as CSS custom properties, as the
existing diff-divider component does.

The ruler remains visible on narrow screens. Its column has a small fixed width
and must not create page-level horizontal overflow.

## Server-rendered contract

### Existing diff structures

`internal/gitdiff` currently represents one complete side-by-side diff with
these structures (irrelevant comments omitted here):

```go
type FileDiff struct {
    Path       string
    BasePath   string
    BaseCommit string
    BaseSource []byte
    Rows       []Row
}

type Row struct {
    Kind         RowKind // unchanged, added, modified, or deleted
    OldLine      int
    NewLine      int
    CurrentStart int
    CurrentEnd   int
    BaseText     string
}
```

`BuildFileDiff` reads the base version of the file and asks Git for only the
lines that changed, without the surrounding unchanged lines. `ParsePatch`
validates that result and reconstructs the complete `Rows` sequence by filling
in the unchanged lines. Each `Row` aligns at most one base line with at most one
current line.

The patch parser also has an internal `hunkRange`, but that value exists only
while parsing a unified hunk header. It is discarded after the complete row
sequence is built. It is not suitable for the ruler because the UI's source of
truth is the validated, aligned `Rows` sequence.

No diff information is persisted. A `FileDiff` exists only for the current
HTTP request and is rebuilt from the pinned base commit and current worktree
file when Changes view is requested.

### New ruler projection

The ruler does not change `FileDiff`, `Row`, Git parsing, or the annotation
schema. `internal/render` derives a small presentation model immediately before
rendering:

```go
type diffOverviewHunk struct {
    StartRow int             // inclusive index into FileDiff.Rows
    EndRow   int             // exclusive index into FileDiff.Rows
    Kind     gitdiff.RowKind // added, deleted, or modified
}

type diffOverviewItem struct {
    Hunk        diffOverviewHunk
    TargetID    string
    EndTargetID string
    Label       string
}
```

These structures are request-local and are never written to disk. `StartRow`
and `EndRow` refer to the existing aligned rows instead of copying line text or
byte ranges. Each item computes its semantic target IDs and accessible label
once, then supplies them to both cell rendering and overview-link rendering.
`Kind` is:

- `added` when every row in the range is added;
- `deleted` when every row in the range is deleted; and
- `modified` for every other changed range.

The data flow is therefore:

```text
base blob + current file + Git patch
                │
                ▼
       gitdiff.FileDiff.Rows
                │
                ├── existing base/current cells
                │
                └── request-local diffOverviewHunk values
                              │
                              ▼
                    ruler links and target IDs
```

There is no new JSON response, database field, sidecar field, or browser-owned
hunk model. The server-rendered links are the complete browser contract.

### Rendering the projection

`RenderDiffWithSyntax` will:

1. group contiguous non-unchanged rows into hunks;
2. classify each hunk as added, deleted, or modified;
3. assign stable page-local identities in display order, such as
   `diff-change-1`;
4. place start and end identities on the hunk's first and last current-side
   cells, including empty current cells for a deletion-only hunk; and
5. render one ordered overview link for every hunk.

The proposed semantic markup shape is:

```html
<div class="diff-cell diff-current diff-modified" id="diff-change-1">...</div>
<div class="diff-cell diff-current diff-added" id="diff-change-1-end">...</div>
...
<nav class="diff-overview" aria-label="Changes in this file">
  <span class="diff-overview-viewport" aria-hidden="true"></span>
  <span class="diff-overview-item">
    <a
      class="diff-overview-marker diff-overview-modified"
      href="#diff-change-1"
      aria-label="Change 1 of 4, modified near current line 27"
    ></a>
    <a
      class="diff-overview-end"
      href="#diff-change-1-end"
      tabindex="-1"
      aria-hidden="true"
    ></a>
  </span>
</nav>
```

The exact line label is derived from authoritative row fields. For a
deletion-only hunk, it describes the deletion relative to the closest current
line (for example, `deletion after current line 27`); it never invents a
current line number for an empty cell. The labels do not include source text.

The rendered contract uses semantic IDs and ordinary links rather than custom
`data-*` attributes. Go owns hunk formation, order, classification, target
identity, extent, and accessible labels. Each item has one normal navigation
link to its start and one non-focusable, hidden link to its end. A one-row hunk
uses the same target for both. TypeScript resolves those explicit targets and
measures their browser positions; it does not inspect row classes, siblings, or
line text to rebuild the hunk list.

A file with no changed rows renders no overview navigation. An empty diff and
a diff-unavailable page retain their current markup and behavior.

## Browser component

A focused `web/src/diff-overview.ts` module exposes one entrypoint:

```ts
export function bindDiffOverview(environment: DiffOverviewEnvironment): void;
```

Its environment receives `Document`, `Window`, `ResizeObserver`, and
`requestAnimationFrame` ports explicitly. `viewer.ts` only passes those
dependencies and remains a composition root.

The component:

- find the optional ruler and its server-rendered links;
- validate that every same-page fragment resolves to a unique element inside
  the current diff pane, skipping the component clearly if the server contract
  is inconsistent;
- find the active vertical scroll owner from computed layout;
- measure target positions relative to the full diff extent;
- project marker, viewport, and current/next presentation through CSS custom
  properties and classes;
- intercept marker activation to scroll the resolved target into view while
  preserving marker focus; and
- tear down transient scroll listeners if ownership changes after responsive
  layout changes.

The links are navigation state supplied by the server, an explicitly permitted
HTML responsibility. All decisions about what constitutes a change remain out
of TypeScript. The component reads only semantic link targets, focus/input
events, and measured browser geometry.

## Accessibility

- The ruler is a named navigation landmark: `Changes in this file`.
- Every marker is an ordinary link with its ordinal, total, hunk kind, and
  nearest meaningful current-line context in its accessible name.
- Markers have a narrow visual line but a larger pointer hit area that does not
  cover the current pane.
- Keyboard focus receives a clearly visible outline in both color schemes.
- Color is not the only indication of the active next marker; outline and
  thickness also change.
- The current/next marker uses `aria-current="location"`. Updates are not sent
  through a live region, avoiding continuous announcements during scrolling.
- Navigation does not use smooth scrolling when the user prefers reduced
  motion.
- The source target uses `scroll-margin` so sticky tabs and headings do not
  cover the destination.

The initial native-link implementation means a file with many hunks also has
many tab stops. This is acceptable for the first slice because the ruler is a
named landmark that assistive-technology users can skip. If large reviews make
that burdensome, a later design can add roving focus without changing hunk or
rendering contracts.

## Failure behavior

The diff remains fully usable if the ruler cannot initialize. Missing targets,
duplicate identities, or unusable geometry cause the enhanced positioning and
scroll interception to stop; they do not hide diff content or change the
server-rendered fallback links. Resize-observer or animation-frame support is
required through the existing viewer environment rather than obtained from an
undeclared global.

## Testing

### Go renderer tests

- No ruler for unchanged, empty, or unavailable diffs.
- One marker for one contiguous run of changed rows.
- Separate markers for changed runs separated by unchanged rows.
- Added-only, deleted-only, and mixed/modified classification.
- Stable IDs, ordinal/total labels, and valid deletion-only destinations.
- No custom state attributes and no source text in accessible labels.

### Vitest tests

- Pure proportional-position and visible-range calculations at the top,
  middle, and bottom of a diff.
- Collision packing preserves order and keeps ordinary markers individually
  visible within the track.
- Density grouping represents every hunk when the marker count exceeds the
  available device pixels.
- Current-hunk precedence and next-hunk selection.
- Same-page target validation and graceful handling of missing or duplicate
  targets.
- Scroll-owner selection for an element scroll container and the window
  fallback.
- Coalescing repeated scroll/resize events into one animation-frame update.
- Marker activation uses the resolved target and retains link focus.
- Binding against a page without a diff or without changes is a no-op.

### Playwright tests

A long multi-hunk fixture verifies behavior in a real browser:

- marker count, order, classification, and proportional vertical order;
- off-screen hunks retain markers inside the visible track, including a hunk
  near the end of a long file;
- closely spaced hunks pack without changing their navigation targets, and a
  synthetic high-density case exposes every link;
- the next marker at initial load and after scrolling past the first hunk;
- pointer and keyboard activation scroll the actual desktop `.document`
  container to the intended hunk;
- the viewport indicator moves and changes size consistently;
- the ruler remains sticky and inside the diff bounds;
- divider dragging and independent horizontal pane scrolling still work;
- narrow viewports with both sidebars in their supported layouts do not gain
  page-level horizontal overflow; and
- markers and focus treatment remain distinguishable in light and dark modes.

The completed implementation passes `npm run check:web`, the focused Playwright
diff suite, `go test ./...`, and `go vet ./...`.

## Implemented documentation

The completed implementation updates:

- `README.md` with the ruler's navigation behavior;
- `docs/designs/code-review.md` with the implemented Changes-view contract;
- `docs/architecture.md` with renderer and browser-component ownership;
- `docs/build.md` only if the contributor workflow changes; and
- `project_status.md` with the completed milestone and verification results.

This document remains the detailed design and verification contract for the
implemented feature.

## Final verification

Final verification completed on 2026-08-25 with:

- `npm run check:web`: 12 Vitest files and 55 tests passed; TypeScript,
  generated JavaScript, and generated CSS rebuilt without a diff;
- `npm run test:browser`: all 47 Playwright scenarios passed, including the six
  dedicated ruler scenarios;
- `go test ./...`: passed;
- `go vet ./...`: passed; and
- `go test -race ./...`: passed.

## Review-gated implementation commits

1. **Approve the design.** Commit this document as the implementation contract.
   No production behavior changes.
2. **Add the server contract.** Derive request-local overview hunks from aligned
   rows, render semantic targets and links, and cover grouping,
   classification, labels, and empty states in Go renderer tests.
3. **Add pure ruler geometry.** Implement proportional marker sizing,
   collision packing, density grouping, viewport calculations, and current/next
   selection as DOM-free TypeScript with Vitest coverage.
4. **Activate the browser ruler.** Add the DOM controller, styles, explicit
   browser ports, viewer composition wiring, generated and embedded assets,
   and focused adapter and static-asset tests.
5. **Verify real-browser behavior.** Add a long multi-hunk fixture and focused
   Playwright coverage for navigation, sticky positioning, actual scroll
   ownership, narrow layouts, themes, and accessibility behavior.
6. **Close out the feature.** Update implemented-behavior documentation and
   run the complete web, browser, Go, vet, and race verification sets.

Every commit must build and pass the checks appropriate to its layer. Stop
after each commit so the maintainer can review it before the next gate begins.
