// Package secrets decides how tokens and codes handed out by Spindle are kept: only a hash of them is stored.
package secrets

import (
	"crypto/sha256"
	"encoding/hex"
)

func Hash(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}
