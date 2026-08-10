package server

import "errors"

var (
	ErrMemberNotFound = errors.New(
		"server member not found",
	)
	ErrManageMembersForbidden = errors.New(
		"not allowed to manage server members",
	)
	ErrRoleInvalid = errors.New(
		"role must be admin or member",
	)
	ErrOwnerRoleImmutable = errors.New(
		"server owner role cannot be changed",
	)
	ErrCannotChangeOwnRole = errors.New(
		"cannot change your own role",
	)
	ErrOwnerCannotBeKicked = errors.New(
		"server owner cannot be kicked",
	)
	ErrCannotKickSelf = errors.New(
		"cannot kick yourself",
	)
)

type UpdateMemberRoleInput struct {
	Role Role
}

func (i UpdateMemberRoleInput) Validate() error {
	if i.Role != RoleAdmin && i.Role != RoleMember {
		return ErrRoleInvalid
	}

	return nil
}
