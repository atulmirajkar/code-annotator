package annotation

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

const identifierRandomBytes = 10

// NewAnnotationID returns a lexicographically time-sortable annotation ID with
// 80 bits of cryptographic randomness for collision resistance.
func NewAnnotationID(now time.Time) (string, error) {
	return newIdentifier("ann_", now, rand.Reader)
}

// NewThreadID returns a lexicographically time-sortable thread-entry ID with 80
// bits of cryptographic randomness for collision resistance.
func NewThreadID(now time.Time) (string, error) {
	return newIdentifier("msg_", now, rand.Reader)
}

// newIdentifier combines a fixed-width hexadecimal Unix millisecond timestamp
// with random bytes. A fixed-width timestamp preserves lexical time ordering.
func newIdentifier(prefix string, now time.Time, random io.Reader) (string, error) {
	if now.IsZero() || now.UnixMilli() < 0 {
		return "", errors.New("identifier timestamp must be on or after the Unix epoch")
	}
	randomBytes := make([]byte, identifierRandomBytes)
	if _, err := io.ReadFull(random, randomBytes); err != nil {
		return "", fmt.Errorf("generate identifier randomness: %w", err)
	}
	return fmt.Sprintf("%s%016x_%s", prefix, uint64(now.UnixMilli()), hex.EncodeToString(randomBytes)), nil
}
