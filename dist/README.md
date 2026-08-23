# Prebuilt binaries

This directory contains architecture-specific `code-annotator` binaries built from
the repository source.

```text
dist/
├── darwin-amd64/code-annotator
├── darwin-arm64/code-annotator
├── linux-amd64/code-annotator
├── linux-arm64/code-annotator
├── windows-amd64/code-annotator.exe
└── windows-arm64/code-annotator.exe
```

Choose the directory matching the operating system and CPU architecture. On
macOS and Linux, the executable bit is stored in Git. If it is lost while
copying or downloading a file, restore it with `chmod +x code-annotator`.

## Rebuild

Run the distribution script from the repository root:

```sh
npm ci
./scripts/build-dist.sh
```

The script compiles and typechecks the TypeScript/Sass frontend, runs `go test ./...`,
builds all six targets in a temporary staging directory, updates the checked-in
binaries only after every build succeeds, and regenerates `SHA256SUMS`. It
requires Node.js, npm, Go, and `shasum` or `sha256sum`.

The binaries embed the viewer template and CSS and require no runtime assets.
They still need access to the Markdown directory supplied on the command line.
Use [`SHA256SUMS`](SHA256SUMS) to verify the artifacts after copying them:

```sh
grep darwin-arm64/code-annotator SHA256SUMS
shasum -a 256 darwin-arm64/code-annotator
```

The two hashes must match. See the root [`README.md`](../README.md#usage)
for downloading and verifying a single binary directly from GitHub without a
full checkout.

Each binary also includes the live `code-annotator agent` HTTP client commands. The
`code-annotator` agent skill is a separate artifact and is not bundled
inside the executable; follow the [skill installation instructions](../README.md#agent-skill)
when an agent needs the guided annotation workflow.
