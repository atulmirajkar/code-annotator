# Vendored HTMX

This directory contains the browser runtime copied from the official
`htmx.org` npm package. It is embedded into the `code-annotator` binary so the
viewer has no runtime CDN or network dependency.

- Package: `htmx.org@2.0.10`
- Source: <https://www.npmjs.com/package/htmx.org/v/2.0.10>
- Runtime source path: `dist/htmx.min.js`
- License source path: `LICENSE`
- License: Zero-Clause BSD (`0BSD`)
- npm tarball SHA-1: `62442b0e2952a885ae2e50a7654b8b20d0981134`

Vendored file SHA-256 digests:

```text
71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de  htmx.min.js
d3d2456f76414f2456104660ebd65aff1c04cd7966b942bdabd63f3cdb316a38  LICENSE
```

To verify the checked-in copies from this directory:

```sh
shasum -a 256 htmx.min.js LICENSE
```

The Go embed test also enforces these digests. The server exposes the runtime
at `/static/htmx.min.js`, but viewer pages intentionally do not load it until
commit gate 7 of the server-rendered review UI migration.
