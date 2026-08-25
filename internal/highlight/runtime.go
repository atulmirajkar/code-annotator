// Package highlight provides bounded, presentation-neutral syntax ranges.
package highlight

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

const (
	// MaxSourceBytes mirrors the application's outer source-read limit. Inputs
	// above MaxHighlightBytes remain reviewable through plain rendering.
	MaxSourceBytes    = 4 << 20
	MaxHighlightBytes = 128 << 10
	ParseTimeout      = 500 * time.Millisecond
	MaxRanges         = 1 << 17
)

var (
	ErrUnsupportedExtension = errors.New("syntax highlighting is unavailable for this extension")
	ErrUnsupportedSource    = errors.New("syntax highlighting requires valid UTF-8 text without NUL bytes")
	ErrSourceTooLarge       = errors.New("syntax highlighting source exceeds the size limit")
	ErrHighlightStopped     = errors.New("syntax highlighting stopped before completion")
	ErrTooManyRanges        = errors.New("syntax highlighting produced too many ranges")
)

// Range is one Tree-sitter capture over a half-open UTF-8 byte interval.
type Range struct {
	StartByte    int
	EndByte      int
	Capture      string
	PatternIndex int
}

// HighlightResult identifies the grammar used and its validated capture ranges.
type HighlightResult struct {
	Grammar string
	Ranges  []Range
}

// Runtime owns no mutable parser state. Every call creates a bounded
// highlighter while gotreesitter lazily caches decoded immutable grammars.
type Runtime struct{}

func NewRuntime() *Runtime { return &Runtime{} }

var grammarByExtension = map[string]string{
	".md": "markdown", ".go": "go", ".cs": "c_sharp",
	".js": "javascript", ".jsx": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".ts": "typescript", ".tsx": "tsx", ".json": "json", ".csproj": "xml",
	".html": "html", ".css": "css", ".scss": "scss",
}

func SupportedExtensions() []string {
	result := make([]string, 0, len(grammarByExtension))
	for extension := range grammarByExtension {
		result = append(result, extension)
	}
	sort.Strings(result)
	return result
}

// GrammarForExtension selects a grammar only from the normalized catalog
// extension; it never guesses from source contents.
func GrammarForExtension(extension string) (string, bool) {
	grammar, ok := grammarByExtension[strings.ToLower(strings.TrimSpace(extension))]
	return grammar, ok
}

// IsCoreExtension reports whether a default code-catalog extension is eligible
// for File-view highlighting. Markdown remains Goldmark-rendered; Changes-view
// highlighting is added in a later milestone.
func IsCoreExtension(extension string) bool {
	switch strings.ToLower(strings.TrimSpace(extension)) {
	case ".go", ".cs", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".json", ".csproj", ".html", ".css", ".scss":
		return true
	default:
		return false
	}
}

// IsChangesExtension includes Markdown because Changes view may use the
// Markdown grammar while File view continues through Goldmark.
func IsChangesExtension(extension string) bool {
	return IsCoreExtension(extension) || strings.EqualFold(strings.TrimSpace(extension), ".md")
}

// Highlight parses one bounded document and returns data only. The renderer
// remains responsible for capture classes, source escaping, and HTML.
func (r *Runtime) Highlight(ctx context.Context, extension string, source []byte) (HighlightResult, error) {
	if err := ctx.Err(); err != nil {
		return HighlightResult{}, err
	}
	grammarName, ok := GrammarForExtension(extension)
	if !ok {
		return HighlightResult{}, ErrUnsupportedExtension
	}
	if len(source) > MaxHighlightBytes {
		return HighlightResult{}, ErrSourceTooLarge
	}
	if !utf8.Valid(source) || bytes.IndexByte(source, 0) >= 0 {
		return HighlightResult{}, ErrUnsupportedSource
	}

	entry := grammars.DetectLanguageByName(grammarName)
	if entry == nil || entry.Language == nil || strings.TrimSpace(entry.HighlightQuery) == "" {
		return HighlightResult{}, fmt.Errorf("%w: grammar %q is not embedded", ErrUnsupportedExtension, grammarName)
	}
	language := entry.Language()
	if language == nil {
		return HighlightResult{}, fmt.Errorf("%w: load grammar %q", ErrUnsupportedExtension, grammarName)
	}
	timeout, err := highlightTimeout(ctx)
	if err != nil {
		return HighlightResult{}, err
	}
	options := []gotreesitter.HighlighterOption{
		gotreesitter.WithHighlighterTimeoutMicros(uint64(timeout.Microseconds())),
	}
	if entry.TokenSourceFactory != nil {
		options = append(options, gotreesitter.WithTokenSourceFactory(func(value []byte) gotreesitter.TokenSource {
			return entry.TokenSourceFactory(value, language)
		}))
	}
	highlighter, err := gotreesitter.NewHighlighter(language, entry.HighlightQuery, options...)
	if err != nil {
		return HighlightResult{}, fmt.Errorf("compile %s highlight query: %w", grammarName, err)
	}

	ranges, tree, err := highlighter.HighlightIncrementalStrict(source, nil)
	if tree != nil {
		tree.Release()
	}
	if err != nil {
		return HighlightResult{}, fmt.Errorf("%w for %s: %v", ErrHighlightStopped, grammarName, err)
	}
	if err := ctx.Err(); err != nil {
		return HighlightResult{}, err
	}
	if len(ranges) > MaxRanges {
		return HighlightResult{}, ErrTooManyRanges
	}

	result := HighlightResult{Grammar: grammarName, Ranges: make([]Range, 0, len(ranges))}
	previousEnd := uint32(0)
	// Tree-sitter reports byte offsets, but the renderer and annotation mapper
	// rely on ranges that are ordered, non-overlapping, in source bounds, and
	// never split a UTF-8 code point. Recheck those presentation-boundary
	// invariants here before exposing any parser output to callers.
	for _, value := range ranges {
		if value.StartByte >= value.EndByte || value.EndByte > uint32(len(source)) || value.StartByte < previousEnd ||
			!utf8Boundary(source, int(value.StartByte)) || !utf8Boundary(source, int(value.EndByte)) {
			return HighlightResult{}, fmt.Errorf("highlight %s source: invalid range [%d,%d)", grammarName, value.StartByte, value.EndByte)
		}
		result.Ranges = append(result.Ranges, Range{
			StartByte: int(value.StartByte), EndByte: int(value.EndByte),
			Capture: value.Capture, PatternIndex: value.PatternIndex,
		})
		previousEnd = value.EndByte
	}
	return result, nil
}

func highlightTimeout(ctx context.Context) (time.Duration, error) {
	timeout := ParseTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, context.DeadlineExceeded
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout < time.Microsecond {
		timeout = time.Microsecond
	}
	return timeout, nil
}

func utf8Boundary(source []byte, offset int) bool {
	return offset == 0 || offset == len(source) || source[offset]&0xc0 != 0x80
}
