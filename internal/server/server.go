package server

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

type Server struct {
	ID        int64
	Name      string
	CreatedBy int64
	CreatedAt int64
}

type Member struct {
	ServerID int64
	UserID   int64
	Role     Role
	JoinedAt int64
}
