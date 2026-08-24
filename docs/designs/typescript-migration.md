# TypeScript Frontend Migration

## Status

Implemented. This document records the completed migration from browser
JavaScript modules to TypeScript. The later server-rendered review UI migration
is authoritative for subsequent frontend work; it moved `web/page.html` to
`web/templates/page.html` and added inactive fragment templates. See
[`server-rendered-review-ui.md`](server-rendered-review-ui.md) and
[`../architecture.md`](../architecture.md) for the current layout.

## Goals

- Make the browser code easier to understand by making data and DOM contracts
  explicit.
- Preserve the current review workflow and browser behavior.
- Preserve the single Go binary and `go:embed` asset model.
- Keep the current module boundaries visible in source and in the generated
  browser assets.
- Make API payload changes fail during type checking instead of surfacing only
  as runtime UI errors.
- Keep browser tests in JavaScript during the first production migration.

## Non-goals

- Introducing a frontend framework.
- Replacing the current DOM-based rendering approach.
- Bundling all frontend code into one opaque file.
- Changing the annotation API or its JSON schema.
- Converting Playwright tests in the first migration.
- Converting the vendored Mermaid distribution.

## Current Runtime Shape

The Go server embeds individual browser assets from `web/` and serves each
application module at its own `/static/*.js` route. `page.html` loads
`viewer.js` on every page and loads `review.js` as an ES module on review pages.
The review modules import one another using browser-visible `.js` paths.

The current production frontend is approximately 1,756 lines across these
files:

```text
web/
  review.js
  review-api.js
  review-actions.js
  review-dom.js
  review-highlights.js
  review-navigation.js
  review-panel.js
  review-render.js
  review-selection.js
  review-thread.js
  viewer.js
  mermaid.js
```

`browser-tests/` contains the Playwright regression suite. Those tests use
CommonJS and launch the Go server directly; they are intentionally outside the
first production conversion.

## Proposed Source Layout

TypeScript and Sass source will live under `web/src/`. Generated browser
JavaScript and CSS will live under `web/generated/`.

```text
web/
  src/
    styles.scss
    styles/
      _base.scss
      _content.scss
      _layout.scss
      _responsive.scss
      _review.scss
    review.ts
    review-api.ts
    review-actions.ts
    review-dom.ts
    review-highlights.ts
    review-navigation.ts
    review-panel.ts
    review-render.ts
    review-selection.ts
    review-thread.ts
    viewer.ts
    mermaid.ts
    types.ts
  generated/
    styles.css
    review.js
    review-api.js
    review-actions.js
    review-dom.js
    review-highlights.js
    review-navigation.js
    review-panel.js
    review-render.js
    review-selection.js
    review-thread.js
    viewer.js
    mermaid.js
```

Generated files are checked in. This is deliberate: Go builds and tests must
continue to work without requiring Node or npm, and the generated files are
part of the embedded single-binary artifact. CI will run the frontend build and
fail if it produces a diff, which keeps generated assets reproducible.

The existing `page.html` and vendored Mermaid files remain in their current
locations. Styles are authored as Sass under `web/src/styles/` and emitted to
`web/generated/styles.css`. `web/embed.go` embeds generated JavaScript and CSS
files instead of authored source files.

Sass conventions: `_tokens.scss` owns compile-time typography, spacing, and
control values; mixins are reserved for repeated control surfaces and primary
actions; component partials use nesting for owned descendants, pseudo-states,
and variants. Runtime theme values remain CSS custom properties so light, dark,
and dynamic topbar-height behavior can continue to change in the browser.

## Module Graph

The dependency direction is intentionally one-way. Shared types are leaf data
definitions and do not import browser or application modules.

```text
                         page.html
                             |
             +---------------+----------------+
             |                                |
          viewer.js                         review.js
        (standalone)                  (review composition root)
                                             |
       +------------------+------------------+------------------+
       |                  |                  |                  |
   review-api        review-actions     review-render       review-selection
       |                  |                  |                  |
       |          +-------+-------+      +---+---+            |
       |          |               |      |       |            |
       |      review-dom   review-thread review-dom       types
       |                                  review-thread      |
       +----------------------------------------------------+
       |
   types.ts

   review-highlights -> review-dom, types
   review-navigation -> types
   review-panel      -> types (callback and DOM contracts)
   mermaid.js         (standalone global Mermaid integration)
```

More precisely, the review entrypoint imports:

```text
review.ts
  -> review-api.ts
  -> review-actions.ts
       -> review-api.ts
       -> review-dom.ts
       -> review-thread.ts
  -> review-highlights.ts
       -> review-dom.ts
  -> review-navigation.ts
  -> review-panel.ts
  -> review-render.ts
       -> review-dom.ts
       -> review-thread.ts
  -> review-selection.ts
```

The following rules keep the graph understandable:

- `review.ts` wires controllers together and owns page-level lifecycle.
- `review-api.ts` is the only module that knows endpoint URLs and HTTP
  mutation headers.
