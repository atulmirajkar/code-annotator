package annotation

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestNewIdentifier(t *testing.T) {
	t.Parallel()

	timestamp := time.UnixMilli(1_777_777_777_777).UTC()
	tests := []struct {
		name      string
		prefix    string
		timestamp time.Time
		random    *bytes.Reader
		want      string
		wantErr   string
	}{
		{name: "annotation", prefix: "ann_", timestamp: timestamp, random: bytes.NewReader(bytes.Repeat([]byte{0xab}, identifierRandomBytes)), want: "ann_0000019debd01c71_abababababababababab"},
		{name: "thread", prefix: "msg_", timestamp: timestamp, random: bytes.NewReader(bytes.Repeat([]byte{0xcd}, identifierRandomBytes)), want: "msg_0000019debd01c71_cdcdcdcdcdcdcdcdcdcd"},
		{name: "zero timestamp", prefix: "ann_", random: bytes.NewReader(make([]byte, identifierRandomBytes)), wantErr: "timestamp"},
		{name: "before epoch", prefix: "ann_", timestamp: time.UnixMilli(-1), random: bytes.NewReader(make([]byte, identifierRandomBytes)), wantErr: "timestamp"},
		{name: "short randomness", prefix: "ann_", timestamp: timestamp, random: bytes.NewReader([]byte{1}), wantErr: "randomness"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := newIdentifier(test.prefix, test.timestamp, test.random)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("newIdentifier() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("newIdentifier() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("newIdentifier() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestIdentifierLexicalOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		earlier time.Time
		later   time.Time
	}{
		{name: "adjacent milliseconds", earlier: time.UnixMilli(1), later: time.UnixMilli(2)},
		{name: "different hexadecimal width", earlier: time.UnixMilli(0xff), later: time.UnixMilli(0x100)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			earlier, err := newIdentifier("ann_", test.earlier, bytes.NewReader(make([]byte, identifierRandomBytes)))
			if err != nil {
				t.Fatalf("earlier newIdentifier() error = %v", err)
			}
			later, err := newIdentifier("ann_", test.later, bytes.NewReader(make([]byte, identifierRandomBytes)))
			if err != nil {
				t.Fatalf("later newIdentifier() error = %v", err)
			}
			if strings.Compare(earlier, later) >= 0 {
				t.Fatalf("earlier ID %q does not sort before later ID %q", earlier, later)
			}
		})
	}
}
