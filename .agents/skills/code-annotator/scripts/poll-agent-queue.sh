#!/bin/sh
# Keep this script POSIX sh so it can run from an installed skill on macOS,
# Linux, and other environments without requiring Bash-specific features.
set -eu

# The URL is optional because the first step can discover a running review
# server. The remaining values are the defaults used by the agent skill.
url=''
root=''
status='open,needs_changes'
interval=30
once=false

# The watcher is intentionally easy to stop from an agent runtime. Exit 130
# also makes it clear to a supervising process that the stop was intentional.
trap 'exit 130' INT TERM

# Print the complete interface in one place. Invalid or incomplete arguments
# are usage errors rather than polling failures, so they exit immediately.
usage() {
    echo "usage: poll-agent-queue.sh [--url URL] [--root PATH] [--status STATES] [--interval SECONDS] [--once]" >&2
    exit 2
}

# Parse options without shifting positional values into environment variables.
# The helper has no positional arguments; every value is named explicitly.
while [ "$#" -gt 0 ]; do
    case "$1" in
        --url) [ "$#" -ge 2 ] || usage; url=$2; shift 2 ;;
        --root) [ "$#" -ge 2 ] || usage; root=$2; shift 2 ;;
        --status) [ "$#" -ge 2 ] || usage; status=$2; shift 2 ;;
        --interval) [ "$#" -ge 2 ] || usage; interval=$2; shift 2 ;;
        --once) once=true; shift ;;
        -h|--help) usage ;;
        *) usage ;;
    esac
done

# An interval of zero is useful for tests and for callers that control timing
# externally. Negative values and non-numbers would make sleep ambiguous.
case "$interval" in
    ''|*[!0-9]*) echo 'interval must be a non-negative integer' >&2; exit 2 ;;
esac

# jq reads the small polling envelope and extracts the queue only when it has
# changed. code-annotator is deliberately the only supported CLI entry point;
# the skill is distributed separately from the repository's Go source tree.
command -v jq >/dev/null 2>&1 || { echo 'jq is required' >&2; exit 2; }
command -v code-annotator >/dev/null 2>&1 || { echo 'code-annotator executable not found' >&2; exit 2; }
run_agent() { code-annotator agent "$@"; }

# Discover once, before entering the loop. Re-discovering on every tick could
# select a different server if multiple review sessions are registered.
if [ -z "$url" ]; then
    if [ -n "$root" ]; then
        url=$(run_agent discover --root "$root" | jq -r '.url')
    else
        url=$(run_agent discover | jq -r '.url')
    fi
    [ -n "$url" ] && [ "$url" != null ] || { echo 'agent discover returned no URL' >&2; exit 1; }
fi

# The CLI only returns the ETag envelope when --etag is supplied. "0" is a
# deliberately stale first value, forcing one full response and giving us the
# real ETag to carry into the next request. This value has no mutation meaning.
etag=0
while :; do
    # Keep the CLI response out of the agent runtime's stdout until we know it
    # is a valid changed queue. This prevents envelope metadata from being
    # mistaken for review work.
    response_file=$(mktemp "${TMPDIR:-/tmp}/code-annotator-queue.XXXXXX")
    poll_failed=false

    # A failed request is retryable in continuous mode. In --once mode it is
    # returned to the caller as a non-zero result below.
    if run_agent queue --url "$url" --status "$status" --etag "$etag" >"$response_file"; then
        # Every successful envelope must provide the next ETag, including an
        # unchanged response. Never advance the cursor from a malformed reply.
        if next_etag=$(jq -er '.etag // empty' "$response_file"); then
            # modified=false means the queue is unchanged: emit no stdout and
            # let the caller sleep until the next tick.
            if jq -e '.modified == true' "$response_file" >/dev/null; then
                # Only the raw queue object is useful to the agent runtime;
                # jq -c also guarantees one complete JSON value per change.
                if ! jq -ec '.queue' "$response_file"; then
                    echo 'queue response had no valid queue payload; retrying after interval' >&2
                    poll_failed=true
                fi
            fi
            if [ "$poll_failed" = false ]; then
                etag=$next_etag
            fi
        else
            echo 'queue response had no etag; retrying after interval' >&2
            poll_failed=true
        fi
        rm -f "$response_file"
    else
        rm -f "$response_file"
        echo 'agent queue failed; retrying after interval' >&2
        poll_failed=true
    fi

    # --once is primarily for schedulers and tests. It performs exactly one
    # request and preserves the request's success/failure status.
    if $once; then
        $poll_failed && exit 1
        exit 0
    fi

    # A failed poll uses the same interval as a normal poll, preventing a
    # stopped server or network problem from becoming a busy loop.
    sleep "$interval"
done
