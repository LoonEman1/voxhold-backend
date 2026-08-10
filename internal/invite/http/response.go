package invitehttp

import (
	"voxhold-backend/internal/invite"
)

type inviteResponse struct {
	ID              int64         `json:"id"`
	ServerID        int64         `json:"server_id"`
	ServerName      string        `json:"server_name"`
	InviterUserID   int64         `json:"inviter_user_id"`
	InviterUsername string        `json:"inviter_username"`
	InviteeUserID   int64         `json:"invitee_user_id"`
	Status          invite.Status `json:"status"`
	ExpiresAt       int64         `json:"expires_at"`
	RespondedAt     *int64        `json:"responded_at"`
	CreatedAt       int64         `json:"created_at"`
}

func newInviteResponse(value invite.Invite) inviteResponse {
	return inviteResponse{
		ID:              value.ID,
		ServerID:        value.ServerID,
		ServerName:      value.ServerName,
		InviterUserID:   value.InviterUserID,
		InviterUsername: value.InviterUsername,
		InviteeUserID:   value.InviteeUserID,
		Status:          value.Status,
		ExpiresAt:       value.ExpiresAt,
		RespondedAt:     value.RespondedAt,
		CreatedAt:       value.CreatedAt,
	}
}
