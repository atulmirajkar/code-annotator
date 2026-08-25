# Design: server-rendered review UI and testable TypeScript

## Status

Implementation in progress. Commit gates 0 through 7, commit 7 review
follow-ups A and B, the commit 4 review follow-up, and the intervening role-only
compatibility slice are approved. Commit 7 review follow-up C is approved.
Commits 8A, 8B, 9, and 10 are implemented and ready for combined review.

This document defines milestone 17 in
[`../../project_status.md`](../../project_status.md). It supersedes the
imperative-DOM non-goal in
[`typescript-migration.md`](typescript-migration.md), which remains the
historical design for the completed JavaScript-to-TypeScript migration.

Every implementation commit is a maintainer review gate. An agent must make
exactly one planned commit, report its hash, scope, checks, and residual risks,
and stop. Work on the next commit begins only after the maintainer explicitly
approves proceeding.

## Documentation authority

Repository documentation is the handoff contract for a new agent session:

- `project_status.md` identifies the current milestone, completed commit gate,
  next approved gate, and any blocking verification result.
- This document defines the approved target, invariants, route and template
  contracts, test strategy, and ordered commit plan.
- `docs/architecture.md` describes only the architecture implemented in the
  current commit. It must not describe a future slice as already present.
- `README.md` describes only user-visible behavior and commands available in
  the current commit.
- `docs/build.md` describes the current build, test, generated-asset, and
  frontend-development workflow.

Every implementation commit must update all affected documentation in the
same commit. A new agent should not need chat history or the Lavish artifact to
determine what exists, what comes next, or how to verify it.

## Problem

The strict TypeScript migration made browser contracts visible but preserved
the original imperative rendering architecture. The authored frontend is now
2,277 lines across 13 modules and contains approximately 220 DOM query,
mutation, dataset, and event-listener touchpoints. There are browser tests but
no TypeScript unit-test files.

Several modules mix responsibilities that change at different rates:

- `review.ts` and `viewer.ts` execute against global browser state at module
  import time.
- `review-render.ts` reconstructs annotation cards and threads from JSON.
- `review-actions.ts` reconstructs lifecycle rules, actor choices, forms,
  mutation state, conflict behavior, and errors in the browser.
- `review-api.ts` returns raw `Response` objects, leaving each caller to repeat
  status and body handling.
- JSON, `FormData`, and `dataset` values are asserted into domain types without
  runtime validation.
- Pure rules such as byte-offset mapping, interval merging, filtering,
  preference decoding, and numeric clamping are embedded in DOM controllers.

The Go server already owns annotation persistence, source anchoring,
optimistic concurrency, lifecycle validation, comparison state, content
cataloging, and safe HTML rendering. Reproducing its state as browser-built
HTML creates a second presentation model without creating useful offline
capability.

## Decision

Use a hybrid server-rendered architecture:

- Go owns authoritative review state, valid actions, and HTML fragments.
- HTMX 2.x owns form transport, request targeting, and fragment replacement.
- TypeScript owns browser-only geometry, selection, highlighting, navigation,
  resizing, local preferences, Mermaid, and small HTMX lifecycle adapters.
- Existing `/api/*` JSON contracts remain stable for the live agent CLI and
  other automation.
- New `/ui/*` routes accept browser forms and return HTML fragments. UI and API
  handlers call the same application operations rather than one handler
  calling another over HTTP.

HTMX will be pinned to stable version `2.0.10`, copied into
`web/vendor/htmx/`, licensed alongside the vendored asset, embedded in the Go
binary, and served from the viewer origin. The application will not use a CDN
at runtime. HTMX 4 is pre-release and is not part of this design.

No frontend framework, JSX renderer, hyperscript, or HTMX extension is planned.

### DOM/state boundary

Removing custom attributes is necessary but not sufficient. Rendered nodes,
classes, text, links, `hidden`, and accessibility attributes must not become an
alternative application-state store.

The governing rule is:

> TypeScript may read user interaction and browser layout from the DOM, but it
> must not reconstruct document, annotation, comparison, or workflow state
> from rendered nodes.

