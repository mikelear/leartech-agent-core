package publish

import (
	"crypto/sha256"
	"encoding/hex"
)

// sha256hex is the lower-hex SHA-256 the artifact manifest uses; finalize verifies
// the uploaded bytes against it.
func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
