// Package render converts Markdown documents into safe HTML fragments.
package render

import (
	"bytes"
	"errors"
	"fmt"
	"html"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"atulm/code-annotator/internal/gitdiff"
	"atulm/code-annotator/internal/highlight"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const blockedDestination = "#invalid-local-path"

var (
	ErrUnsupportedText = errors.New("source is not valid UTF-8 text")
	ErrInvalidDiff     = errors.New("file diff is inconsistent with current source")
)

// Renderer converts GitHub Flavored Markdown to HTML. It retains goldmark's
// safe defaults, including omission of raw HTML and dangerous URLs.
type Renderer struct {
	markdown       goldmark.Markdown
	reviewMarkdown goldmark.Markdown
}

// SourcePosition binds one semantic HTML element identity to its source byte
// range. Browser code receives these typed values from viewer state instead of
// reading custom attributes from presentation markup.
type SourcePosition struct {
	ElementID string
	StartByte int
	EndByte   int
}

// SourceMap contains selectable text nodes and whole-diagram regions for one
// rendered document mode.
type SourceMap struct {
	Nodes    []SourcePosition
	Diagrams []SourcePosition
}

// New creates a Renderer configured with GitHub Flavored Markdown extensions.
func New() *Renderer {
	return &Renderer{
		markdown: goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithRendererOptions(renderer.WithNodeRenderers(
				util.Prioritized(newFencedCodeRenderer(false), 500),
			)),
		),
		reviewMarkdown: goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithRendererOptions(renderer.WithNodeRenderers(
				util.Prioritized(newSourceTextRenderer(), 500),
				util.Prioritized(newFencedCodeRenderer(true), 500),
			)),
		),
	}
}

// sourceTextRenderer adds semantic IDs to source-backed Markdown text. The
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
}