Permitted DOM inputs are native `Selection` and `Range`, focus, pointer and
keyboard events, form control values, measured geometry, rendered text needed
to map a native selection, and semantic-ID lookup used to project typed state
onto an element. Classes, visibility, labels, and accessibility attributes are
outputs of that projection, not inputs to domain decisions.

The remaining reviewed debt is:

- `viewer.ts` enumerates document `<li>` nodes, derives paths, changed status,
  filter matches, visible results, and open-comment badges from or into DOM;
- `document-tree.ts` combines tree nodes with `HTMLLIElement` instances and
  treats their visibility as filtering state;
- source-mode initialization derives state from the active tab and rewrites
  every document link;
- comparison JSON is asserted into an interface without runtime validation;
- `review.ts` derives visible annotations, mutation kind, and conflict feedback
  from swapped card/form/feedback nodes, and its root-page document fallback
  reads the active navigation link;
- source-range ordering still enumerates rendered spans even though viewer
  state already provides ordered semantic identities; and
- Mermaid obtains diagram definitions by treating rendered source text as its
  input model.

Migration does not ban `querySelector`. Entry points and view adapters still
need element lookup and event delegation. Pure state modules, however, must be
DOM-free and protected by an architecture test that rejects browser globals,
DOM element types, selectors, and datasets in those modules.

## Goals

- Make the remaining TypeScript independently instantiable and unit testable.
- Remove browser-side construction of annotation cards, threads, actions,
  document summaries, document-tree rows, and comparison options.
- Make server lifecycle rules the only source of visible annotation actions.
- Preserve the JSON agent API, sidecar schema, source anchors, and optimistic
  concurrency semantics.
- Preserve the single offline Go binary and self-only Content Security Policy.
- Preserve keyboard, accessibility, responsive, selection, diff, and Mermaid
  behavior.
- Keep every migration commit buildable, testable, documented, and small
  enough to review in isolation.
- Reduce handwritten browser TypeScript to at most 1,200 lines by the end of
  the milestone, excluding generated JavaScript and vendored dependencies.

## Non-goals

- Replacing all TypeScript with HTMX.
- Sending pointer movement, text selection, resize, or local preference changes
  to the server.
- Changing annotation lifecycle semantics or sidecar storage.
- Changing the live agent CLI or its JSON payloads.
- Adding live reload, syntax highlighting, editing, uploads, or network sharing.
- Converting the complete document page to partial navigation in the first
  milestone.
- Adding `hx-boost` before fragment lifecycle and history behavior are proven.
- Using DOM emulation as the correctness oracle for browser `Selection`,
  `Range`, layout, CSS Highlights, scrolling, or Mermaid.

## Invariants

Every commit must preserve these contracts:

1. `code-annotator` remains a single offline Go binary.
2. Runtime assets load only from the loopback viewer; no CDN is required.
3. Raw Markdown HTML remains disabled.
4. Existing mutation origin, token, size, and revision checks remain in force.
5. API mutations keep `application/json` and strong `If-Match` behavior.
6. UI mutations are form encoded but call the same validated application
   operations and use the same revision coordinator.
7. A `409 Conflict` never retries automatically. The UI displays the latest
   authoritative state and asks the reviewer to reconsider.
8. User-controlled text is escaped by `html/template`; it is never marked as
   trusted HTML.
9. Agent-facing `/api/*` routes and response shapes remain compatible.
10. Generated browser assets remain checked in and reproducible.
11. A commit that changes behavior also changes its tests and documentation.

## Responsibility boundary

### Go and HTML templates

The server will own:

- annotation cards, metadata, source context, thread entries, and counts;
- active/inactive filtering;
- available lifecycle actions and required activity fields;
- create, reply, transition, quick-close, and reattach forms;
- mutation success, validation, and revision-conflict messages;
- the recursive document tree, document kind, changed state, and active-comment
  counts;
- document search/scope result markup;
- comparison selector options and active comparison state;
- accessible labels that are derived from authoritative state.

### TypeScript

The browser will retain:

