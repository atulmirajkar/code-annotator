package render

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"atulm/code-annotator/internal/gitdiff"
	"atulm/code-annotator/internal/highlight"
)

func TestRenderCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		source   []byte
		review   bool
		contains []string
		wantErr  error
	}{
		{name: "escapes source", source: []byte("if a < b && b > c {}\n"), contains: []string{`id="source-line-1"`, `if a &lt; b &amp;&amp; b &gt; c {}`}},
		{name: "review byte ranges with CRLF gaps", source: []byte("café\r\nnext"), review: true, contains: []string{`id="source-0-5" class="source-text">café`, `id="source-7-11" class="source-text">next`}},
		{name: "review empty line anchor", source: []byte("one\n\ntwo"), review: true, contains: []string{`id="source-4-4" class="source-text"></span>`}},
		{name: "empty file", source: nil, contains: []string{`id="source-line-1"`, `<code></code>`}},
		{name: "invalid UTF-8", source: []byte{0xff}, wantErr: ErrUnsupportedText},
		{name: "NUL byte", source: []byte{'a', 0, 'b'}, wantErr: ErrUnsupportedText},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output, err := New().RenderCode(test.source, test.review)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("RenderCode() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RenderCode() error = %v", err)
			}
			for _, want := range test.contains {
				if !strings.Contains(string(output), want) {
					t.Errorf("RenderCode() output missing %q:\n%s", want, output)
				}
			}
		})
	}
}

