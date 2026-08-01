package account

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const sessionTokenSize = 32

func generateSessionToken() (string, []byte, error) {
	randomBytes := make([]byte, sessionTokenSize)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", nil, fmt.Errorf("generate random token: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(randomBytes)

	hash := sha256.Sum256([]byte(token))

	return token, hash[:], nil
}

func hashSessionToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))

	return hash[:]
}
