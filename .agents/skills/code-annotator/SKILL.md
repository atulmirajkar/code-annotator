---
name: code-annotator
description: Process code-annotator annotations when asked to discover, implement, discuss, or report review work. Use the live HTTP API whenever a review server is running; use offline commands only after confirming no server is active, and never write sidecars directly.
---

# code-annotator

Choose exactly one operating mode before reading or mutating annotations:

- If the user supplies a viewer URL or says a review server is running, use the
  live HTTP workflow. Never substitute offline commands.
- If server state is unclear and no URL was given, run `agent discover` (see
  below) before asking. Only ask the user when discovery itself cannot
  resolve to exactly one server.
- Use the offline workflow only when the user confirms no review server is
  running, or `agent discover` reports none.

Never write annotation sidecars directly in either mode.

## Live server workflow

From this repository use `go run ./cmd/code-annotator agent`; outside the source
tree use an installed `code-annotator agent` command.

0. If no viewer URL is already known, discover one instead of asking first:

   ```sh
   go run ./cmd/code-annotator agent discover [--root <content-root>]
   ```

   A single match prints `{"url":...}`; use that URL for every step below. If
   multiple servers are running, discovery first tries to auto-select the one
   whose content root contains the current working directory before giving
   up. Zero or multiple matches then exit non-zero: zero means no review
   server is running (fall back to the offline workflow or ask); multiple
   means more than one is running and neither working directory nor
   `--root` disambiguated it (ask the user which one, or add `--root`). This
   only ever probes a URL the server itself
   registered; it does not scan ports.
1. Load the cross-document queue:

   ```sh
   go run ./cmd/code-annotator agent queue \
     --url <viewer-url> --status open,needs_changes
   ```

   Each document has its own `revision`. Retain the revision belonging to the
   annotation's document for the next mutation.
2. Select only annotation IDs in scope. Use each document's `kind` and
   `language` to interpret the selected source, anchor state, and complete
   thread. A stale anchor means the old selection is no longer uniquely
   located; do not guess a replacement.
3. For a `needs_changes` annotation, choose the valid next transition after
   reviewing the request: acknowledge it as an agent to begin a retry, or
   reject it as an agent with a message when the requested change cannot or
   should not be performed. Before changing files, acknowledge an `open` or
   `needs_changes` annotation when proceeding with implementation:

   ```sh
   go run ./cmd/code-annotator agent resolve \
     --url <viewer-url> --document <document> \
     --revision <revision> --id <annotation-id> --status acknowledged \
     --role agent --author <agent-name>
   ```

   The mutation response contains the new `revision`. Use it for the next
   mutation, or reload the queue if browser activity may have intervened.
4. Make only the requested repository changes and run appropriate checks.
5. Report completed work with a concrete summary. Include `--commit` only when
   that commit already exists:

   ```sh
   go run ./cmd/code-annotator agent resolve \
     --url <viewer-url> --document <document> \
     --revision <revision> --id <annotation-id> --status applied \
     --role agent --author <agent-name> --summary <completed-work> \
     [--commit <commit>]
   ```

Use `reply` with the same document, revision, ID, and author arguments plus
`--message` for clarification or discussion that must not change lifecycle
state. Use `resolve --status rejected --message <reason>` when an agent rejects
an `open` or `needs_changes` request because it cannot or should not be
performed, not merely because clarification is needed.

```sh
go run ./cmd/code-annotator agent reply \
  --url <viewer-url> --document <document> --revision <revision> \
  --id <annotation-id> --author <agent-name> --message <question-or-context>
```

## Watching for new work

Nothing in this skill, the CLI, or the server makes an agent poll on its own.
A skill document is instructions read while already running; it cannot
schedule its own future invocation. If new or changed annotations should be
noticed without a human re-invoking this skill, that loop has to come from
the agent's own runtime or orchestration — a scheduled wakeup, `/loop`, a
cron job, a shell loop — calling `agent queue` on whatever interval the
caller chooses. This skill's job is only to make each individual check cheap,
never to decide whether or how often one happens.

Once such a loop exists, poll cheaply with `--etag`:

```sh
go run ./cmd/code-annotator agent queue \
  --url <viewer-url> --status open,needs_changes --etag <etag-from-last-poll>
```

The response is always `{"etag": "...", "modified": <bool>, "queue": {...}}`.
`modified: false` means nothing changed; do nothing else this tick, and carry
the same `etag` into the next call. `modified: true` means the `queue` field
holds a fresh response — proceed to step 2 of the live workflow using it, and
carry forward the new `etag`. Omit `--etag` on the first call of a new loop,
or whenever `--etag` is dropped, output reverts to the plain unwrapped queue
response used elsewhere in this document.

## Offline workflow

Use this only after confirming that no review server is running. From this
repository use `go run ./cmd/code-annotator annotations`; outside the source tree use
an installed `code-annotator annotations` command.

Discover actionable work with:

```sh
go run ./cmd/code-annotator annotations export \
  --root <content-root> --status open,needs_changes
```

Add `--include-code` (or `--code-extensions <csv>`) when the stopped viewer's
catalog included source files. Use the same `--exclude-dirs` value used to
launch it so the offline queue matches the live catalog.

Use `annotations resolve` and `annotations reply` with the same lifecycle rules
as the live workflow. Supply `--root`, the stable annotation `--id`, and any
explicit `--annotations-dir` used by the reviewer. Repeat the same code-catalog
flags on these mutation commands when processing a source file. The offline
commands load a sidecar revision immediately before saving and reject concurrent
changes. If a conflict occurs, export again and reconsider the action instead
of retrying blindly.

For example, acknowledge before editing and then report completed work:

```sh
go run ./cmd/code-annotator annotations resolve \
  --root <content-root> --id <annotation-id> \
  --status acknowledged --role agent --author <agent-name>

go run ./cmd/code-annotator annotations resolve \
  --root <content-root> --id <annotation-id> \
  --status applied --role agent --author <agent-name> \
  --summary <completed-work> [--commit <commit>]
```

## Preserve the review contract

- Never edit `.code-annotator` JSON sidecars directly and never use offline reads or
  mutations while the review server is running.
- Never create a replacement annotation for a retry. Continue with the same ID
  and read the reviewer's latest `needs_changes` message before acknowledging.
- Do not close annotations. Only a reviewer may move `applied` to `closed` or
  return it as `needs_changes`.
- Treat `409` as new information: reload the queue, reread status and thread,
  and reconsider the action. Never blindly repeat a stale mutation.
- Do not expose or print the review token. The Go client reads it from the
  supplied loopback viewer page and keeps it internal.
- Report processed annotation IDs and final states to the user.

Offline `code-annotator annotations` commands are the agent handoff only when no
review server is running; the live HTTP API remains authoritative otherwise.
