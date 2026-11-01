package account

type SessionRevoker interface {
	RevokeSession(token string)
	// RevokeSessionHash closes sockets of a session known only by its stored hash.
	RevokeSessionHash(hash []byte)
}
