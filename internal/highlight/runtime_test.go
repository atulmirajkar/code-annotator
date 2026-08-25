package highlight

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSupportedExtensions(t *testing.T) {
	t.Parallel()
	want := []string{".cjs", ".cs", ".csproj", ".css", ".go", ".html", ".js", ".json", ".jsx", ".md", ".mjs", ".scss", ".ts", ".tsx"}
	if got := SupportedExtensions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedExtensions() = %v, want %v", got, want)
	}
}

func TestGrammarForExtension(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		".go": "go", " .CS ": "c_sharp", ".js": "javascript", ".jsx": "javascript",
		".mjs": "javascript", ".cjs": "javascript", ".ts": "typescript", ".tsx": "tsx",
		".json": "json", ".csproj": "xml", ".html": "html", ".css": "css",
		".scss": "scss", ".md": "markdown",
	}
	for extension, want := range tests {
		got, ok := GrammarForExtension(extension)
		if !ok || got != want {
			t.Errorf("GrammarForExtension(%q) = %q, %t; want %q, true", extension, got, ok, want)
		}
	}
	if _, ok := GrammarForExtension(".py"); ok {
		t.Fatal("GrammarForExtension(.py) unexpectedly succeeded")
	}
}

func TestIsCoreExtension(t *testing.T) {
	t.Parallel()
	for _, extension := range []string{".go", ".cs", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".json", ".csproj", ".html", ".css", ".scss"} {
		if !IsCoreExtension(extension) {
			t.Errorf("IsCoreExtension(%q) = false", extension)
		}
	}
	for _, extension := range []string{".xml", ".md", ".py"} {
		if IsCoreExtension(extension) {
			t.Errorf("IsCoreExtension(%q) = true", extension)
		}
	}
}

func TestRuntimeHighlightsDefaultGrammarSet(t *testing.T) {
	tests := []struct {
		extension string
		grammar   string
		source    string
	}{
		{".go", "go", "package main\n\nconst answer = 42\n"},
		{".cs", "c_sharp", "class App { static void Main() { } }\n"},
		{".js", "javascript", "const answer = () => 42;\n"},
		{".jsx", "javascript", "const App = () => <main>Hello</main>;\n"},
		{".mjs", "javascript", "export const answer = 42;\n"},
		{".cjs", "javascript", "module.exports = { answer: 42 };\n"},
		{".ts", "typescript", "interface User { name: string }\n"},
		{".tsx", "tsx", "const App = () => <main>Hello</main>;\n"},
		{".json", "json", "{\"enabled\": true}\n"},
		{".csproj", "xml", "<Project Sdk=\"Microsoft.NET.Sdk\"></Project>\n"},
		{".html", "html", "<main class=\"app\">Hello</main>\n"},
		{".css", "css", ".app { color: red; }\n"},
		{".scss", "scss", "$color: red;\n.app { color: $color; }\n"},
		{".md", "markdown", "# Heading\n\nParagraph.\n"},
	}
	runtime := NewRuntime()
	for _, test := range tests {
		t.Run(test.extension, func(t *testing.T) {
			result, err := runtime.Highlight(context.Background(), test.extension, []byte(test.source))
			if err != nil {
				t.Fatalf("Highlight() error = %v", err)
			}
			if result.Grammar != test.grammar {
				t.Errorf("Highlight().Grammar = %q, want %q", result.Grammar, test.grammar)
			}
			if len(result.Ranges) == 0 {
				t.Fatal("Highlight().Ranges is empty")
			}
			assertValidRanges(t, []byte(test.source), result.Ranges)
		})
	}
}

func TestRuntimeGoCaptureFixture(t *testing.T) {
	t.Parallel()
	source := []byte("package main\n\nconst answer = 42\n")
	result, err := NewRuntime().Highlight(context.Background(), ".go", source)
	if err != nil {
		t.Fatal(err)
	}
	for text, capture := range map[string]string{"package": "keyword", "const": "keyword", "42": "number"} {
		if !hasCapture(source, result.Ranges, text, capture) {
			t.Errorf("Highlight() missing %q capture %q; ranges = %#v", text, capture, result.Ranges)
		}
	}
}

func TestRuntimeToleratesIncompleteSyntax(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		extension string
		source    string
	}{
		{".go", "package main\nfunc main("},
		{".tsx", "const App = () => <main>"},
		{".json", "{\"enabled\":"},
	} {
		result, err := NewRuntime().Highlight(context.Background(), test.extension, []byte(test.source))
		if err != nil {
			t.Errorf("Highlight(%s incomplete source) error = %v", test.extension, err)
			continue
		}
		if len(result.Ranges) == 0 {
			t.Errorf("Highlight(%s incomplete source) returned no ranges", test.extension)
		}
	}
}

func TestRuntimeRejectsUnsupportedInput(t *testing.T) {
	t.Parallel()
	runtime := NewRuntime()
	tests := []struct {
		name, extension string
		source          []byte
		ctx             context.Context
		want            error
	}{
		{"extension", ".py", []byte("pass"), context.Background(), ErrUnsupportedExtension},
		{"invalid UTF-8", ".go", []byte{0xff}, context.Background(), ErrUnsupportedSource},
		{"NUL", ".go", []byte{'a', 0, 'b'}, context.Background(), ErrUnsupportedSource},
		{"too large", ".go", make([]byte, MaxHighlightBytes+1), context.Background(), ErrSourceTooLarge},
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests = append(tests, struct {
		name, extension string
		source          []byte
		ctx             context.Context
		want            error
	}{"canceled", ".go", []byte("package main"), canceled, context.Canceled})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := runtime.Highlight(test.ctx, test.extension, test.source); !errors.Is(err, test.want) {
				t.Fatalf("Highlight() error = %v, want %v", err, test.want)
			}
		})
	}
}

func assertValidRanges(t *testing.T, source []byte, ranges []Range) {
	t.Helper()
	previousEnd := 0
	for _, value := range ranges {
		if value.StartByte < previousEnd || value.StartByte >= value.EndByte || value.EndByte > len(source) || value.Capture == "" {
			t.Fatalf("invalid range %#v after byte %d for %d source bytes", value, previousEnd, len(source))
		}
		previousEnd = value.EndByte
	}
}

func hasCapture(source []byte, ranges []Range, text, capture string) bool {
	for _, value := range ranges {
		if value.Capture == capture && string(source[value.StartByte:value.EndByte]) == text {
			return true
		}
	}
	return false
}