- text and Mermaid selection capture;
- UTF-16 DOM offset to UTF-8 source offset conversion;
- source-range reconstruction and CSS/fallback highlights;
- annotation-to-source navigation, scrolling, focus, and temporary emphasis;
- panel collapse and resize behavior;
- diff divider behavior;
- tab-local and local preferences;
- keyboard shortcuts for search focus and result navigation;
- Mermaid rendering;
- one HTMX request adapter and one post-swap initializer.

The interaction modules must export explicit initialization functions that
accept a root and narrow dependencies. Entry modules may pass `document`,
`window`, `sessionStorage`, and `localStorage`; leaf modules must not capture
those globals at import time.

## Target flow

```text
browser gesture or form submit
            |
            v
small TypeScript adapter (only when browser geometry is needed)
            |
            v
HTMX request to /ui/*
            |
            v
shared Go application operation
            |
            +--> annotation store / content / Git comparison
            |
            v
Go template renders authoritative fragment
            |
            v
HTMX replaces the smallest stable target
            |
            v
post-swap TypeScript reapplies highlights and delegated behavior
```

## Server structure

### Shared application operations

Current JSON handlers combine HTTP decoding with annotation operations. Before
the UI routes become active, mutation logic will be extracted into unexported
`internal/server` application methods with typed inputs and typed results.
Both JSON and HTML handlers will call these methods.

The shared layer owns:

- catalog membership and safe source reads;
- sidecar revision loading and conflict reporting;
- selector construction and reattachment validation;
- reply and lifecycle entry construction;
- atomic saves;
- refreshed annotation view-model collection.

The shared layer does not own HTTP headers, content negotiation, JSON, form
parsing, or template execution.

### UI routes

The target UI surface is separate from the agent API:

| Method and route | Response target | Purpose |
| --- | --- | --- |
| `GET /ui/review/annotations?document={path}&show_inactive={bool}` | `#annotation-panel-content` | Render the current annotation panel content. |
| `POST /ui/review/annotations` | `#annotation-panel-content` | Create an annotation and return the authoritative panel. |
| `POST /ui/review/annotations/{id}/replies` | `#annotation-panel-content` | Append a reply and return the authoritative panel. |
| `POST /ui/review/annotations/{id}/transition` | `#annotation-panel-content` | Apply one lifecycle transition and return the authoritative panel. |
| `POST /ui/review/annotations/{id}/reattach` | `#annotation-panel-content` | Verify a browser selection, reattach, and return the panel. |
| `GET /ui/review/documents?q={query}&scope={scope}&selected={path}&mode={mode}` | `#document-results` | Render the filtered recursive document tree and status. |
| `GET /ui/review/git-comparison` | `.diff-comparison-control` | Render the bounded comparison options. |
| `POST /ui/review/git-comparison` | document page | Re-pin the comparison and refresh the current URL/mode. |

UI mutation routes require the exact review origin, session token, and strong
revision just like the JSON API. They accept
`application/x-www-form-urlencoded` with the existing 64 KiB body limit.
Tokens and revisions are not placed in query strings.

The UI route prefix is not an alternate public API. Its HTML structure may
evolve with the page template, while `/api/*` remains the automation contract.

### Template layout

Move the current page into a parsed template set:

```text
web/templates/
  page.html
  annotation-panel.html
  annotation-card.html
  annotation-actions.html
  document-tree.html
  comparison-control.html
```

Templates use named definitions so the full page and fragment handlers share
the same markup. `web/embed.go` embeds the template directory, generated
assets, HTMX, and third-party licenses.

Template view models are presentation-specific Go types. They contain already
validated domain values and derived display fields, not request objects or
stores. Helpers may format labels and URLs, but lifecycle authorization remains
in the annotation domain/application layer.

## HTMX contract

### Loading and targets

The initial page remains a complete server-rendered document. In review mode,
the annotation panel may load its content through HTMX so the same fragment is
used after mutations. Targets must be the smallest stable region that can be
replaced without losing unrelated focus or local state.

The milestone will not apply `hx-boost` to document links. Ordinary navigation
continues to provide history, fallback behavior, and correct Mermaid/script
loading while the fragment lifecycle is introduced.

### Security and headers

A single typed `htmx:configRequest` listener will add:

- `X-Code-Annotator-Token` from the existing review-token meta element;
- quoted `If-Match` from the active panel's `data-revision` value;
- the comparison token only for the comparison route.

Do not use JavaScript-valued `hx-headers` or inline event attributes. This keeps
the current CSP free of `unsafe-eval` and inline script allowances.

HTMX configuration will keep requests same-origin, disable script processing
in loaded fragments, disable the history cache for review pages, and set
`allowNestedOobSwaps` to false.

### Responses and conflicts

- Successful reads and mutations return `200 OK`, `text/html`, and the new
  authoritative target fragment.
- A successful creation does not require the JSON API's `201`/`Location`
  contract because the browser receives the complete new panel.
- Validation errors return `422 Unprocessable Entity` with the submitted form
  and escaped field-level or form-level feedback.
- Revision conflicts return `409 Conflict` with the latest authoritative panel
  and a conflict banner. The fragment rehydrates submitted role, intent, and
  draft text from the validated form input and escapes them through the
  template.
- If the document changed after selection capture, creation succeeds and saves
  the comment as a stale selection awaiting reattachment. The panel never
  substitutes text from stale byte offsets for the original selection.
- A small `htmx:beforeSwap` adapter permits HTML fragment swaps for expected
  `409` and `422` responses only. Other error responses are not swapped and
  reach the shared error status region.

There is no automatic conflict retry.

Commit 5 implements the reply and transition form routes while leaving them
inactive in the browser until HTMX activation. Both call the same application
operations as their JSON counterparts. Domain validation returns `422` with an
escaped draft in the authoritative panel; optimistic conflicts return `409`,
the latest ETag and panel state, and the escaped draft. JSON status codes,
headers, and payloads remain unchanged.

Commit 6 implements the reattachment form route and completes the inactive
HTML mutation surface. Reattachment hidden fields are parsed into typed byte
offsets before reaching the shared operation. Invalid ranges return `422` with
escaped retained fields; document or anchor-state conflicts return an accurate
`409` panel and discard an obsolete selection; sidecar revision conflicts
return the latest ETag while retaining the still-verified selection for an
explicit retry. Creation validation and revision conflicts now use the same
authoritative fragment contract. The production browser remains on JSON until
commit 7.

### Related updates

Annotation mutations also change cross-document counts. Once the document
sidebar is server-rendered, a mutation response may include one top-level
out-of-band document-summary fragment. Nested out-of-band swaps are disabled.

Before that slice is active, the existing count refresh remains in place. A
commit must not introduce two competing owners for the same badge markup.

### Browser-only selection data

Selection capture remains TypeScript. It writes verified selection values into
hidden form fields:

- `selection_start_byte`;
- `selection_end_byte`;
- `document_sha256`.

The server continues to rebuild source selectors from current bytes and does
not trust a quote, line number, context string, or anchor state from HTML.

## Document tree and filtering

The Go server already has the complete catalog, changed paths, selected path,
document kinds, and annotation summary. It will render the recursive tree
instead of sending a flat list for `document-tree.ts` to rebuild.

Path search and scope filtering will use
`GET /ui/review/documents` with:

- `input changed delay:150ms, search`;
- request replacement so stale responses cannot overwrite newer input;
- `all`, `changed`, and `open-comments` as the only scope values;
- ordinary form submission as the non-HTMX fallback;
- stable IDs on the search input and status region so focus is preserved.

The `/` keyboard shortcut, Escape clearing, ArrowDown focus, and Enter
navigation remain a small TypeScript adapter. Directory expansion remains a
tab-local browser preference. The server renders directory rows and keys; the
adapter only toggles/persists their disclosure state.

Before activation, benchmark a catalog of at least 5,000 nested paths. The
150 ms debounced request must render and swap without visible input lag on the
loopback server. If it fails, keep path matching as a pure TypeScript function
while still server-rendering the tree and counts; record the decision here and
in `project_status.md` before merging that commit.

## Comparison selector

The server will render the active comparison and bounded option list directly.
The UI POST accepts only one commit from the freshly listed bounded set, uses
the existing comparison origin/token guard, updates the server-wide base, and
returns an `HX-Location` or redirect to the current File/Changes URL.

