package highlight

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func BenchmarkHighlightGoTypical(b *testing.B) {
	benchmarkHighlight(b, bytes.Repeat([]byte("func answer() int { return 42 }\n"), 256))
}

func BenchmarkHighlightGoFourMiB(b *testing.B) {
	line := []byte("func answer() int { return 42 }\n")
	source := bytes.Repeat(line, MaxSourceBytes/len(line))
	source = append(source, bytes.Repeat([]byte{' '}, MaxSourceBytes-len(source))...)
	runtime := NewRuntime()
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	for range b.N {
		_, err := runtime.Highlight(context.Background(), ".go", source)
		if !errors.Is(err, ErrSourceTooLarge) {
			b.Fatalf("highlight outer-limit input: got %v, want size stop", err)
		}
	}
}

func BenchmarkHighlightGoMaximumAccepted(b *testing.B) {
	line := []byte("func answer() int { return 42 }\n")
	source := bytes.Repeat(line, MaxHighlightBytes/len(line))
	source = append(source, bytes.Repeat([]byte{' '}, MaxHighlightBytes-len(source))...)
	benchmarkHighlight(b, source)
}

func benchmarkHighlight(b *testing.B, source []byte) {
	b.Helper()
	runtime := NewRuntime()
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	for range b.N {
		result, err := runtime.Highlight(context.Background(), ".go", source)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Ranges) == 0 {
			b.Fatal("no highlight ranges")
		}
	}
}