func (r *sourceTextRenderer) renderText(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	textNode := node.(*ast.Text)
	value := textNode.Segment.Value(source)
	startByte, endByte, eligible, includeSoftBreak := textNodeSourceRange(textNode, source)
	if eligible {
		_, _ = fmt.Fprintf(writer, `<span id="%s" class="source-text">`, sourceElementID(startByte, endByte))
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
		_, _ = fmt.Fprintf(writer, `<span id="%s" class="source-text">`, sourceElementID(segment.Start, segment.Stop))
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

// fencedCodeRenderer recognizes Mermaid fences and optionally gives ordinary
// code lines semantic IDs that viewer state maps to exact byte ranges.
type fencedCodeRenderer struct {
	writer          goldmarkhtml.Writer
	sourcePositions bool
}

func newFencedCodeRenderer(sourcePositions bool) *fencedCodeRenderer {
	return &fencedCodeRenderer{
		writer:          goldmarkhtml.NewWriter(),
		sourcePositions: sourcePositions,
	}
}

// RegisterFuncs replaces the default fenced-code renderer in both normal and
// review modes so Mermaid recognition is consistent.
func (r *fencedCodeRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(ast.KindFencedCodeBlock, r.renderFencedCodeBlock)
}

func (r *fencedCodeRenderer) renderFencedCodeBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	block := node.(*ast.FencedCodeBlock)
	if !entering {
		if isMermaidBlock(block, source) {
			_, _ = writer.WriteString("</code></pre></details>\n")
			_, _ = writer.WriteString(`<div class="mermaid-output" role="img" aria-label="Rendered Mermaid diagram"></div>`)
			_, _ = writer.WriteString(`<p class="mermaid-error" role="alert" hidden></p></div>` + "\n")
			return ast.WalkContinue, nil
		}
		_, _ = writer.WriteString("</code></pre>\n")
		return ast.WalkContinue, nil
	}

	if isMermaidBlock(block, source) {
		_, _ = writer.WriteString(`<div`)
		if start, end, ok := mermaidSourceRange(block); r.sourcePositions && ok {
			_, _ = fmt.Fprintf(writer, ` id="%s"`, diagramElementID(start, end))
		}
		_, _ = writer.WriteString(` class="mermaid-diagram"><details class="mermaid-source"><summary>Diagram source</summary><pre><code class="language-mermaid">`)
		r.renderLines(writer, source, block)
		return ast.WalkContinue, nil
	}

	_, _ = writer.WriteString("<pre><code")
	if language := block.Language(source); language != nil {
		_, _ = writer.WriteString(` class="language-`)
		r.writer.Write(writer, language)
		_ = writer.WriteByte('"')
	}
	_ = writer.WriteByte('>')
	r.renderLines(writer, source, block)
	return ast.WalkContinue, nil
}

// renderLines escapes fenced content and adds selectable source ranges only
// when the renderer is serving review mode.
func (r *fencedCodeRenderer) renderLines(writer util.BufWriter, source []byte, block *ast.FencedCodeBlock) {
	for index := 0; index < block.Lines().Len(); index++ {
		line := block.Lines().At(index)
		if r.sourcePositions {
			_, _ = fmt.Fprintf(writer, `<span id="%s" class="source-text source-code-text">`, sourceElementID(line.Start, line.Stop))
		}
		r.writer.RawWrite(writer, line.Value(source))
		if r.sourcePositions {
			_, _ = writer.WriteString("</span>")
		}
	}
}

// isMermaidBlock matches the conventional fenced-code language label without
// making Markdown authors depend on a particular capitalization.
func isMermaidBlock(block *ast.FencedCodeBlock, source []byte) bool {
	return strings.EqualFold(string(block.Language(source)), "mermaid")
}

// mermaidSourceRange returns the complete diagram definition while excluding
// the opening fence, language label, and closing fence.
func mermaidSourceRange(block *ast.FencedCodeBlock) (int, int, bool) {
	if block.Lines().Len() == 0 {
		return 0, 0, false
	}
	return block.Lines().At(0).Start, block.Lines().At(block.Lines().Len() - 1).Stop, true
}

func sourceElementID(start, end int) string {
	return fmt.Sprintf("source-%d-%d", start, end)
}

func diagramElementID(start, end int) string {
	return fmt.Sprintf("diagram-%d-%d", start, end)
}

func textNodeSourceRange(textNode *ast.Text, source []byte) (start, end int, eligible, includeSoftBreak bool) {
	value := textNode.Segment.Value(source)
	eligible = !bytes.ContainsAny(value, "\\&\r\n")
	start = textNode.Segment.Start
	end = textNode.Segment.Stop
	includeSoftBreak = eligible && textNode.SoftLineBreak() && end < len(source) && source[end] == '\n'
	if includeSoftBreak {
		end++
	}
	return start, end, eligible, includeSoftBreak
}

// MarkdownSourceMap derives the same semantic identities emitted by the
// review renderer without serializing byte offsets into HTML.
func (r *Renderer) MarkdownSourceMap(source []byte) SourceMap {
	document := r.reviewMarkdown.Parser().Parse(text.NewReader(source))
	result := SourceMap{Nodes: []SourcePosition{}, Diagrams: []SourcePosition{}}
	_ = ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *ast.FencedCodeBlock:
			for index := 0; index < typed.Lines().Len(); index++ {
				line := typed.Lines().At(index)
				result.Nodes = append(result.Nodes, sourcePosition(line.Start, line.Stop))
			}
			if isMermaidBlock(typed, source) {
				if start, end, ok := mermaidSourceRange(typed); ok {
					result.Diagrams = append(result.Diagrams, SourcePosition{ElementID: diagramElementID(start, end), StartByte: start, EndByte: end})
				}
			}
			return ast.WalkSkipChildren, nil
		case *ast.CodeSpan:
			first := typed.FirstChild()
			if first != nil && first == typed.LastChild() && !bytes.ContainsAny(first.(*ast.Text).Segment.Value(source), "\r\n") {
				segment := first.(*ast.Text).Segment
				result.Nodes = append(result.Nodes, sourcePosition(segment.Start, segment.Stop))
			}
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			start, end, eligible, _ := textNodeSourceRange(typed, source)
			if eligible {
				result.Nodes = append(result.Nodes, sourcePosition(start, end))
			}
		}
		return ast.WalkContinue, nil
	})
	return result
}

func sourcePosition(start, end int) SourcePosition {
	return SourcePosition{ElementID: sourceElementID(start, end), StartByte: start, EndByte: end}
}