The JSON comparison routes remain available and compatible for existing tests
and possible automation. The browser no longer fetches or renders their JSON.

## TypeScript conventions

Remaining authored TypeScript follows these rules:

- ES modules do not use whole-file IIFEs.
- Entry modules are tiny composition roots; meaningful initializers are
  exported and accept a root plus narrow dependency ports.
- Pure modules do not import DOM types when a primitive or domain type is
  sufficient.
- External JSON begins as `unknown` and is decoded before use.
- `FormData` and `dataset` strings are narrowed through parser functions; type
  assertions do not stand in for validation.
- Exported functions have explicit return types. Local callback types use
  contextual inference when unambiguous.
- Constant lookup tables use readonly values and `satisfies` where it checks a
  domain mapping without widening it.
- Domain unions use exhaustive switches or an `assertNever` helper where all
  states must be handled.
- Event listeners on replaceable content use delegation from a stable root.
- Initializers are idempotent or return a disposer. `AbortController` is
  preferred for groups of listeners that must be removed together.
- Async event handlers explicitly consume returned promises; floating promises
  are prohibited.
- Non-null assertions are allowed only after an invariant check in the same
  scope; prefer typed required-element helpers.

## Test strategy

### TypeScript unit tests

Add Vitest with separate environments:

- Node environment for UTF-8 mapping, interval merging, preference parsing,
  clamping, scope parsing, form/dataset decoders, and command derivation.
- `happy-dom` only for simple initializer, event delegation, attribute, hidden
  field, and HTMX-hook contracts.

DOM emulation is not authoritative for `Selection`, `Range`, layout,
scrolling, CSS Highlights, pointer capture, or Mermaid. Those remain
Playwright responsibilities.

Unit tests live beside source as `web/src/*.test.ts`. They are excluded from
production emission but included in the test TypeScript configuration.

### Go fragment and handler tests

Use `httptest` plus focused golden or structural assertions for:

- escaping comments, source quotes, and thread activity;
- actions visible for every annotation status and role;
- active/inactive filtering and counts;
- form validation and retained safe draft values;
- `If-Match`, origin, token, media type, and size failures;
- 409 authoritative fragments and no automatic mutation retry;
- document tree ordering, pruning, counts, query/scope validation, and empty
  states;
- comparison options, re-pin validation, redirect location, and cross-tab
  state;
- unchanged JSON API behavior.

Prefer semantic assertions for fragment behavior. Use golden files only for
stable complete fragments where a markup review is valuable; avoid large
full-page snapshots that obscure meaningful changes.

### Browser tests

Retain Playwright for critical cross-layer behavior:

- create, reply, transition, quick close, and 409 conflict;
- stale reattachment with a fresh browser selection;
- exact/moved/stale highlighting and source navigation;
- active/inactive filtering and count updates;
- document search/scope keyboard behavior and narrow layout;
- comparison re-pin and URL/mode preservation;
- panel/diff resize, Mermaid, CSP, and offline assets.

After unit and fragment tests cover branches, remove only browser cases that
are exact duplicates and provide no cross-layer signal.

## Build and verification

The target frontend commands are:

```sh
npm run typecheck
npm run test:unit
npm run build:web
npm run check:web
npm run test:browser
```

`check:web` will run typecheck, unit tests, and the reproducible frontend
build. Go verification remains:

```sh
go test ./...
go vet ./...
go test -race ./...
```

A commit runs the narrow tests needed while editing and the full applicable
checks before handoff. Browser tests require a working configured browser; an
environment launch failure must be reported separately from an application
test failure and recorded in `project_status.md`.

## Commit review protocol

For every gate:

1. Start from the maintainer-approved preceding commit.
2. Implement only the named slice and its tests/documentation.
3. Regenerate checked-in assets when authored frontend files change.
4. Run the checks listed for that gate and `git diff --check`.
5. Commit the complete slice with no unrelated changes.
6. Report the commit hash, concise diff summary, checks, known limitations, and
   next proposed gate.
7. Stop until the maintainer explicitly says to proceed.