- `types.ts` owns shared API models, request payloads, string unions, and
  callback contracts.
- `review-thread.ts` owns lifecycle transition rules and thread display
  policy. It does not touch the DOM.
- `review-dom.ts` contains small DOM construction helpers only.
- `review-render.ts` converts typed API models into DOM nodes. It does not make
  API requests.
- `review-actions.ts` creates forms and submits mutations through
  `review-api.ts`.
- `review-selection.ts`, `review-highlights.ts`, and
  `review-navigation.ts` own source-selection behavior and receive typed
  callbacks from the composition root.
- `review-panel.ts` owns panel visibility, resizing, and form status UI.
- `viewer.ts` remains independent from review mode and must not import review
  modules.
- `mermaid.ts` remains independent and consumes the global Mermaid library
  loaded by `page.html`.

## Shared Type Boundaries

`types.ts` should define the browser representation of the server's annotation
JSON. It should use string unions rather than unrestricted strings for values
with a fixed lifecycle vocabulary.

The initial models should include:

```ts
export type AnnotationIntent =
  | "question"
  | "suggestion"
  | "change_request"
  | "approval";

export type AnnotationStatus =
  | "open"
  | "acknowledged"
  | "applied"
  | "needs_changes"
  | "closed"
  | "rejected";

export type ActorRole = "agent" | "reviewer";

export interface AnnotationPayload {
  document: string;
  revision: string;
  annotations: Annotation[];
}

export interface Annotation {
  id: string;
  intent: AnnotationIntent;
  status: AnnotationStatus;
  comment: string;
  author: string;
  source?: Source;
  anchor?: AnchorResult;
  thread: ThreadEntry[];
}
```

The exact fields must be checked against `internal/annotation/model.go` and
`internal/server/annotations.go` while implementing. The browser response
types should model the response actually sent by the server, including
optional fields such as stale anchors. They should not blindly mirror every Go
internal field.

Request payloads should be separate from response models. For example,
`CreateAnnotationRequest`, `ReplyRequest`, `TransitionRequest`, and
`SelectionPayload` should not reuse `Annotation` with optional fields. This
prevents invalid request shapes from becoming easy to construct.

`JSON.parse` remains an untrusted boundary. The first migration may type the
decoded response with a narrow assertion after checking the HTTP response, but
future work may add runtime validation if the API becomes externally consumed.

## DOM Typing Rules

The current code relies heavily on `querySelector`, `dataset`, forms, and event
delegation. TypeScript should make these boundaries explicit without hiding
them behind a large abstraction.

- Use `querySelector<HTMLElement>(...)` where the element type is known.
- Check required elements at the composition root and return early with a
  clear invariant failure when the template is incomplete.
- Use small helper functions for repeated required-element lookups.
- Keep optional controls typed as nullable and handle the absent-control case
  explicitly.
- Narrow `EventTarget` before reading `value`, `checked`, `files`, or form
  fields.
- Define typed helpers for the known annotation forms rather than casting every
  `FormData.get()` result independently.
- Treat `dataset` values as strings and parse byte offsets and booleans at the
  point of use.
- Type `sourceRange` and selection callbacks around `Range`, `Element`, and
  `Selection` instead of returning generic `any` values.
- Use `unknown` in catch clauses and convert errors through a small error
  message helper.

The migration should not use `any` for API models, controller arguments, or
returned values. A narrowly scoped DOM cast is acceptable when the HTML
template is the source of truth and the alternative would obscure the code.

## TypeScript Compiler Configuration

The initial `tsconfig.json` should be intentionally conservative:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "Bundler",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "rootDir": "web/src",
    "outDir": "web/generated",
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "exactOptionalPropertyTypes": true,
    "useUnknownInCatchVariables": true,
    "noEmitOnError": true,
    "verbatimModuleSyntax": true,
    "forceConsistentCasingInFileNames": true,
    "skipLibCheck": true
  },
  "include": ["web/src/**/*.ts"],
  "exclude": ["node_modules", "browser-tests"]
}
```

Source imports should continue to use `.js` extensions:

```ts
import { Annotation } from "./types.js";
```

TypeScript resolves this to `types.ts` during compilation and emits a browser
import of `./types.js`, which matches the generated output and native browser
module behavior.

`noUncheckedIndexedAccess` and `exactOptionalPropertyTypes` are useful here
because the frontend has many optional server fields and array lookups. If
either option creates excessive noise in an individual module, the code should
be made more explicit rather than weakening the project-wide setting.

## Build Process

The initial build should use the TypeScript compiler directly, without a
bundler:

```text
npm run typecheck
  tsc --noEmit

npm run build:web
  tsc

go test ./...
  compiles the checked-in generated assets through go:embed

npm run test:browser
  runs the existing Playwright regression suite against the generated assets
