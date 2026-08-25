# TypeScript conventions

This file is the repository-level guide for TypeScript changes. Apply these
rules to `web/src/**/*.ts` and keep them consistent with `README.md`,
`docs/architecture.md`, `docs/build.md`, and the active design document.
Apply the guide to code touched by the current task; do not create unrelated
formatting or refactoring churn merely to modernize older code.

## Optimize for the next reader

- Prefer direct, unsurprising code over compressed expressions.
- Give functions, types, variables, and modules names that describe their
  responsibility rather than their implementation detail.
- Keep one level of abstraction within a function. Extract policy or a
  multi-step operation instead of mixing it with event wiring.
- Use early returns to keep the main path flat.
- Use braces for multiline conditionals.
- Format TypeScript with the repository's Prettier version. Do not hand-format
  generated JavaScript.

## Keep composition roots thin

Entrypoints such as `viewer.ts` and `review.ts` should resolve dependencies and
wire focused components. Feature behavior belongs in a module named for that
responsibility.

- Declare interfaces, type aliases, constants, and named functions at module
  scope. Do not declare them inside another function.
- Split a file when it owns multiple independent interaction areas.
- Export the smallest useful entrypoint from each component, normally a named
  `bind...`, `create...`, `initialize...`, or `start...` function.
- Use `start()` for a controller lifecycle. Do not use a method named `bind()`.

## Pass dependencies explicitly

- Pass `Document`, `Window`, `Location`, `Storage`, observers, and third-party
  APIs through typed parameters or environment interfaces.
- Do not reach into `window`, `document`, `sessionStorage`, `localStorage`, or
  an undeclared third-party global from feature code.
- Keep third-party ports narrow. Describe only the API surface this project
  calls.
- Use a focused typed context when several handlers share mutable state. Do not
  use a context as a dumping ground for unrelated dependencies.

```ts
interface SearchContext {
    document: Document;
    storage: Storage;
    scope: DocumentScope;
}

document.addEventListener("change", (event) =>
    handleScopeChange(context, event),
);
```

Use modern arrow callbacks to adapt event or observer arguments. Do not use
`Function.prototype.bind` to prefill callback parameters.

```ts
const observer = new ResizeObserver(() => updateTopbarHeight(document, topbar));
```

Keep the underlying operation named and independently understandable. Inline
substantial behavior only when it is genuinely local and trivial.

## Keep application state out of HTML

HTML is for presentation, semantic structure, form controls, accessibility,
and stable element IDs. It is not an application-state database.

- Do not store application data in custom `data-*` attributes.
- Do not reconstruct domain state from classes, text, visibility, element
  order, links, or ARIA attributes.
- Use runtime-validated TypeScript state from the server for document,
  annotation, lifecycle, comparison, source-location, and navigation data.
- Use semantic IDs only to join typed state to rendered elements.
- Reading an explicit user interaction value such as a checked form control is
  allowed. Treat it as input, not as the authoritative domain model.
- Keep transient presentation state, such as expanded directory IDs, in a
  typed in-memory structure and render it to the DOM in one direction.

## Prefer server-rendered UI

- Let Go own markup, filtering, lifecycle permissions, counts, navigation URLs,
  and validation.
- Use HTMX for server-rendered fragment replacement when an interaction does
  not require rich client-side computation.
- Keep TypeScript adapters focused on browser-only behavior: focus, selection,
  keyboard shortcuts, observers, drag gestures, request timing, and tab-local
  preferences.
- After an HTMX swap, reapply typed browser presentation state. Do not inspect
  the replacement fragment to rebuild state.

## Use TypeScript deliberately

- Prefer interfaces for object contracts and explicit union types for bounded
  states.
- Treat server responses and third-party event detail as `unknown` until a
  runtime validator or type guard narrows them.
- Avoid `any`, non-null assertions, broad type assertions, and optional chaining
  that silently hides a required template element.
- Fail clearly for required template elements. Gracefully skip genuinely
  optional features.
- Prefer `const`; use `let` only for state that actually changes.
- Use `ReadonlyArray`, `ReadonlyMap`, or readonly fields where mutation is not
  part of the contract.
- Centralize repeated storage keys, bounds, and timing values as named
  constants.

## Make asynchronous intent explicit

- Use `await` when subsequent work depends on a promise.
- Return promises from async operations so callers can test or compose them.
- At a synchronous event boundary, use `void` only when a promise is
  intentionally fire-and-forget and rejection is handled internally.
- Do not use `void` as a substitute for error handling.
- Serialize or coalesce requests when concurrent responses could race to update
  the same fragment.

## Comment decisions and invariants

Comments should explain information the code cannot express by itself:

- who owns a piece of state;
- why an event is delegated;
- why a request is serialized;
- why a stable callback identity is retained;
- what remains usable after an error;
- where server-rendered and browser-owned responsibilities meet.

Do not add comments that merely restate a function name or individual
statement. Update comments when the invariant changes.

## Test at the right boundary

- Put DOM-free rules in focused modules with Vitest unit tests.
- Give DOM adapters small `happy-dom` contract tests when practical.
- Use Playwright for selection, layout, focus, HTMX, pointer, and real browser
  behavior.
- Add regression coverage when refactoring, even when behavior should remain
  unchanged.
- Keep imports and browser services injectable so tests do not require ambient
  globals or network calls.

Run before committing TypeScript changes:

```sh
npm run check:web
go test ./...
go vet ./...
```

Run the relevant Playwright file for a focused adapter change. Run the complete
browser suite when shared initialization, state, or multiple interaction areas
change:

```sh
npm run test:browser
```

## Generated modules and documentation

- Edit `web/src`, then run `npm run build:web`; never edit `web/generated`
  manually.
- A new ES module imported by a browser entrypoint must be embedded in
  `web/embed.go`, served from an explicit `/static/...` route, and covered by
  the server's static-asset test.
- Update README, build, architecture, design, and project-status documentation
  when behavior, module ownership, or contributor workflow changes.
- Keep commits focused and reviewable. Stop after each requested commit for
  maintainer review.

## Review checklist

Before considering a TypeScript change complete, verify:

- no interface, type alias, or named function is nested inside another function;
- no callback uses `.bind(...)` to inject dependencies;
- no domain state is inferred from rendered HTML;
- browser globals and third-party APIs are explicit dependencies;
- each module has one coherent responsibility;
- async errors and request races are handled;
- non-obvious invariants have useful comments;
- tests cover the changed behavior;
- generated assets and source-of-truth docs are current.