Do not combine gates, pre-implement later slices, or silently amend a commit
that the maintainer has begun reviewing. If review feedback changes a commit,
use a focused follow-up commit unless the maintainer explicitly requests an
amend or rebase.

## Ordered commit plan

### Commit 0: approve the design and gates

Scope:

- add this design;
- add milestone 17 and the commit gates to `project_status.md`;
- link the plan from README, build, and architecture documentation;
- record the one-commit-at-a-time review protocol.

Checks: Markdown/link inspection, `git diff --check`, `go test ./...`, and
`npm run typecheck`.

### Commit 1: add the TypeScript unit-test harness

Scope:

- add pinned Vitest and `happy-dom` development dependencies;
- add `test:unit` and update `check:web`;
- configure production TypeScript emission to exclude tests;
- extract and test one surviving pure primitive from selection/highlight code
  without changing runtime behavior;
- update README and build commands.

Checks: `npm run test:unit`, `npm run check:web`, `go test ./...`.

### Commit 2: vendor and serve HTMX without activating it

Status: implemented and approved.

Scope:

- vendor HTMX 2.0.10 and its license under `web/vendor/htmx/`;
- embed and serve it at `/static/htmx.min.js`;
- add checksum/provenance documentation and static-route/CSP tests;
- do not load it from `page.html` yet.

Checks: `go test ./internal/server ./web`, `go test ./...`.

### Commit 3: add inactive fragment templates and view models

Status: implemented and approved.

Scope:

- parse the page and fragment templates as one embedded template set;
- add annotation panel/card/action view models;
- render fragment templates in tests, not production routes;
- cover escaping, statuses, threads, stale anchors, and action availability;
- keep the current browser renderer active.

Checks: focused server/template tests, `go test ./...`.

### Commit 4: share annotation read/create operations and add UI handlers

Status: implemented and approved.

Scope:

- extract typed read/create application operations from JSON handlers;
- keep JSON status codes, headers, and bodies unchanged;
- add secured HTML read/create handlers and tests;
- register the UI routes, but do not point the page at them yet.

Checks: focused API compatibility and UI handler tests, `go test ./...`, race
test for the affected packages.

### Commit 5: share reply/transition operations and add UI handlers

Status: implemented and approved.

Scope:

- extract typed reply and lifecycle application operations;
- add reply/transition HTML handlers including 422 and 409 fragments;
- preserve JSON API behavior and lifecycle tests;
- keep the current browser mutation path active.

Checks: annotation domain, API, UI handler, full Go, and affected race tests.

### Commit 6: share reattach operations and complete UI mutation coverage

Status: implemented and approved.

Scope:

- extract the typed reattach application operation;
- add the HTML reattach handler and safe hidden-field parsing;
- cover stale-only rules, source digest/range validation, conflict fragments,
  and unchanged JSON behavior.

Checks: annotation, renderer/server, full Go, and affected race tests.

### Commit 7: activate the HTMX annotation panel

Status: implemented and approved.

Scope:

- load embedded HTMX on review pages with safe configuration;
- add the typed header, expected-error swap, and post-swap adapters;
- switch annotation read and mutations to HTML fragments;
- keep selection/highlight/navigation behavior through delegated adapters;
- delete `review-render.ts`, `review-actions.ts`, `review-dom.ts`, and the
  browser-only portion of `review-api.ts` when unused;
- update generated assets, README, build, and architecture docs.

Checks: unit tests, `npm run check:web`, full Go tests, race tests, and focused
annotation Playwright tests.

### Commit 7 review follow-up A: establish typed browser state

Status: implemented and approved.

Scope:

- make semantic element IDs and typed state the replacement for custom HTML
  application data;
- add the inactive versioned viewer-state response and TypeScript runtime
  parser without changing current browser behavior;
- reject unknown schema values, malformed nested fields, and duplicate IDs;
- add a shrinking per-file baseline that rejects new authored `data-*`
  attributes and dataset consumers;
- update all source-of-truth documentation and stop for review.

Checks: parser unit tests, viewer-state handler tests, `npm run check:web`, full
Go/race tests, vet, and diff checks.

### Commit 7 review follow-up B: migrate annotation and source state

Status: implemented and approved.

Scope:

