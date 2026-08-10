package realtime

import "voxhold-backend/internal/invite"

var _ invite.EventPublisher = (*InviteEventPublisher)(nil)

type InviteEventPublisher struct {
	hub *Hub
}

func NewInviteEventPublisher(
	hub *Hub,
) *InviteEventPublisher {
	return &InviteEventPublisher{hub: hub}
}

func (p *InviteEventPublisher) PublishInvitationReceived(
	invitation invite.Invite,
) {
	p.hub.PublishToUser(
		invitation.InviteeUserID,
		OutgoingEvent{
			Type: EventInvitationReceived,
			Data: InvitationReceivedData{
				ID:              invitation.ID,
				ServerID:        invitation.ServerID,
				ServerName:      invitation.ServerName,
				InviterUserID:   invitation.InviterUserID,
				InviterUsername: invitation.InviterUsername,
				InviteeUserID:   invitation.InviteeUserID,
				Status:          string(invitation.Status),
				ExpiresAt:       invitation.ExpiresAt,
				CreatedAt:       invitation.CreatedAt,
			},
		},
	)
}
