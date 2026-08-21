// Package render converts Markdown documents into safe HTML fragments.
package render

import (
	"bytes"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const blockedDestination = "#invalid-local-path"

// Renderer converts GitHub Flavored Markdown to HTML. It retains goldmark's
// safe defaults, including omission of raw HTML and dangerous URLs.
type Renderer struct {
	markdown       goldmark.Markdown
	reviewMarkdown goldmark.Markdown
}

// New creates a Renderer configured with GitHub Flavored Markdown extensions.
func New() *Renderer {
	return &Renderer{
		markdown: goldmark.New(goldmark.WithExtensions(extension.GFM)),
		reviewMarkdown: goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithRendererOptions(renderer.WithNodeRenderers(
				util.Prioritized(newSourceTextRenderer(), 500),
			)),
		),
	}
}

// sourceTextRenderer adds Markdown byte offsets to plain text segments. The
// browser intentionally accepts selections within one segment only, avoiding
// ambiguous mappings across Markdown formatting delimiters.
type sourceTextRenderer struct {
	writer goldmarkhtml.Writer
}

func newSourceTextRenderer() *sourceTextRenderer {
	return &sourceTextRenderer{writer: goldmarkhtml.NewWriter()}
}

// RegisterFuncs replaces goldmark's text renderer while leaving every other
// node kind on the safe default HTML renderer.
func (r *sourceTextRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(ast.KindText, r.renderText)
	register.Register(ast.KindCodeSpan, r.renderCodeSpan)
	register.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
}

func (r *sourceTextRenderer) renderText(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	textNode := node.(*ast.Text)
	value := textNode.Segment.Value(source)
	eligible := !bytes.ContainsAny(value, "\\&\r\n")
	endByte := textNode.Segment.Stop
	includeSoftBreak := eligible && textNode.SoftLineBreak() && endByte < len(source) && source[endByte] == '\n'
	if includeSoftBreak {
		endByte++
	}
	if eligible {
		_, _ = writer.WriteString(`<span class="source-text" data-source-start="`)
		_, _ = writer.WriteString(strconv.Itoa(textNode.Segment.Start))
		_, _ = writer.WriteString(`" data-source-end="`)
		_, _ = writer.WriteString(strconv.Itoa(endByte))
		_, _ = writer.WriteString(`">`)
	}
	if textNode.IsRaw() {
		r.writer.RawWrite(writer, value)
	} else {
		r.writer.Write(writer, value)
	}
	if includeSoftBreak {
		_ = writer.WriteByte('\n')
	}
	if eligible {
		_, _ = writer.WriteString(`</span>`)
	}
	if textNode.HardLineBreak() {
		_, _ = writer.WriteString("<br>\n")
	} else if textNode.SoftLineBreak() && !includeSoftBreak {
		_ = writer.WriteByte('\n')
	}
	return ast.WalkContinue, nil
}

// renderCodeSpan maps single-line inline-code content while keeping its
// backtick delimiters outside the source-backed span. Multiline code spans are
// left unmapped because goldmark normalizes their newlines to spaces.
func (r *sourceTextRenderer) renderCodeSpan(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = writer.WriteString("</code>")
		return ast.WalkContinue, nil
	}
	_, _ = writer.WriteString("<code")
	if node.Attributes() != nil {
		goldmarkhtml.RenderAttributes(writer, node, goldmarkhtml.CodeAttributeFilter)
	}
	_ = writer.WriteByte('>')

	first := node.FirstChild()
	eligible := first != nil && first == node.LastChild() && !bytes.ContainsAny(first.(*ast.Text).Segment.Value(source), "\r\n")
	if eligible {
		segment := first.(*ast.Text).Segment
		_, _ = writer.WriteString(`<span class="source-text" data-source-start="`)
		_, _ = writer.WriteString(strconv.Itoa(segment.Start))
		_, _ = writer.WriteString(`" data-source-end="`)
		_, _ = writer.WriteString(strconv.Itoa(segment.Stop))
		_, _ = writer.WriteString(`">`)
	}
	for child := first; child != nil; child = child.NextSibling() {
		value := child.(*ast.Text).Segment.Value(source)
		if bytes.HasSuffix(value, []byte("\n")) {
			r.writer.RawWrite(writer, value[:len(value)-1])
			r.writer.RawWrite(writer, []byte(" "))
		} else {
			r.writer.RawWrite(writer, value)
		}
	}
	if eligible {
		_, _ = writer.WriteString("</span>")
	}
	return ast.WalkSkipChildren, nil
}