- give annotation cards, source spans, and diagrams semantic IDs;
- extend viewer state with source-node and diagram ranges;
- move panel revision, annotation locations, lifecycle behavior, document
  digest, selection preview, and temporary navigation state into typed maps and
  objects;
- remove the corresponding template, renderer, TypeScript, Sass, and test
  `data-*` uses and shrink the enforcement baseline;
- retain the verified HTMX, selection, highlight, navigation, lifecycle, and
  Mermaid behavior.

Implementation notes:

- `/ui/viewer-state` is now fetched during review initialization and after
  annotation mutations; `mode=diff` returns the current-side diff source map;
- renderer-owned `source-*` and `diagram-*` IDs are tested against the typed
  source map, while annotation and lifecycle IDs are shared server helpers;
- selection preview and temporary focus ownership stay in TypeScript memory;
  they are never serialized into presentation HTML;
- the only remaining custom-data baseline is document-tree/filter state and
  the active comparison commit, reserved for commits 8 and 9.

Checks: unit, renderer/server, `check:web`, Go/race, annotation and Mermaid
Playwright tests.

### Commit 7 review follow-up C: codify the DOM/state boundary

Status: implemented and approved.

Scope:

- define legitimate browser DOM inputs separately from application state;
- inventory remaining node-as-state logic across document, comparison, review,
  source-range, and Mermaid modules;
- split document state modeling from document-tree activation so each remains
  independently reviewable;
- require runtime validation at every JSON boundary and DOM-free pure state
  modules;
- update all handoff documentation and stop before implementation.

Checks: documentation consistency and diff checks.

### Commit 8A: establish typed document catalog state

Status: implemented. The maintainer approved continuing to 8B as part of the
combined commits 8–10 review sequence.

Scope:

- add a versioned, runtime-validated document catalog boundary containing
  identity, kind, selected state, changed state, navigation URL, and open-comment
  count;
- extract pure document tree construction, filtering, result ordering, counts,
  and status-label derivation into DOM-free TypeScript with unit tests;
- add an architecture test that keeps designated pure state modules free of
  browser globals, DOM types, selectors, and datasets;
- do not activate new rendering or filtering behavior in this commit.

Checks: parser and pure-state unit tests, catalog handler tests, `check:web`,
full Go/race, vet, and diff checks.

Implementation notes:

- inactive `GET /ui/document-state` returns a versioned ordered catalog and
  defaults an omitted document to the same index selection as `/`;
- `document-state.ts` validates unknown wire data, cross-field capability
  invariants, unique paths, and selected identity before building its path map;
- `document-catalog.ts` contains only typed tree/filter/count/status rules and
  has no DOM dependency;
- Go and TypeScript tests cover endpoint behavior, parser rejection, nested
  tree construction, combined scopes, stable ordering, summaries, and the
  pure-module architecture guard.

### Commit 8B: server-render the document tree and benchmark filtering

Status: implemented. A 5,000-path nested-tree filter and template-render
benchmark measured 4.08 ms/op on Apple M1, below the 50 ms acceptance
threshold; server filtering is active behind a 150 ms debounce.

Scope:

- add recursive document-tree and summary templates;
- add the filtered document UI handler and 5,000-path benchmark;
- activate debounced server filtering if the acceptance threshold is met;
- retain a small keyboard/directory-preference view adapter driven by typed
  catalog/filter results;
- remove flat `<li>` enumeration, DOM-derived visible-result logic,
  `document-tree.ts`, manual badge mutation, source-mode link rewriting, and
  the remaining document datasets when unused;
- update generated assets and documentation with the measured decision.

Checks: unit, benchmark, server, `check:web`, Go/race, navigation Playwright.

Implementation notes:

- `GET /ui/review/documents` returns one recursive, escaped panel fragment;
- server state owns filtering, URLs, open-comment counts, and tree structure;
- the adapter uses programmatic HTMX swaps, validated catalog state for Enter,
  and semantic directory IDs for tab-local presentation preferences;
- flat DOM catalog reconstruction, manual count badges, document datasets,
  source-mode link rewriting, and `document-tree.ts` are removed.

