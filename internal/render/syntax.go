package render

import (
	"strings"

	"atulm/code-annotator/internal/highlight"
)

// SyntaxClass maps a pinned Tree-sitter capture to the fixed CSS vocabulary.
// Capture names are never copied directly into HTML class attributes.
func SyntaxClass(capture string) (string, bool) {
	base := strings.ToLower(strings.TrimSpace(capture))
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch base {
	case "attribute", "comment", "constant", "constructor", "escape", "function", "keyword", "number", "operator", "property", "punctuation", "string", "tag", "type", "variable":
		return base, true
	default:
		return "", false
	}
}

func validHighlightResult(source []byte, result *highlight.HighlightResult) bool {
	if result == nil || len(result.Ranges) == 0 {
		return result == nil || len(result.Ranges) == 0
	}
	previousEnd := 0
	for _, value := range result.Ranges {
		if value.StartByte < previousEnd || value.StartByte < 0 || value.StartByte >= value.EndByte || value.EndByte > len(source) ||
			!utf8Boundary(source, value.StartByte) || !utf8Boundary(source, value.EndByte) {
			return false
		}
		previousEnd = value.EndByte
	}
	return true
}

func utf8Boundary(source []byte, offset int) bool {
	return offset == 0 || offset == len(source) || source[offset]&0xc0 != 0x80
}
