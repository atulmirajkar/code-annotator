#!/usr/bin/env python3
"""Revision-safe client for a live md-viewer annotation review session."""

import argparse
import ipaddress
import json
import sys
import urllib.error
import urllib.parse
import urllib.request
from html.parser import HTMLParser


class ReviewTokenParser(HTMLParser):
    """Extract the review token without depending on HTML attribute order."""

    def __init__(self):
        super().__init__()
        self.token = ""

    def handle_starttag(self, tag, attrs):
        if tag != "meta":
            return
        values = dict(attrs)
        if values.get("name") == "md-viewer-review-token":
            self.token = values.get("content", "")


def viewer_origin(value):
    """Validate and normalize one loopback HTTP viewer origin."""
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme != "http" or not parsed.hostname or parsed.username or parsed.password:
        raise ValueError("--url must be a loopback HTTP viewer URL")
    try:
        loopback = ipaddress.ip_address(parsed.hostname).is_loopback
    except ValueError:
        loopback = parsed.hostname == "localhost"
    if not loopback:
        raise ValueError("--url host must be loopback")
    if parsed.query or parsed.fragment:
        raise ValueError("--url must not contain a query or fragment")
    return urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, "", "", ""))


def request_json(origin, path, *, method="GET", body=None, revision="", token=""):
    """Send one API request and decode its JSON response."""
    data = None if body is None else json.dumps(body).encode("utf-8")
    headers = {"Accept": "application/json"}
    if data is not None:
        headers.update({
            "Content-Type": "application/json",
            "Origin": origin,
            "If-Match": json.dumps(revision),
            "X-MD-Viewer-Token": token,
        })
    request = urllib.request.Request(origin + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        message = error.read().decode("utf-8", errors="replace").strip()
        if error.code == 409:
            raise RuntimeError(f"annotations changed concurrently; reload the queue ({message})") from error
        raise RuntimeError(f"annotation API returned {error.code}: {message}") from error


def review_token(origin):
    """Read the per-process review token from the local viewer page."""
    request = urllib.request.Request(origin + "/", headers={"Accept": "text/html"})
    with urllib.request.urlopen(request, timeout=10) as response:
        parser = ReviewTokenParser()
        parser.feed(response.read().decode("utf-8"))
    if not parser.token:
        raise RuntimeError("viewer is not running in review mode")
    return parser.token


def mutation_body(arguments):
    """Construct only fields accepted by the selected mutation endpoint."""
    body = {"document": arguments.document, "author": arguments.author}
    if arguments.command == "reply":
        body["message"] = arguments.message
        return body
    body.update({"status": arguments.status, "actorRole": arguments.role})
    for name in ("message", "summary", "commit"):
        value = getattr(arguments, name)
        if value:
            body[name] = value
    return body


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", required=True, help="running loopback md-viewer URL")
    commands = parser.add_subparsers(dest="command", required=True)

    queue = commands.add_parser("queue", help="list annotations across documents")
    queue.add_argument("--status", default="open,needs_changes")

    def add_mutation_arguments(command):
        command.add_argument("--document", required=True)
        command.add_argument("--revision", required=True)
        command.add_argument("--id", required=True)
        command.add_argument("--author", required=True)

    reply = commands.add_parser("reply", help="append ordinary discussion")
    add_mutation_arguments(reply)
    reply.add_argument("--message", required=True)

    resolve = commands.add_parser("resolve", help="apply a lifecycle transition")
    add_mutation_arguments(resolve)
    resolve.add_argument("--status", required=True)
    resolve.add_argument("--role", required=True, choices=("agent", "reviewer"))
    resolve.add_argument("--message", default="")
    resolve.add_argument("--summary", default="")
    resolve.add_argument("--commit", default="")
    return parser.parse_args()


def main():
    arguments = parse_args()
    origin = viewer_origin(arguments.url)
    if arguments.command == "queue":
        query = urllib.parse.urlencode({"status": arguments.status})
        result = request_json(origin, "/api/annotations?" + query)
    else:
        token = review_token(origin)
        identifier = urllib.parse.quote(arguments.id, safe="")
        suffix = "/replies" if arguments.command == "reply" else ""
        method = "POST" if arguments.command == "reply" else "PATCH"
        result = request_json(
            origin,
            f"/api/annotations/{identifier}{suffix}",
            method=method,
            body=mutation_body(arguments),
            revision=arguments.revision,
            token=token,
        )
    json.dump(result, sys.stdout, indent=2)
    sys.stdout.write("\n")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, ValueError) as error:
        print(f"md-viewer annotation client: {error}", file=sys.stderr)
        raise SystemExit(1)
