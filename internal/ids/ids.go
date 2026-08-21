package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"
)

const timestampLayout = "2006-01-02T15:04:05.000Z"

func NewWorkflow() (string, error) { return newPrefixed(rand.Reader, "wf-", 12) }
func NewWorkUnit() (string, error) { return newPrefixed(rand.Reader, "wu-", 12) }
func NewClaim() (string, error)    { return newHex(rand.Reader, 16) }
func NewSecret() (string, error)   { return newHex(rand.Reader, 16) }

func FormatTime(value time.Time) string { return value.UTC().Format(timestampLayout) }

func newPrefixed(source io.Reader, prefix string, bytes int) (string, error) {
	seed := make([]byte, 32)
	if _, err := io.ReadFull(source, seed); err != nil {
		return "", err
	}
	digest := sha256.Sum256(seed)
	return prefix + hex.EncodeToString(digest[:bytes]), nil
}

func newHex(source io.Reader, bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := io.ReadFull(source, value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
