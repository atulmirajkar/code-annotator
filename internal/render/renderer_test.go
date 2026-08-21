package render

import (
	"strings"
	"testing"
)

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
