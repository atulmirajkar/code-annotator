package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"
)

func TestEmbeddedHTMXAssets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path       string
		wantSHA256 string
	}{
		{path: "vendor/htmx/htmx.min.js", wantSHA256: "71ea67185bfa8c98c39d31717c6fce5d852370fcdfd129db4543774d3145c0de"},
		{path: "vendor/htmx/LICENSE", wantSHA256: "d3d2456f76414f2456104660ebd65aff1c04cd7966b942bdabd63f3cdb316a38"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			contents, err := fs.ReadFile(Files, test.path)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", test.path, err)
			}
			digest := sha256.Sum256(contents)
			if got := hex.EncodeToString(digest[:]); got != test.wantSHA256 {
				t.Fatalf("SHA-256 = %s, want %s", got, test.wantSHA256)
			}
		})
	}
}
