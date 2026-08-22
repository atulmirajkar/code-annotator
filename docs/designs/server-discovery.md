# Design: agent server discovery

## Status

Implemented. This design defines milestone 13 in
[`../../project_status.md`](../../project_status.md), tracked after Source
syntax highlighting.

## Problem

The live agent workflow (see
[`annotations.md`](annotations.md#agent-handoff) and
[`code-review.md`](code-review.md#agent-handoff)) requires an agent to already
know the review server's loopback URL before it can call `agent queue`,
`agent resolve`, or `agent reply`. In practice a human has to read the URL off
their terminal or browser and paste it into the conversation every time,
because nothing about the running server is discoverable: `run()` binds an
OS-selected or `--port`-pinned loopback address, prints the URL, and opens a
browser, but writes nothing to disk that another process could find. An agent
also has no way to tell "no server is running" apart from "a server is
running but I wasn't told about it."

## Goals

- Let an agent find a running `--review` server's URL without a human
  supplying it, when exactly one such server is running.
- Work for any agent or tool that can run the `code-annotator` CLI, not only
  Claude Code, matching this project's existing "human and AI-agent handoff"
  framing.
- When multiple servers are running, prefer the one whose content root
  contains the agent's own working directory before asking, since that
  matches how an agent is actually invoked in practice.
- Fail safely to "ask the user" when discovery is genuinely ambiguous (zero or
  multiple candidates, and working directory didn't resolve it), rather than
  guessing.
- Self-heal after a server exits without cleaning up after itself (for
  example, `kill -9`), without requiring a human to notice and delete a stale
  entry.
- Keep the review mutation token exactly as protected as it is today: never
  written anywhere but served from the loopback HTML page.

## Non-goals

- Scanning loopback ports to find a server that never advertised itself. The
  registry only ever contains addresses a server chose to register; discovery
  only ever probes those specific addresses.
- Discovering non-review (read-only) viewer instances. Agents have nothing to
  do against a server with no annotation APIs, so only `--review` servers
  register.
- Cross-machine or cross-user discovery. The registry lives under the current
  user's per-user state directory.
- Replacing `--url` as an explicit override. A caller that already knows the
  URL can always supply it directly and skip discovery entirely.
- Notifying an agent about new or changed annotations after it has started
  working. Discovery only answers "where is the server"; it says nothing
  about "what changed since I last looked" — see
  [Watching for updates](#watching-for-updates).

## Proposed user experience

Starting a review server behaves exactly as before; registration is
automatic and silent on success:

```sh
code-annotator --review ./docs
```

An agent that does not already have a URL asks for one instead of asking a
human first:

```sh
code-annotator agent discover
```

```json
{"url":"http://127.0.0.1:54321/","root":"/Users/atul/docs","pid":41213}
```

If more than one review server is running, `discover` first tries to
disambiguate using the caller's own working directory: if exactly one
candidate's content root contains it, that one is used automatically, no
`--root` required. This covers the common case, since an agent almost always
runs from inside the project it is working on. Only when that still leaves
more than one candidate — or the caller's working directory isn't inside any
of them — does `discover` give up and report every candidate instead of
guessing:

```text
code-annotator: multiple running review servers found; pass --root to disambiguate or supply --url directly:
  http://127.0.0.1:54321/ (root: /Users/atul/docs)
  http://127.0.0.1:54890/ (root: /Users/atul/notes)
```

`--root <content-root>` resolves the ambiguity explicitly instead of relying
on the working directory. `<content-root>` is the same directory a human
passed as the positional argument when they started that particular server
(`code-annotator --review <content-root>`) — not necessarily the agent's own
current working directory, which is why the automatic working-directory match
above is a best-effort narrowing step, not the authority on which server is
meant:

```sh
code-annotator agent discover --root ./docs
```

Zero running servers is reported the same way any other "nothing to do"
condition is: a non-zero exit with a clear message, so the caller can fall
back to the offline workflow or ask a human whether one should be started.

## Discovery file format and location

One JSON file per running review-mode server, named by process ID, under a
per-user state directory:

```text
<state-dir>/servers/<pid>.json
```

`<state-dir>` defaults to `os.UserConfigDir()/code-annotator` (platform
config directory: `~/Library/Application Support` on macOS, `$XDG_CONFIG_HOME`
or `~/.config` on Linux, `%AppData%` on Windows) and can be overridden with
`CODE_ANNOTATOR_STATE_DIR`, which also makes automated tests hermetic without
depending on `$XDG_CONFIG_HOME` (Go's `os.UserConfigDir()` does not read it on
Darwin).

```json
{
  "schemaVersion": 1,
  "pid": 41213,
  "url": "http://127.0.0.1:54321/",
  "root": "/Users/atul/docs",
  "startedAt": "2026-08-21T22:10:00Z"
}
```

`root` is the same absolute, symlink-resolved path `content.Open` already
produces, so a `--root` filter that points at the same directory (even
through a different symlink) still matches.

The registry never contains the review mutation token. A discovering client
still fetches that from the loopback HTML page exactly as it does when the
URL is supplied directly by a human, so discovery only ever tells an agent
*where* to look, never grants it any additional access.

### Lifecycle

- Written once, after the server has bound its listener and is otherwise
  ready to serve, using the same atomic temp-file-plus-rename pattern already
  used for annotation sidecars, so a reader never observes a partially
  written entry.
- Removed via a deferred cleanup registered immediately after a successful
  write, so it fires on every `run()` exit path: the existing `ctx.Done()`
  graceful-shutdown branch and the `serveResult` error branch alike.
- A registration failure (for example, an unwritable state directory) is
  logged to stderr and otherwise non-fatal; discovery is a convenience, and a
  server must still be usable without it.
- A registry entry left behind by an unclean exit (a hard kill that skips the
  deferred cleanup) is detected and pruned the next time `agent discover`
  probes it and gets no live answer back — see below.

## Discovery mechanism

`agent discover`:

1. Reads every `*.json` file in the state directory. Malformed or
   future-schema entries are skipped rather than failing the whole command,
   so one broken file never blocks discovery of the rest.
2. For each entry, issues a short-timeout `GET <url>/healthz` — the same
   route the browser and other agent tooling already use — against that one,
   already-known address. An entry that fails this check is treated as
   stale and removed from the registry.
3. If `--root` was supplied, filters live entries to a matching, similarly
   symlink-resolved root. Otherwise, if more than one entry is still live,
   narrows to whichever candidates' roots contain the caller's own working
   directory; if that narrows to exactly one, it wins without needing
   `--root`.
4. Exactly one live match: prints it as JSON and exits 0. Zero: exits
   non-zero with a clear message. More than one: exits non-zero, listing
   every candidate's URL and root so the caller can retry with `--root` or
   fall back to `--url`.

Step 2 is the reason this design does not need to reason about PID liveness
or platform-specific process APIs at all: verifying a specific, already-known
loopback address is safe and cheap, and it is what actually matters (a
process that still exists but stopped serving is exactly as unusable to an
agent as one that has exited).

## Watching for updates

`agent discover` answers "where is the server" once, at the moment an agent
starts working. It does not tell an agent about annotations that open,
change, or get replied to afterward — there is no push channel here, and
`GET /api/annotations` has no `ETag`/`If-None-Match` support, so a repeated
`agent queue` call always costs a full JSON payload even when nothing
changed.

Noticing new or changed annotations without a human re-invoking the agent is
a client-side concern, not something this design or the server provides:
schedule `agent discover` (or a known `--url`) plus `agent queue` on an
interval. That scheduling is deliberately out of scope here — a skill
document or CLI cannot make an agent wake itself up; that has to come from
the agent's own runtime or orchestration (a scheduled wakeup, `/loop`, a cron
job, a shell loop). This design only ever answers "where is the server,"
once, at the moment an agent starts working.

Making each individual poll cheap once such a loop exists — an `ETag` on the
queue response so an unchanged poll costs a `304` with no body — is
server-side queue behavior, not discovery, so it was deliberately kept out of
scope here and designed separately in
[`queue-etag.md`](queue-etag.md).

## Security

- The registry file contains only `pid`, `url`, `root`, and `startedAt` —
  never the review mutation token, which remains readable only from the
  served loopback page.
- The state directory lives under the current user's own per-user config
  location, not a world-writable location, and entries are written with
  `0600` permissions.
- `agent discover` never contacts an address it did not read from its own
  registry; it does not scan or guess.
- A PID being reused by an unrelated process between a server's unclean exit
  and the next `discover` call is an accepted, extremely low-probability risk:
  the stale entry would still have to fail (or coincidentally pass) the
  `/healthz` probe against whatever now owns that old port, and either
  outcome is bounded to "an inaccurate `discover` result," not privileged
  access, since discovery never carries the token.

## Test strategy

- Unit tests for the `internal/discovery` package: register/list/cleanup
  round-trip, malformed and future-schema entries skipped, `Remove` is
  idempotent, all using `CODE_ANNOTATOR_STATE_DIR` for hermetic temp
  directories.
- Command tests for `agent discover` faking `/healthz` with
  `httptest.NewServer`, covering zero/one/many candidates, `--root`
  filtering (including the symlink-resolution case that mirrors
  `content.Open`), and stale-entry pruning when the fake server is closed
  before the probe.
- One end-to-end `internal/app` test that runs the real server with
  `--review` and asserts a registry entry appears while serving and is gone
  after the context is canceled, plus a companion test asserting no entry is
  written without `--review`.

## Implementation slices

1. `internal/discovery` package: `StateDir`, `Entry`, `Register`, `List`,
   `Remove`.
2. Wire `discovery.Register` into `internal/app/app.go` `run()`, gated on
   review mode, with deferred cleanup.
3. `agent discover` subcommand in `internal/commands/agent.go`: flag parsing,
   liveness verification, `--root` filtering, and selection output.
4. Update the `code-annotator` agent skill's live workflow to try discovery
   before asking a human for a URL.
5. Update `README.md` with the new subcommand, state directory, and the
   `CODE_ANNOTATOR_STATE_DIR` override.

## Acceptance criteria

- Starting `code-annotator --review <root>` registers a discoverable entry;
  stopping it (via `Ctrl-C`/`SIGTERM`) removes that entry.
- `agent discover` with exactly one live, matching server prints its URL as
  JSON and exits 0.
- `agent discover` with multiple live servers, run from inside exactly one of
  their content roots, auto-selects that one without needing `--root`.
- `agent discover` with zero matches, or multiple matches that working
  directory can't resolve, exits non-zero with a message that lets the caller
  either retry with `--root` or fall back to asking a human.
- An entry left behind by a server that exited without cleaning up (a hard
  kill) is pruned the next time `agent discover` runs, without manual
  intervention.
- A non-review server never registers an entry.
- `go build ./...`, `go vet ./...`, `go test ./...`, and
  `go test -race ./...` pass.
