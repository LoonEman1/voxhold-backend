package account

type SessionRevoker interface {
	RevokeSession(token string)
}