// renderFencedCodeBlock gives each code line its exact source byte range. The
// opening fence, info string, and closing fence remain outside selectable spans.
func (r *sourceTextRenderer) renderFencedCodeBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		_, _ = writer.WriteString("</code></pre>\n")
		return ast.WalkContinue, nil
	}

	block := node.(*ast.FencedCodeBlock)
	_, _ = writer.WriteString("<pre><code")
	if language := block.Language(source); language != nil {
		_, _ = writer.WriteString(` class="language-`)
		r.writer.Write(writer, language)
		_ = writer.WriteByte('"')
	}
	_ = writer.WriteByte('>')
	for index := 0; index < block.Lines().Len(); index++ {
		line := block.Lines().At(index)
		_, _ = writer.WriteString(`<span class="source-text source-code-text" data-source-start="`)
		_, _ = writer.WriteString(strconv.Itoa(line.Start))
		_, _ = writer.WriteString(`" data-source-end="`)
		_, _ = writer.WriteString(strconv.Itoa(line.Stop))
		_, _ = writer.WriteString(`">`)
		r.writer.RawWrite(writer, line.Value(source))
		_, _ = writer.WriteString("</span>")
	}
	return ast.WalkContinue, nil
}

// Render converts source into an HTML fragment. documentPath is the URL-style
// path of the source document relative to the content root and is used to
// rewrite document-relative links and assets to viewer routes.
func (r *Renderer) Render(source []byte, documentPath string) ([]byte, error) {
	return r.render(r.markdown, source, documentPath)
}

// RenderWithSourcePositions renders a review-mode fragment whose eligible text
// spans carry Markdown byte ranges for exact browser selection mapping.
func (r *Renderer) RenderWithSourcePositions(source []byte, documentPath string) ([]byte, error) {
	return r.render(r.reviewMarkdown, source, documentPath)
}

func (r *Renderer) render(markdown goldmark.Markdown, source []byte, documentPath string) ([]byte, error) {
	document := markdown.Parser().Parse(text.NewReader(source))
	if err := rewriteDestinations(document, documentPath); err != nil {
		return nil, err
	}

	var output bytes.Buffer
	if err := markdown.Renderer().Render(&output, source, document); err != nil {
		return nil, fmt.Errorf("render markdown: %w", err)
	}
	return output.Bytes(), nil
}

func rewriteDestinations(document ast.Node, documentPath string) error {
	documentDirectory := path.Dir(strings.ReplaceAll(documentPath, `\`, "/"))
	if documentDirectory == "." {
		documentDirectory = ""
	}

	return ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		switch typed := node.(type) {
		case *ast.Link:
			typed.Destination = rewriteDestination(typed.Destination, documentDirectory, false)
		case *ast.Image:
			typed.Destination = rewriteDestination(typed.Destination, documentDirectory, true)
		}
		return ast.WalkContinue, nil
	})
}

func rewriteDestination(destination []byte, documentDirectory string, image bool) []byte {
	raw := string(destination)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || strings.HasPrefix(raw, "//") || strings.HasPrefix(parsed.Path, "/") {
		return destination
	}
	if parsed.Path == "" {
		return destination
	}

	target := path.Clean(path.Join(documentDirectory, parsed.Path))
	if target == "." || target == ".." || strings.HasPrefix(target, "../") {
		return []byte(blockedDestination)
	}

	route := "/asset/"
	if !image && strings.EqualFold(path.Ext(target), ".md") {
		route = "/view/"
	}

	rewritten := &url.URL{
		Path:     route + target,
		RawQuery: parsed.RawQuery,
		Fragment: parsed.Fragment,
	}
	return []byte(rewritten.String())
}
