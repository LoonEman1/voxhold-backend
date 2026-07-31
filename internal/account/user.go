package account

type User struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    int64
}
