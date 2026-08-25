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

// validHighlightResult verifies the renderer's safety contract at the HTML
// boundary: ranges must be ordered, non-empty, inside source, and aligned to
// UTF-8 code-point boundaries. Invalid results are rendered as plain escaped
// source instead of allowing malformed spans to affect adjacent lines.
func validHighlightResult(source []byte, syntaxHighLightResult *highlight.HighlightResult) bool {
	if syntaxHighLightResult == nil || len(syntaxHighLightResult.Ranges) == 0 {
		return syntaxHighLightResult == nil || len(syntaxHighLightResult.Ranges) == 0
	}
	previousEnd := 0
	for _, value := range syntaxHighLightResult.Ranges {
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
