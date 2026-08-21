---
name: md-viewer-annotations
description: Process annotations through a running md-viewer review server when asked to discover, implement, discuss, or report annotation work. Use the live HTTP API client and never write sidecars directly.
---

# md-viewer annotations

Require the user to provide the URL of the running review-mode viewer. From
this repository use `go run ./cmd/md-viewer agent`; outside the source tree use
an installed `md-viewer agent` command. This client talks only to the live HTTP
API so browser and agent activity share the webserver's revision checks. Do not
substitute the offline `annotations` command family while that server is live.

## Process actionable work

1. Load the cross-document queue:

   ```sh
   go run ./cmd/md-viewer agent queue \
     --url <viewer-url> --status open,needs_changes
   ```

   Each document has its own `revision`. Retain the revision belonging to the
   annotation's document for the next mutation.
2. Select only annotation IDs in scope. Read the original comment, selected
   Markdown, anchor state, and complete thread. A stale anchor means the old
   selection is no longer uniquely located; do not guess a replacement.
3. Before changing files, acknowledge an `open` or `needs_changes` annotation:

   ```sh
   go run ./cmd/md-viewer agent resolve \
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
   go run ./cmd/md-viewer agent resolve \
     --url <viewer-url> --document <document> \
     --revision <revision> --id <annotation-id> --status applied \
     --role agent --author <agent-name> --summary <completed-work> \
     [--commit <commit>]
   ```

Use `reply` with the same document, revision, ID, and author arguments plus
`--message` for clarification or discussion that must not change lifecycle
state. Use `resolve --status rejected --message <reason>` only when the request
cannot or should not be performed, not merely because clarification is needed.

```sh
go run ./cmd/md-viewer agent reply \
  --url <viewer-url> --document <document> --revision <revision> \
  --id <annotation-id> --author <agent-name> --message <question-or-context>
```

## Preserve the review contract

- Never edit `.md-viewer` JSON sidecars directly and never use offline mutation
  commands while the review server is running.
- Never create a replacement annotation for a retry. Continue with the same ID
  and read the reviewer's latest `needs_changes` message before acknowledging.
- Do not close annotations. Only a reviewer may move `applied` to `closed` or
  return it as `needs_changes`.
- Treat `409` as new information: reload the queue, reread status and thread,
  and reconsider the action. Never blindly repeat a stale mutation.
- Do not expose or print the review token. The Go client reads it from the
  supplied loopback viewer page and keeps it internal.
- Report processed annotation IDs and final states to the user.

Offline `md-viewer annotations` commands are maintenance tools for times when
no review server is running; they are not the live agent handoff contract.