### Commit 9: server-render the comparison selector

Status: implemented.

Scope:

- add the comparison template and form handler;
- runtime-validate comparison JSON while both paths coexist;
- activate HTML selection and preserve the current URL/mode;
- remove the comparison JSON renderer from `viewer.ts` while retaining JSON
  API compatibility;
- update generated assets and documentation.

Checks: comparison unit/handler/concurrency tests, `check:web`, full Go/race,
comparison Playwright.

Implementation notes:

- the initial page renders the bounded option list and selected commit;
- `POST /ui/review/git-comparison` reuses the guarded comparison operation and
  returns `HX-Refresh`, preserving the current document and mode;
- TypeScript only supplies the comparison token header and submit-on-change;
- the JSON API remains compatible, and `comparison-state.ts` provides a tested
  validator for consumers of its unknown wire payload;
- the final custom `data-*` state attribute is removed.

### Commit 10: make the remaining TypeScript explicitly instantiable

Status: implemented.

Scope:

- replace whole-file IIFEs with small entrypoints and exported initializers;
- inject roots, storage, location, observers, and narrow browser ports;
- extract remaining pure selection/highlight/preference functions;
- replace visible-card, mutation-class, conflict-text, active-link,
  source-span-order, and Mermaid-source inference with typed state or explicit
  interaction inputs;
- add delegated listeners and lifecycle cleanup;
- add typed lint rules only when the code is ready to pass them without broad
  suppressions;
- update generated assets and frontend architecture documentation.

Checks: unit/lint/typecheck/build, full Go/race, complete Playwright suite.

Implementation notes:

- viewer, review, and Mermaid entrypoints are named initializers; viewer and
  Mermaid accept explicit browser/service ports for isolated tests;
- viewer interfaces and helpers are module-scoped; the initializer is a thin
  composition root, and handlers receive shared state through typed contexts;
- directory expansion is held in a typed ID set and projected onto each
  server-rendered fragment rather than reconstructed from DOM classes; the
  complete adapter is isolated in `document-tree.ts`;
- `viewer.ts` is reduced to composition; document search, layout, comparison,
  diff-divider, environment, and storage behavior each have a focused module;
- root-page document identity comes from typed catalog state rather than an
  active link, and visible annotation decisions come from typed statuses;
- HTMX reports explicit create/reattach/other mutation kinds, so review logic
  does not infer operations from presentation classes or conflict text;
- Mermaid definitions and source ordering come from typed viewer-state maps;
- pure preference resolution and clamping are extracted into a DOM-free module
  covered by unit tests and the state-module architecture guard.

### Commit 11: close the milestone and refresh release artifacts

Scope:

- confirm the handwritten TypeScript target and delete dead modules/assets;
- remove browser tests made redundant by lower-level coverage;
- run the complete verification matrix;
- update README, build, architecture, status, and this design to implemented;
- refresh `dist/` only after the maintainer approves the source/docs state.

Checks: `npm run check:web`, complete Playwright, `go test ./...`,
`go vet ./...`, `go test -race ./...`, generated-output clean check,
cross-platform builds, and `git diff --check`.

## Acceptance criteria

- Annotation cards, forms, threads, counts, document tree, and comparison
  options are rendered by Go templates.
- HTMX is embedded, offline, pinned, licensed, CSP-compatible, and used without
  extensions or inline evaluated code.
- Existing JSON APIs and the agent workflow remain compatible.
- Conflicts display authoritative state and never retry automatically.
- Remaining TypeScript has explicit initializers and unit-tested pure rules.
- Handwritten browser TypeScript is no more than 1,200 lines.
- README, build, architecture, project status, and this design agree with the
  implemented state.
- All required checks pass, or an environment-only browser limitation is
  documented and explicitly accepted by the maintainer.
- Every commit was reviewed and approved before the next commit began.

## References

- [HTMX 2 documentation](https://htmx.org/docs/)
- [HTMX 2.0.10 changelog](https://github.com/bigskysoftware/htmx/blob/master/CHANGELOG.md)
- [TypeScript type assertions](https://www.typescriptlang.org/docs/handbook/2/everyday-types.html#type-assertions)