func TestRenderCodeWithHighlights(t *testing.T) {
	t.Parallel()
	source := []byte("const value = 1 < 2\n")
	result := &highlight.HighlightResult{Grammar: "go", Ranges: []highlight.Range{
		{StartByte: 0, EndByte: 5, Capture: "keyword"},
		{StartByte: 14, EndByte: 15, Capture: "number"},
		{StartByte: 16, EndByte: 17, Capture: "operator"},
	}}
	output, err := New().RenderCodeWithSyntaxHighLight(source, true, result)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="source-0-19" class="source-text"><span class="syntax-keyword">const</span> value = <span class="syntax-number">1</span> <span class="syntax-operator">&lt;</span> 2`,
	} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("highlighted output missing %q:\n%s", want, output)
		}
	}

	invalid := &highlight.HighlightResult{Ranges: []highlight.Range{{StartByte: 1, EndByte: 1, Capture: "keyword"}}}
	plain, err := New().RenderCodeWithSyntaxHighLight(source, true, invalid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "syntax-") || strings.Contains(string(plain), "<span class=\"syntax") {
		t.Fatalf("invalid highlight result was not plain fallback:\n%s", plain)
	}
}

func TestRenderDiff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		current  []byte
		diff     gitdiff.FileDiff
		review   bool
		contains []string
		excludes []string
		wantErr  error
	}{
		{
			name:    "renders aligned rows with current annotation metadata",
			current: []byte("same\nnew <value>\nadded & more\n"),
			review:  true,
			diff: gitdiff.FileDiff{Rows: []gitdiff.Row{
				{Kind: gitdiff.RowUnchanged, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 4, BaseText: "same"},
				{Kind: gitdiff.RowModified, OldLine: 2, NewLine: 2, CurrentStart: 5, CurrentEnd: 16, BaseText: "old <value>"},
				{Kind: gitdiff.RowDeleted, OldLine: 3, BaseText: "removed & gone"},
				{Kind: gitdiff.RowAdded, NewLine: 3, CurrentStart: 17, CurrentEnd: 29},
			}},
			contains: []string{
				`class="diff-pane diff-base-pane"`,
				`class="diff-pane diff-current-pane"`,
				`class="diff-divider" role="separator" aria-orientation="vertical"`,
				`class="diff-cell diff-base diff-unchanged"`,
				`class="diff-cell diff-current diff-modified"`,
				`class="diff-cell diff-base diff-deleted"`,
				`class="diff-cell diff-current diff-added"`,
				`id="source-5-16" class="source-text">new &lt;value&gt;</span>`,
				`removed &amp; gone`,
				`id="source-17-29" class="source-text">added &amp; more</span>`,
				`<div id="diff-change-1" class="diff-cell diff-current diff-modified">`,
				`<div id="diff-change-1-end" class="diff-cell diff-current diff-added">`,
				`<nav class="diff-overview" aria-label="Changes in this file" hidden>`,
				`class="diff-overview-marker diff-overview-modified" href="#diff-change-1" aria-label="Change 1 of 1, modified near current line 2"`,
				`class="diff-overview-end" href="#diff-change-1-end" tabindex="-1" aria-hidden="true"`,
			},
			excludes: []string{`id="source-0-0"`, `>old &lt;value&gt;</span>`},
		},
		{
			name:    "omits annotation metadata outside review mode",
			current: []byte("new"),
			diff: gitdiff.FileDiff{Rows: []gitdiff.Row{
				{Kind: gitdiff.RowModified, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 3, BaseText: "old"},
			}},
			contains: []string{`<span class="diff-marker" aria-hidden="true">-</span>`, `<span class="diff-marker" aria-hidden="true">+</span>`, `<code>new</code>`},
			excludes: []string{`class="source-text"`},
		},
		{
			name:    "accepts CRLF ranges",
			current: []byte("a\r\n\r\n"),
			review:  true,
			diff: gitdiff.FileDiff{Rows: []gitdiff.Row{
				{Kind: gitdiff.RowUnchanged, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 1, BaseText: "a"},
				{Kind: gitdiff.RowAdded, NewLine: 2, CurrentStart: 3, CurrentEnd: 3},
			}},
			contains: []string{`id="source-0-1" class="source-text">a</span>`, `<span class="diff-line-number" aria-hidden="true">2</span><code><span id="source-3-3" class="source-text"></span></code>`},
		},
		{name: "empty source and rows", diff: gitdiff.FileDiff{}, contains: []string{`<div class="diff-pane diff-base-pane"></div>`, `<div class="diff-pane diff-current-pane"></div>`}, excludes: []string{`class="diff-overview"`}},
		{
			name: "targets a deletion-only file",
			diff: gitdiff.FileDiff{Rows: []gitdiff.Row{
				{Kind: gitdiff.RowDeleted, OldLine: 1, BaseText: "removed"},
			}},
			contains: []string{
				`<div id="diff-change-1" class="diff-cell diff-current diff-deleted">`,
				`href="#diff-change-1" aria-label="Change 1 of 1, deletion from current file"`,
				`class="diff-overview-end" href="#diff-change-1" tabindex="-1" aria-hidden="true"`,
			},
		},
		{
			name:    "renders separate overview hunk kinds and locations",
			current: []byte("first\nadded\nmiddle\nbridge\nchanged\nlast"),
			diff: gitdiff.FileDiff{Rows: []gitdiff.Row{
				{Kind: gitdiff.RowUnchanged, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 5, BaseText: "first"},
				{Kind: gitdiff.RowAdded, NewLine: 2, CurrentStart: 6, CurrentEnd: 11},
				{Kind: gitdiff.RowUnchanged, OldLine: 2, NewLine: 3, CurrentStart: 12, CurrentEnd: 18, BaseText: "middle"},
				{Kind: gitdiff.RowDeleted, OldLine: 3, BaseText: "removed"},
				{Kind: gitdiff.RowUnchanged, OldLine: 4, NewLine: 4, CurrentStart: 19, CurrentEnd: 25, BaseText: "bridge"},
				{Kind: gitdiff.RowModified, OldLine: 5, NewLine: 5, CurrentStart: 26, CurrentEnd: 33, BaseText: "old"},
				{Kind: gitdiff.RowUnchanged, OldLine: 6, NewLine: 6, CurrentStart: 34, CurrentEnd: 38, BaseText: "last"},
			}},
			contains: []string{
				`class="diff-overview-marker diff-overview-added" href="#diff-change-1" aria-label="Change 1 of 3, added near current line 2"`,
				`class="diff-overview-marker diff-overview-deleted" href="#diff-change-2" aria-label="Change 2 of 3, deletion after current line 3"`,
				`class="diff-overview-marker diff-overview-modified" href="#diff-change-3" aria-label="Change 3 of 3, modified near current line 5"`,
			},
		},
		{
			name:    "markdown source never renders through goldmark",
			current: []byte("## Heading"),
			diff: gitdiff.FileDiff{Rows: []gitdiff.Row{
				{Kind: gitdiff.RowModified, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 10, BaseText: "# Heading"},
			}},
			contains: []string{`<code># Heading</code>`, `<code>## Heading</code>`},
			excludes: []string{`<h1>`, `<h2>`},
		},
		{name: "invalid UTF-8", current: []byte{0xff}, wantErr: ErrUnsupportedText},
		{name: "invalid row kind", current: []byte("a"), diff: gitdiff.FileDiff{Rows: []gitdiff.Row{{Kind: "mystery", OldLine: 1, NewLine: 1, CurrentEnd: 1}}}, wantErr: ErrInvalidDiff},
		{name: "invalid current offset", current: []byte("a\nb"), diff: gitdiff.FileDiff{Rows: []gitdiff.Row{{Kind: gitdiff.RowUnchanged, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 2, BaseText: "a"}}}, wantErr: ErrInvalidDiff},
		{name: "missing current row", current: []byte("a\nb"), diff: gitdiff.FileDiff{Rows: []gitdiff.Row{{Kind: gitdiff.RowUnchanged, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 1, BaseText: "a"}}}, wantErr: ErrInvalidDiff},
		{name: "deleted row has current metadata", diff: gitdiff.FileDiff{Rows: []gitdiff.Row{{Kind: gitdiff.RowDeleted, OldLine: 1, NewLine: 1}}}, wantErr: ErrInvalidDiff},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output, err := New().RenderDiff(test.current, test.diff, test.review)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("RenderDiff() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RenderDiff() error = %v", err)
			}
			for _, want := range test.contains {
				if !strings.Contains(string(output), want) {
					t.Errorf("RenderDiff() output missing %q:\n%s", want, output)
				}
			}
			for _, unwanted := range test.excludes {
				if strings.Contains(string(output), unwanted) {
					t.Errorf("RenderDiff() output unexpectedly contains %q:\n%s", unwanted, output)
				}
			}
		})
	}
}

