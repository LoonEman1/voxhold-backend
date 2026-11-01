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
	ErrOwnerCannotBeBanned = errors.New(
		"instance owner cannot be banned",
	)
	ErrCannotBanSelf = errors.New(
		"cannot ban yourself",
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

var ErrTimeoutInvalid = errors.New("timeout must end in the future, within 28 days")

var ErrBanNotFound = errors.New("ban not found")

// Ban is an instance-wide ban shown in the server settings.
type Ban struct {
	UserID    int64  `json:"user_id"`
	Username  string `json:"username"`
	BannedBy  int64  `json:"banned_by"`
	Reason    string `json:"reason"`
	CreatedAt int64  `json:"created_at"`
}

var ErrBanReasonTooLong = errors.New("ban reason must not exceed 500 characters")
