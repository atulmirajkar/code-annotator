# Syntax grammar provenance

Milestone 12 uses `github.com/odvcencio/gotreesitter` v0.51.0, pinned by
`go.mod` and verified by `go.sum`. The runtime and each selected grammar are
MIT-licensed at the recorded revision. The runtime notice is retained in
`LICENSE.gotreesitter`; the final distribution slice must carry this notice and
the upstream grammar notices alongside the release artifacts.

| Grammar | Upstream | Revision |
| --- | --- | --- |
| Go | `tree-sitter/tree-sitter-go` | `2346a3ab1bb3857b48b29d779a1ef9799a248cd7` |
| C# | `tree-sitter/tree-sitter-c-sharp` | `88366631d598ce6595ec655ce1591b315cffb14c` |
| JavaScript/JSX | `tree-sitter/tree-sitter-javascript` | `58404d8cf191d69f2674a8fd507bd5776f46cb11` |
| TypeScript/TSX | `tree-sitter/tree-sitter-typescript` | `75b3874edb2dc714fb1fd77a32013d0f8699989f` |
| JSON | `tree-sitter/tree-sitter-json` | `001c28d7a29832b06b0e831ec77845553c89b56d` |
| HTML | `tree-sitter/tree-sitter-html` | `73a3947324f6efddf9e17c0ea58d454843590cc0` |
| CSS | `tree-sitter/tree-sitter-css` | `dda5cfc5722c429eaba1c910ca32c2c0c5bb1a3f` |
| SCSS | `tree-sitter-grammars/tree-sitter-scss` | `bca847c1410f7dd97e13fbe7838b3c2c203fb473` |
| XML | `tree-sitter-grammars/tree-sitter-xml` | `5000ae8f22d11fbe93939b05c1e37cf21117162d` |
| Markdown | `tree-sitter-grammars/tree-sitter-markdown` | `f969cd3ae3f9fbd4e43205431d0ae286014c05b5` |

These revisions come from the dependency's `grammars/languages.lock`.
Highlight queries are generated into the dependency registry at the same
module version. TypeScript inherits JavaScript captures and TSX inherits
TypeScript captures there.

Release builds use the build tags in `scripts/build-dist.sh` to embed only:

```text
go c_sharp javascript typescript tsx json html css scss xml markdown
```

Update the module with `go get`, compare its lock entries and licenses with
this table, run the default and selective tests and benchmarks in
`docs/build.md`, build all six targets, and update this file with `go.sum`.