// Render converts source into an HTML fragment. documentPath is the URL-style
// path of the source document relative to the content root and is used to
// rewrite document-relative links and assets to viewer routes.
func (r *Renderer) Render(source []byte, documentPath string) ([]byte, error) {
	return r.render(r.markdown, source, documentPath)
}

// RenderWithSourcePositions renders a review-mode fragment whose eligible text
// spans have semantic IDs for exact browser selection mapping.
func (r *Renderer) RenderWithSourcePositions(source []byte, documentPath string) ([]byte, error) {
	return r.render(r.reviewMarkdown, source, documentPath)
}

// RenderCode converts UTF-8 source into escaped, line-oriented HTML. Review
// mode adds semantic IDs only to visible content; line terminators remain gaps.
func (r *Renderer) RenderCode(source []byte, review bool) ([]byte, error) {
	return r.RenderCodeWithSyntaxHighLight(source, review, nil)
}

// RenderCodeWithSyntaxHighLight renders optional validated syntax ranges inside the
// existing source-line spans. Invalid or absent ranges fall back to escaped
// plain source without changing source IDs or line-ending gaps.
func (r *Renderer) RenderCodeWithSyntaxHighLight(source []byte, review bool, syntaxHighLightResult *highlight.HighlightResult) ([]byte, error) {
	if !utf8.Valid(source) || bytes.IndexByte(source, 0) >= 0 {
		return nil, ErrUnsupportedText
	}
	if !validHighlightResult(source, syntaxHighLightResult) {
		syntaxHighLightResult = nil
	}
	var output strings.Builder
	output.WriteString(`<div class="source-view"><ol class="source-lines">`)
	start := 0
	line := 1
	for start < len(source) {
		end := bytes.IndexByte(source[start:], '\n')
		if end < 0 {
			end = len(source)
		} else {
			end += start
		}
		contentEnd := end
		if contentEnd > start && source[contentEnd-1] == '\r' {
			contentEnd--
		}
		fmt.Fprintf(&output, `<li id="source-line-%d" class="source-line"><span class="source-line-number" aria-hidden="true">%d</span><code>`, line, line)
		if review {
			fmt.Fprintf(&output, `<span id="%s" class="source-text">`, sourceElementID(start, contentEnd))
		}
		renderCodeLine(&output, source, start, contentEnd, syntaxHighLightResult)
		if review {
			output.WriteString(`</span>`)
		}
		output.WriteString(`</code></li>`)
		line++
		if end == len(source) {
			start = end
		} else {
			start = end + 1
		}
	}
	if len(source) == 0 {
		output.WriteString(`<li id="source-line-1" class="source-line"><span class="source-line-number" aria-hidden="true">1</span><code></code></li>`)
	}
	output.WriteString(`</ol></div>`)
	return []byte(output.String()), nil
}

