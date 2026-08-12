package invite

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const linkTokenSize = 32

func generateLinkToken() (string, []byte, error) {
	randomBytes := make([]byte, linkTokenSize)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", nil, fmt.Errorf("generate invite link token: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	hash := sha256.Sum256([]byte(token))

	return token, hash[:], nil
}

func hashLinkToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))

	return hash[:]
}
