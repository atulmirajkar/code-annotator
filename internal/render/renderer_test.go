package render

import (
	"errors"
	"strings"
	"testing"

	"atulm/code-annotator/internal/gitdiff"
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
		{name: "escapes source", source: []byte("if a < b && b > c {}\n"), contains: []string{`data-line="1"`, `if a &lt; b &amp;&amp; b &gt; c {}`}},
		{name: "review byte ranges with CRLF gaps", source: []byte("café\r\nnext"), review: true, contains: []string{`data-source-start="0" data-source-end="5">café`, `data-source-start="7" data-source-end="11">next`}},
		{name: "review empty line anchor", source: []byte("one\n\ntwo"), review: true, contains: []string{`data-source-start="4" data-source-end="4"></span>`}},
		{name: "empty file", source: nil, contains: []string{`data-line="1"`, `<code></code>`}},
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
				`data-source-start="5" data-source-end="16">new &lt;value&gt;</span>`,
				`removed &amp; gone`,
				`data-source-start="17" data-source-end="29">added &amp; more</span>`,
			},
			excludes: []string{`data-source-start="0" data-source-end="0"`, `>old &lt;value&gt;</span>`},
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
			contains: []string{`data-source-start="0" data-source-end="1">a</span>`, `<span class="diff-line-number" aria-hidden="true">2</span><code><span class="source-text" data-source-start="3" data-source-end="3"></span></code>`},
		},
		{name: "empty source and rows", diff: gitdiff.FileDiff{}, contains: []string{`<div class="diff-pane diff-base-pane"></div>`, `<div class="diff-pane diff-current-pane"></div>`}},
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
				`data-source-start="2" data-source-end="7">Plain</span>`,
				`data-source-start="7" data-source-end="13"> café</span>`,
			},
		},
		{
			name:   "formatted text has independent segments",
			source: "Before **bold** after\n",
			contains: []string{
				`data-source-start="0" data-source-end="7">Before </span>`,
				`data-source-start="9" data-source-end="13">bold</span>`,
				`data-source-start="15" data-source-end="21"> after</span>`,
			},
		},
		{
			name:   "soft line break remains source contiguous",
			source: "first line\nsecond line\n",
			contains: []string{
				`data-source-start="5" data-source-end="11"> line`,
				`data-source-start="11" data-source-end="17">second`,
			},
		},
		{
			name:   "inline code content",
			source: "Before `code` after",
			contains: []string{
				`<code><span class="source-text" data-source-start="8" data-source-end="12">code</span></code>`,
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
				`<pre><code class="language-go"><span class="source-text source-code-text" data-source-start="6" data-source-end="14">first()`,
				`data-source-start="14" data-source-end="23">second()`,
			},
		},
		{
			name:     "escaped text is not mapped",
			source:   `Escaped \* marker`,
			excludes: []string{`data-source-start="0"`},
		},
		{
			name:     "entity text is not mapped",
			source:   `Copyright &copy;`,
			excludes: []string{`data-source-start="10"`},
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
				`<div class="mermaid-diagram" data-source-start="11" data-source-end="28">`,
				`class="source-text source-code-text" data-source-start="11"`,
			},
		},
		{
			name:   "empty Mermaid fence has no annotation region",
			source: "```mermaid\n```\n",
			review: true,
			wantContains: []string{
				`<div class="mermaid-diagram">`,
			},
			wantNotContain: []string{`data-source-start=`},
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
