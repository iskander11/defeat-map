package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// newID returns a short, sortable-ish unique id: unix-ms timestamp + random suffix.
func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return time.Now().UTC().Format("20060102150405") + hex.EncodeToString(b)
}