```

Recommended package scripts:

```json
{
  "scripts": {
    "typecheck": "tsc --noEmit",
    "build:styles": "sass web/src/styles.scss web/generated/styles.css --style=expanded --no-source-map",
    "build:web": "tsc && npm run build:styles",
    "format:styles": "prettier --write web/src/styles.scss web/src/styles/*.scss",
    "watch:web": "tsc --watch",
    "watch:styles": "sass --watch web/src/styles.scss:web/generated/styles.css --no-source-map",
    "check:web": "npm run typecheck && npm run build:web",
    "test:browser": "playwright test"
  }
}
```

The generated directory should contain only compiler output. It should not
contain source maps or declaration files in the first phase. If debugging
generated code becomes difficult, source maps can be added later with an
explicit decision about whether they belong in the embedded binary.

The Go build should not invoke npm implicitly. This preserves predictable Go
tooling and offline builds. Repository and CI workflows should run
`npm run check:web` before `go test ./...`, and CI should verify that the
generated directory is clean after compilation.

For local frontend development, `npm run watch:web` keeps generated assets
current while TypeScript files are edited. The Go server currently reads the
generated assets at startup, so the server must be restarted after a frontend
change before the browser can observe it.

## Go Embedding Changes

`web/embed.go` will embed `web/generated/*.js` using paths relative to the
`web` package, for example:

```go
//go:embed page.html generated/review.js generated/review-api.js ...
var Files embed.FS
```

`internal/server/server.go` should read the generated paths but continue to
serve the same URL paths:

```text
web/generated/review.js          -> /static/review.js
web/generated/review-api.js      -> /static/review-api.js
web/generated/viewer.js          -> /static/viewer.js
```

The public runtime URLs do not change. Existing server tests should continue to
assert those URLs and content types. They should be updated only where they
currently inspect implementation details that TypeScript compilation changes,
such as a source-level `export function` string.

`page.html` keeps its current script tags. The review entrypoint remains an ES
module, and browser-visible relative imports continue to resolve because the
generated files preserve the current names and directory layout.

## Migration Phases

### Phase 1: Tooling and generated asset path

- Add TypeScript and compiler scripts to `package.json`.
- Add `tsconfig.json`.
- Add `web/src` and `web/generated` conventions.
- Prove compilation with one small leaf module.
- Update `web/embed.go` and server loading to read generated output.

### Phase 2: Shared models and leaf modules

- Add `types.ts`.
- Convert `review-dom.ts` and `review-thread.ts`.
- Convert `review-api.ts` using typed request and response payloads.
- Confirm typecheck and browser tests before converting controllers.

### Phase 3: Controllers and rendering

- Convert `review-render.ts`.
- Convert `review-actions.ts`.
- Convert `review-selection.ts`, `review-highlights.ts`, and
  `review-navigation.ts`.
- Convert `review-panel.ts`.
- Convert `review.ts` last within the review graph so its controller contracts
  are already established.

### Phase 4: Standalone scripts

- Convert `viewer.ts`.
- Convert `mermaid.ts` with a minimal declaration for the global Mermaid API
  used by the integration.
- Keep the vendored Mermaid file unchanged.

### Phase 5: Cleanup and documentation

- Remove the authored JavaScript files after all runtime references use the
  generated directory.
- Update `docs/build.md` and `docs/architecture.md` with the npm prerequisite
  and generated-asset workflow.
- Add a CI check that generated assets are reproducible.
- Consider converting Playwright tests to TypeScript as a separate change.

## Verification and Acceptance Criteria

The migration is complete when:

- `npm run typecheck` succeeds with `strict: true` and no production `any`
  escape hatches.
- `npm run build:web` produces the generated assets with stable names.
- `go test ./...` succeeds without requiring a running Node process.
- `npm run test:browser` passes unchanged.
- Read-only pages still load `viewer.js` and Mermaid behavior is unchanged.
- Review pages still load every review module and support creating, replying to,
  transitioning, reattaching, filtering, and navigating annotations.
- The generated asset check produces no working-tree diff in CI.
- The Go server continues to expose the same `/static/*.js` URLs and content
  types.

## Risks and Decisions

### Checked-in generated files

This adds generated-file churn, but it preserves the repository's current
offline Go build behavior and makes `go:embed` deterministic. A later build
system can move generation into release packaging if the project accepts a
Node prerequisite for all Go builds.

### No bundler in the first phase

Direct `tsc` output keeps module boundaries and existing server handlers
intact. It does not optimize or bundle the assets, but the current application
already serves separate modules and the migration is primarily about type
contracts and readability.

### Server response drift

TypeScript interfaces do not validate JSON at runtime. The source of truth
remains the Go handlers and annotation model. Any future API schema change
should update the Go response tests, `web/src/types.ts`, and the relevant
request/response tests together.

### Browser compatibility

The current code already uses modern browser APIs such as CSS Highlights,
`replaceChildren`, and `TextEncoder`. Targeting ES2022 is consistent with that
baseline. If the supported browser matrix changes, adjust `target` and add a
transpilation strategy as a separate decision.
