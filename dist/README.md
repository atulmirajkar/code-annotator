# Prebuilt binaries

This directory contains architecture-specific `md-viewer` binaries built from
the repository source.

```text
dist/
├── darwin-amd64/md-viewer
├── darwin-arm64/md-viewer
├── linux-amd64/md-viewer
├── linux-arm64/md-viewer
├── windows-amd64/md-viewer.exe
└── windows-arm64/md-viewer.exe
```

Choose the directory matching the operating system and CPU architecture. On
macOS and Linux, the executable bit is stored in Git. If it is lost while
copying or downloading a file, restore it with `chmod +x md-viewer`.

## Rebuild

Run these commands from the repository root:

```sh
GOOS=darwin GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist/darwin-amd64/md-viewer ./cmd/md-viewer
GOOS=darwin GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist/darwin-arm64/md-viewer ./cmd/md-viewer
GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist/linux-amd64/md-viewer ./cmd/md-viewer
GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist/linux-arm64/md-viewer ./cmd/md-viewer
GOOS=windows GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist/windows-amd64/md-viewer.exe ./cmd/md-viewer
GOOS=windows GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o dist/windows-arm64/md-viewer.exe ./cmd/md-viewer
```

The binaries embed the viewer template and CSS and require no runtime assets.
They still need access to the Markdown directory supplied on the command line.
Use [`SHA256SUMS`](SHA256SUMS) to verify the artifacts after copying them.