func renderCodeLine(output *strings.Builder, source []byte, start, end int, syntaxHighLightResult *highlight.HighlightResult) {
	// Render one line from normalized, non-overlapping byte ranges. For
	// example, `const value = 1` becomes
	// `<span class="syntax-keyword">const</span> value = <span class="syntax-number">1</span>`;
	// bytes outside known captures stay escaped plain text.
	if syntaxHighLightResult == nil {
		output.WriteString(html.EscapeString(string(source[start:end])))
		return
	}
	cursor := start
	for _, token := range syntaxHighLightResult.Ranges {
		if token.EndByte <= start {
			continue
		}
		if token.StartByte >= end {
			break
		}
		tokenStart := maxInt(start, token.StartByte)
		tokenEnd := minInt(end, token.EndByte)
		if tokenStart > cursor {
			output.WriteString(html.EscapeString(string(source[cursor:tokenStart])))
		}
		class, known := SyntaxClass(token.Capture)
		if known {
			fmt.Fprintf(output, `<span class="syntax-%s">%s</span>`, class, html.EscapeString(string(source[tokenStart:tokenEnd])))
		} else {
			output.WriteString(html.EscapeString(string(source[tokenStart:tokenEnd])))
		}
		cursor = tokenEnd
	}
	if cursor < end {
		output.WriteString(html.EscapeString(string(source[cursor:end])))
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// CodeSourceMap derives selectable line identities for the plain source view.
func (r *Renderer) CodeSourceMap(source []byte) (SourceMap, error) {
	if !utf8.Valid(source) || bytes.IndexByte(source, 0) >= 0 {
		return SourceMap{}, ErrUnsupportedText
	}
	result := SourceMap{Nodes: []SourcePosition{}, Diagrams: []SourcePosition{}}
	for _, value := range sourceRanges(source) {
		result.Nodes = append(result.Nodes, sourcePosition(value[0], value[1]))
	}
	return result, nil
}

// RenderDiff converts aligned Git rows into a side-by-side view with the base
// on the left and current source on the right. Only current-side text receives
// source offsets because annotations always target the editable current file.
func (r *Renderer) RenderDiff(current []byte, diff gitdiff.FileDiff, review bool) ([]byte, error) {
	return r.RenderDiffWithSyntax(current, diff, review, nil, nil)
}

// RenderDiffWithSyntax renders both diff panes with optional bounded token
// ranges. Current source IDs and byte offsets remain unchanged for review mode.
func (r *Renderer) RenderDiffWithSyntax(current []byte, diff gitdiff.FileDiff, review bool, baseSyntax, currentSyntax *highlight.HighlightResult) ([]byte, error) {
	if !utf8.Valid(current) || bytes.IndexByte(current, 0) >= 0 {
		return nil, ErrUnsupportedText
	}
	if err := validateDiffRows(current, diff.Rows); err != nil {
		return nil, err
	}

	baseRanges := sourceRanges(diff.BaseSource)
	overviewItems := buildDiffOverviewItems(diff.Rows)
	overviewTargetByRow := diffOverviewTargets(overviewItems)
	var basePane, currentPane strings.Builder
	for rowIndex, row := range diff.Rows {
		baseStart, baseEnd := 0, 0
		if row.OldLine > 0 && row.OldLine <= len(baseRanges) {
			baseStart, baseEnd = baseRanges[row.OldLine-1][0], baseRanges[row.OldLine-1][1]
		}
		renderDiffCell(&basePane, "", "base", row.Kind, diffMarker(row.Kind, false), row.OldLine, row.BaseText, baseStart, baseEnd, false, diff.BaseSource, baseSyntax)
		currentText := ""
		if row.NewLine > 0 {
			currentText = string(current[row.CurrentStart:row.CurrentEnd])
		}
		renderDiffCell(&currentPane, overviewTargetByRow[rowIndex], "current", row.Kind, diffMarker(row.Kind, true), row.NewLine, currentText, row.CurrentStart, row.CurrentEnd, review, current, currentSyntax)
	}

	var output strings.Builder
	output.WriteString(`<div class="diff-view"><div class="diff-column-headings" aria-hidden="true"><span>Base</span><span aria-hidden="true"></span><span>Current</span></div><div class="diff-panes"><div class="diff-pane diff-base-pane">`)
	output.WriteString(basePane.String())
	output.WriteString(`</div><div class="diff-divider" role="separator" aria-orientation="vertical" aria-label="Resize base and current panes" aria-valuemin="20" aria-valuemax="80" aria-valuenow="50" tabindex="0"></div><div class="diff-pane diff-current-pane">`)
	output.WriteString(currentPane.String())
	output.WriteString(`</div>`)
	writeDiffOverview(&output, overviewItems)
	output.WriteString(`</div></div>`)
	return []byte(output.String()), nil
}

// diffOverviewHunk is a request-local projection over the complete aligned
// row sequence. It stores no source text or persisted state.
type diffOverviewHunk struct {
	StartRow int
	EndRow   int
	Kind     gitdiff.RowKind
}

type diffOverviewItem struct {
	Hunk        diffOverviewHunk
	TargetID    string
	EndTargetID string
	Label       string
}

func deriveDiffOverviewHunks(rows []gitdiff.Row) []diffOverviewHunk {
	hunks := make([]diffOverviewHunk, 0)
	for rowIndex := 0; rowIndex < len(rows); {
		if rows[rowIndex].Kind == gitdiff.RowUnchanged {
			rowIndex++
			continue
		}
		hunk := diffOverviewHunk{StartRow: rowIndex, Kind: rows[rowIndex].Kind}
		for rowIndex < len(rows) && rows[rowIndex].Kind != gitdiff.RowUnchanged {
			if rows[rowIndex].Kind != hunk.Kind {
				hunk.Kind = gitdiff.RowModified
			}
			rowIndex++
		}
		hunk.EndRow = rowIndex
		hunks = append(hunks, hunk)
	}
	return hunks
}

func buildDiffOverviewItems(rows []gitdiff.Row) []diffOverviewItem {
	hunks := deriveDiffOverviewHunks(rows)
	items := make([]diffOverviewItem, 0, len(hunks))
	for index, hunk := range hunks {
		targetID := fmt.Sprintf("diff-change-%d", index+1)
		endTargetID := targetID
		if hunk.EndRow > hunk.StartRow+1 {
			endTargetID += "-end"
		}
		items = append(items, diffOverviewItem{
			Hunk:        hunk,
			TargetID:    targetID,
			EndTargetID: endTargetID,
			Label:       diffOverviewLabel(hunk, rows, index+1, len(hunks)),
		})
	}
	return items
}

func diffOverviewTargets(items []diffOverviewItem) map[int]string {
	targets := make(map[int]string, len(items)*2)
	for _, item := range items {
		targets[item.Hunk.StartRow] = item.TargetID
		if item.EndTargetID != item.TargetID {
			targets[item.Hunk.EndRow-1] = item.EndTargetID
		}
	}
	return targets
}

func writeDiffOverview(output *strings.Builder, items []diffOverviewItem) {
	if len(items) == 0 {
		return
	}
	output.WriteString(`<nav class="diff-overview" aria-label="Changes in this file"><span class="diff-overview-viewport" aria-hidden="true"></span>`)
	for _, item := range items {
		fmt.Fprintf(output, `<span class="diff-overview-item"><a class="diff-overview-marker diff-overview-%s" href="#%s" aria-label="%s"></a><a class="diff-overview-end" href="#%s" tabindex="-1" aria-hidden="true"></a></span>`, item.Hunk.Kind, item.TargetID, html.EscapeString(item.Label), item.EndTargetID)
	}
	output.WriteString(`</nav>`)
}

func diffOverviewLabel(hunk diffOverviewHunk, rows []gitdiff.Row, ordinal, total int) string {
	if hunk.Kind != gitdiff.RowDeleted {
		for _, row := range rows[hunk.StartRow:hunk.EndRow] {
			if row.NewLine > 0 {
				return fmt.Sprintf("Change %d of %d, %s near current line %d", ordinal, total, hunk.Kind, row.NewLine)
			}
		}
	}
	// Deleted rows have no current line number. Prefer the closest surviving
	// line before the deletion so the label can say "after" that line.
	for rowIndex := hunk.StartRow - 1; rowIndex >= 0; rowIndex-- {
		if rows[rowIndex].NewLine > 0 {
			return fmt.Sprintf("Change %d of %d, deletion after current line %d", ordinal, total, rows[rowIndex].NewLine)
		}
	}
	// A deletion at the beginning has no preceding current line. In that case,
	// use the first surviving line after it and describe the deletion as before.
	for rowIndex := hunk.EndRow; rowIndex < len(rows); rowIndex++ {
		if rows[rowIndex].NewLine > 0 {
			return fmt.Sprintf("Change %d of %d, deletion before current line %d", ordinal, total, rows[rowIndex].NewLine)
		}
	}
	return fmt.Sprintf("Change %d of %d, deletion from current file", ordinal, total)
}

// DiffSourceMap derives selectable current-side identities for a validated
// aligned diff.
func (r *Renderer) DiffSourceMap(current []byte, diff gitdiff.FileDiff) (SourceMap, error) {
	if !utf8.Valid(current) || bytes.IndexByte(current, 0) >= 0 {
		return SourceMap{}, ErrUnsupportedText
	}
	if err := validateDiffRows(current, diff.Rows); err != nil {
		return SourceMap{}, err
	}
	result := SourceMap{Nodes: []SourcePosition{}, Diagrams: []SourcePosition{}}
	for _, row := range diff.Rows {
		if row.NewLine > 0 {
			result.Nodes = append(result.Nodes, sourcePosition(row.CurrentStart, row.CurrentEnd))
		}
	}
	return result, nil
}

// validateDiffRows ensures renderer metadata cannot point outside or at the
// wrong line of current source, even if a caller constructs FileDiff directly.
func validateDiffRows(current []byte, rows []gitdiff.Row) error {
	currentLines := sourceRanges(current)
	oldLine, newLine := 1, 1
	for _, row := range rows {
		hasOld := row.Kind != gitdiff.RowAdded
		hasNew := row.Kind != gitdiff.RowDeleted
		if row.Kind != gitdiff.RowUnchanged && row.Kind != gitdiff.RowAdded && row.Kind != gitdiff.RowModified && row.Kind != gitdiff.RowDeleted {
			return ErrInvalidDiff
		}
		if hasOld {
			if row.OldLine != oldLine {
				return ErrInvalidDiff
			}
			oldLine++
		} else if row.OldLine != 0 || row.BaseText != "" {
			return ErrInvalidDiff
		}
		if !hasNew {
			if row.NewLine != 0 || row.CurrentStart != 0 || row.CurrentEnd != 0 {
				return ErrInvalidDiff
			}
			continue
		}
		if row.NewLine != newLine || newLine > len(currentLines) {
			return ErrInvalidDiff
		}
		rangeValue := currentLines[newLine-1]
		if row.CurrentStart != rangeValue[0] || row.CurrentEnd != rangeValue[1] {
			return ErrInvalidDiff
		}
		newLine++
	}
	if newLine != len(currentLines)+1 {
		return ErrInvalidDiff
	}
	return nil
}

// sourceRanges returns byte ranges for visible line content, excluding CRLF or
// LF terminators. An empty source has no diff rows, unlike the plain code view.
func sourceRanges(source []byte) [][2]int {
	ranges := make([][2]int, 0)
	for start := 0; start < len(source); {
		end := bytes.IndexByte(source[start:], '\n')
		if end < 0 {
			end = len(source)
		} else {
			end += start
		}
		contentEnd := end
		if contentEnd > start && source[contentEnd-1] == '\r' {
			contentEnd--
		}
		ranges = append(ranges, [2]int{start, contentEnd})
		if end == len(source) {
			break
		}
		start = end + 1
	}
	return ranges
}

// renderDiffCell writes one escaped diff cell. A zero line number represents
// the intentionally empty half of an added or deleted row.
func renderDiffCell(output *strings.Builder, elementID, side string, kind gitdiff.RowKind, marker string, line int, content string, start, end int, review bool, source []byte, syntax *highlight.HighlightResult) {
	fmt.Fprintf(output, `<div`)
	if elementID != "" {
		fmt.Fprintf(output, ` id="%s"`, elementID)
	}
	fmt.Fprintf(output, ` class="diff-cell diff-%s diff-%s"><span class="diff-marker" aria-hidden="true">%s</span>`, side, kind, marker)
	if line > 0 {
		fmt.Fprintf(output, `<span class="diff-line-number" aria-hidden="true">%d</span><code>`, line)
		if review && side == "current" {
			fmt.Fprintf(output, `<span id="%s" class="source-text">`, sourceElementID(start, end))
		}
		if syntax != nil && start >= 0 && end >= start {
			renderCodeLine(output, source, start, end, syntax)
		} else {
			output.WriteString(html.EscapeString(content))
		}
		if review && side == "current" {
			output.WriteString(`</span>`)
		}
		output.WriteString(`</code>`)
	} else {
		output.WriteString(`<span class="diff-line-number" aria-hidden="true"></span><code></code>`)
	}
	output.WriteString(`</div>`)
}

// diffMarker returns conventional patch markers for changed cells.
func diffMarker(kind gitdiff.RowKind, current bool) string {
	if current && (kind == gitdiff.RowAdded || kind == gitdiff.RowModified) {
		return "+"
	}
	if !current && (kind == gitdiff.RowDeleted || kind == gitdiff.RowModified) {
		return "-"
	}
	return ""
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
