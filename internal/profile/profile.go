package profile

type Profile struct {
	UserID      int64
	Username    string
	About       string
	CountryCode *string
	CreatedAt   int64
	LastSeenAt  *int64
	UpdatedAt   *int64
}