func TestDeriveDiffOverviewHunks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rows []gitdiff.Row
		want []diffOverviewHunk
	}{
		{name: "empty"},
		{name: "unchanged only", rows: []gitdiff.Row{{Kind: gitdiff.RowUnchanged}}},
		{
			name: "separate kinds",
			rows: []gitdiff.Row{
				{Kind: gitdiff.RowUnchanged},
				{Kind: gitdiff.RowAdded},
				{Kind: gitdiff.RowUnchanged},
				{Kind: gitdiff.RowDeleted},
				{Kind: gitdiff.RowUnchanged},
				{Kind: gitdiff.RowModified},
			},
			want: []diffOverviewHunk{
				{StartRow: 1, EndRow: 2, Kind: gitdiff.RowAdded},
				{StartRow: 3, EndRow: 4, Kind: gitdiff.RowDeleted},
				{StartRow: 5, EndRow: 6, Kind: gitdiff.RowModified},
			},
		},
		{
			name: "mixed contiguous rows are modified",
			rows: []gitdiff.Row{{Kind: gitdiff.RowAdded}, {Kind: gitdiff.RowDeleted}, {Kind: gitdiff.RowAdded}},
			want: []diffOverviewHunk{{StartRow: 0, EndRow: 3, Kind: gitdiff.RowModified}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := deriveDiffOverviewHunks(test.rows); !slices.Equal(got, test.want) {
				t.Fatalf("deriveDiffOverviewHunks() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDiffOverviewLabelBeforeFirstCurrentLine(t *testing.T) {
	t.Parallel()
	rows := []gitdiff.Row{
		{Kind: gitdiff.RowDeleted, OldLine: 1},
		{Kind: gitdiff.RowUnchanged, OldLine: 2, NewLine: 1},
	}
	hunk := diffOverviewHunk{StartRow: 0, EndRow: 1, Kind: gitdiff.RowDeleted}
	want := "Change 1 of 1, deletion before current line 1"
	if got := diffOverviewLabel(hunk, rows, 1, 1); got != want {
		t.Fatalf("diffOverviewLabel() = %q, want %q", got, want)
	}
}

func TestBuildDiffOverviewItemsAndTargets(t *testing.T) {
	t.Parallel()
	rows := []gitdiff.Row{
		{Kind: gitdiff.RowAdded, NewLine: 1},
		{Kind: gitdiff.RowDeleted},
		{Kind: gitdiff.RowUnchanged, NewLine: 2},
		{Kind: gitdiff.RowAdded, NewLine: 3},
	}
	want := []diffOverviewItem{
		{
			Hunk:        diffOverviewHunk{StartRow: 0, EndRow: 2, Kind: gitdiff.RowModified},
			TargetID:    "diff-change-1",
			EndTargetID: "diff-change-1-end",
			Label:       "Change 1 of 2, modified near current line 1",
		},
		{
			Hunk:        diffOverviewHunk{StartRow: 3, EndRow: 4, Kind: gitdiff.RowAdded},
			TargetID:    "diff-change-2",
			EndTargetID: "diff-change-2",
			Label:       "Change 2 of 2, added near current line 3",
		},
	}
	items := buildDiffOverviewItems(rows)
	if !slices.Equal(items, want) {
		t.Fatalf("buildDiffOverviewItems() = %#v, want %#v", items, want)
	}
	targets := diffOverviewTargets(items)
	if len(targets) != 3 || targets[0] != "diff-change-1" || targets[1] != "diff-change-1-end" || targets[3] != "diff-change-2" {
		t.Fatalf("diffOverviewTargets() = %#v, want start/end targets", targets)
	}
}

func TestRenderDiffWithSyntaxHighlightsBothPanes(t *testing.T) {
	base := []byte("const before = 1\n")
	current := []byte("const after = 2\n")
	diff := gitdiff.FileDiff{
		BaseSource: base,
		Rows:       []gitdiff.Row{{Kind: gitdiff.RowModified, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: len(current) - 1, BaseText: "const before = 1"}},
	}
	result := &highlight.HighlightResult{Ranges: []highlight.Range{{StartByte: 0, EndByte: 5, Capture: "keyword"}, {StartByte: 14, EndByte: 15, Capture: "number"}}}
	output, err := New().RenderDiffWithSyntax(current, diff, true, result, result)
	if err != nil {
		t.Fatalf("RenderDiffWithSyntax() error = %v", err)
	}
	if strings.Count(string(output), `class="syntax-keyword"`) != 2 || strings.Count(string(output), `class="syntax-number"`) != 2 {
		t.Fatalf("both diff panes were not highlighted:\n%s", output)
	}
	if !strings.Contains(string(output), `id="source-0-15" class="source-text"><span class="syntax-keyword">const</span> after`) {
		t.Fatalf("current source identity or token markup changed:\n%s", output)
	}
}

func TestRenderGitHubFlavoredMarkdown(t *testing.T) {
	t.Parallel()

	source := []byte(`~~removed~~

| Name | Ready |
| --- | --- |
| viewer | yes |

- [x] render

https://example.com
`)

	output, err := New().Render(source, "README.md")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	html := string(output)
	for _, want := range []string{"<del>removed</del>", "<table>", `type="checkbox"`, `<a href="https://example.com">`} {
		if !strings.Contains(html, want) {
			t.Errorf("Render() output missing %q:\n%s", want, html)
		}
	}
}

func TestRenderRewritesLocalDestinations(t *testing.T) {
	t.Parallel()

	source := []byte(`[sibling](other.md?mode=read#part)
[root](../README.MD)
[download](files/report.pdf)
![diagram](images/flow%20chart.png?raw=1#preview)
[section](#details)
[external](https://example.com/file.md)
![external image](//cdn.example.com/image.png)
`)

	output, err := New().Render(source, "guide/intro.md")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	html := string(output)

	for _, want := range []string{
		`href="/view/guide/other.md?mode=read#part"`,
		`href="/view/README.MD"`,
		`href="/asset/guide/files/report.pdf"`,
		`src="/asset/guide/images/flow%20chart.png?raw=1#preview"`,
		`href="#details"`,
		`href="https://example.com/file.md"`,
		`src="//cdn.example.com/image.png"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("Render() output missing %q:\n%s", want, html)
		}
	}
}

func TestRenderBlocksEscapingLocalDestinations(t *testing.T) {
	t.Parallel()

	source := []byte(`[secret](../../secret.md)
![secret](../../../secret.png)
[encoded](%2e%2e/%2e%2e/secret.md)
`)
	output, err := New().Render(source, "guide/intro.md")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	html := string(output)
	if count := strings.Count(html, blockedDestination); count != 3 {
		t.Fatalf("Render() blocked destination count = %d, want 3:\n%s", count, html)
	}
}

func TestRenderOmitsRawHTMLAndDangerousURL(t *testing.T) {
	t.Parallel()

	source := []byte(`<script>alert("xss")</script>

[bad](javascript:alert%281%29)
`)
	output, err := New().Render(source, "README.md")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	html := string(output)
	if strings.Contains(html, "<script>") {
		t.Fatalf("Render() retained raw script HTML:\n%s", html)
	}
	if strings.Contains(strings.ToLower(html), `href="javascript:`) {
		t.Fatalf("Render() retained dangerous URL:\n%s", html)
	}
}

func TestRenderReadsFreshSource(t *testing.T) {
	t.Parallel()

	renderer := New()
	first, err := renderer.Render([]byte("first"), "README.md")
	if err != nil {
		t.Fatalf("first Render() error = %v", err)
	}
	second, err := renderer.Render([]byte("second"), "README.md")
	if err != nil {
		t.Fatalf("second Render() error = %v", err)
	}
	if string(first) == string(second) || !strings.Contains(string(second), "second") {
		t.Fatalf("Render() reused stale output: first %q, second %q", first, second)
	}
}

func TestRenderSourcePositionMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   string
		contains []string
		excludes []string
	}{
		{
			name:   "plain UTF-8 text",
			source: "# Plain café\n",
			contains: []string{
				`id="source-2-7" class="source-text">Plain</span>`,
				`id="source-7-13" class="source-text"> café</span>`,
			},
		},
		{
			name:   "formatted text has independent segments",
			source: "Before **bold** after\n",
			contains: []string{
				`id="source-0-7" class="source-text">Before </span>`,
				`id="source-9-13" class="source-text">bold</span>`,
				`id="source-15-21" class="source-text"> after</span>`,
			},
		},
		{
			name:   "soft line break remains source contiguous",
			source: "first line\nsecond line\n",
			contains: []string{
				`id="source-5-11" class="source-text"> line`,
				`id="source-11-17" class="source-text">second`,
			},
		},
		{
			name:   "inline code content",
			source: "Before `code` after",
			contains: []string{
				`<code><span id="source-8-12" class="source-text">code</span></code>`,
			},
		},
		{
			name:     "multiline inline code is not mapped",
			source:   "`code\nspan`",
			excludes: []string{`<code><span class="source-text"`},
		},
		{
			name:   "fenced code lines",
			source: "```go\nfirst()\nsecond()\n```\n",
			contains: []string{
				`<pre><code class="language-go"><span id="source-6-14" class="source-text source-code-text">first()`,
				`id="source-14-23" class="source-text source-code-text">second()`,
			},
		},
		{
			name:     "escaped text is not mapped",
			source:   `Escaped \* marker`,
			excludes: []string{`id="source-0-`},
		},
		{
			name:     "entity text is not mapped",
			source:   `Copyright &copy;`,
			excludes: []string{`id="source-10-`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			output, err := New().RenderWithSourcePositions([]byte(test.source), "README.md")
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			html := string(output)
			for _, want := range test.contains {
				if !strings.Contains(html, want) {
					t.Errorf("Render() output missing %q:\n%s", want, html)
				}
			}
			for _, unwanted := range test.excludes {
				if strings.Contains(html, unwanted) {
					t.Errorf("Render() output contains %q:\n%s", unwanted, html)
				}
			}
		})
	}
}

func TestSourceMapsMatchRenderedSemanticIDs(t *testing.T) {
	t.Parallel()

	renderer := New()
	markdown := []byte("Before `code`\n\n```mermaid\ngraph TD\n  A-->B\n```\n")
	markdownHTML, err := renderer.RenderWithSourcePositions(markdown, "README.md")
	if err != nil {
		t.Fatalf("RenderWithSourcePositions() error = %v", err)
	}
	assertSourceMapIDs(t, string(markdownHTML), renderer.MarkdownSourceMap(markdown))

	code := []byte("first\n\nthird")
	codeHTML, err := renderer.RenderCode(code, true)
	if err != nil {
		t.Fatalf("RenderCode() error = %v", err)
	}
	codeMap, err := renderer.CodeSourceMap(code)
	if err != nil {
		t.Fatalf("CodeSourceMap() error = %v", err)
	}
	assertSourceMapIDs(t, string(codeHTML), codeMap)

	diff := gitdiff.FileDiff{Rows: []gitdiff.Row{
		{Kind: gitdiff.RowModified, OldLine: 1, NewLine: 1, CurrentStart: 0, CurrentEnd: 5, BaseText: "older"},
		{Kind: gitdiff.RowAdded, NewLine: 2, CurrentStart: 6, CurrentEnd: 6},
	}}
	diffHTML, err := renderer.RenderDiff([]byte("first\n\n"), diff, true)
	if err != nil {
		t.Fatalf("RenderDiff() error = %v", err)
	}
	diffMap, err := renderer.DiffSourceMap([]byte("first\n\n"), diff)
	if err != nil {
		t.Fatalf("DiffSourceMap() error = %v", err)
	}
	assertSourceMapIDs(t, string(diffHTML), diffMap)
}

func assertSourceMapIDs(t *testing.T, html string, sourceMap SourceMap) {
	t.Helper()
	for _, position := range append(sourceMap.Nodes, sourceMap.Diagrams...) {
		if !strings.Contains(html, `id="`+position.ElementID+`"`) {
			t.Errorf("rendered HTML is missing source-map element %q:\n%s", position.ElementID, html)
		}
	}
}

func TestRenderFencedCodeBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		source         string
		review         bool
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:   "Mermaid fence becomes diagram with source fallback",
			source: "```mermaid\nsequenceDiagram\n  A->>B: Hello\n```\n",
			wantContains: []string{
				`<div class="mermaid-diagram">`,
				`<details class="mermaid-source">`,
				`<div class="mermaid-output"`,
				`sequenceDiagram`,
			},
			wantNotContain: []string{`class="source-text`},
		},
		{
			name:   "Mermaid language is case insensitive",
			source: "```MERMAID\ngraph TD\n  A-->B\n```\n",
			wantContains: []string{
				`<div class="mermaid-diagram">`,
			},
		},
		{
			name:   "review Mermaid source retains byte positions",
			source: "```mermaid\ngraph TD\n  A-->B\n```\n",
			review: true,
			wantContains: []string{
				`<div id="diagram-11-28" class="mermaid-diagram">`,
				`id="source-11-20" class="source-text source-code-text"`,
			},
		},
		{
			name:   "empty Mermaid fence has no annotation region",
			source: "```mermaid\n```\n",
			review: true,
			wantContains: []string{
				`<div class="mermaid-diagram">`,
			},
			wantNotContain: []string{`id="source-`},
		},
		{
			name:   "ordinary fence remains code",
			source: "```go\nfmt.Println()\n```\n",
			wantContains: []string{
				`<pre><code class="language-go">fmt.Println()`,
			},
			wantNotContain: []string{`mermaid-diagram`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var (
				output []byte
				err    error
			)
			if test.review {
				output, err = New().RenderWithSourcePositions([]byte(test.source), "README.md")
			} else {
				output, err = New().Render([]byte(test.source), "README.md")
			}
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			html := string(output)
			for _, want := range test.wantContains {
				if !strings.Contains(html, want) {
					t.Errorf("Render() output missing %q:\n%s", want, html)
				}
			}
			for _, unwanted := range test.wantNotContain {
				if strings.Contains(html, unwanted) {
					t.Errorf("Render() output contains %q:\n%s", unwanted, html)
				}
			}
		})
	}
}
