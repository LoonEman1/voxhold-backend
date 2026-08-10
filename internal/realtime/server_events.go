package realtime

import "voxhold-backend/internal/server"

var _ server.EventPublisher = (*ServerEventPublisher)(nil)

type ServerEventPublisher struct {
	hub *Hub
}

func NewServerEventPublisher(
	hub *Hub,
) *ServerEventPublisher {
	return &ServerEventPublisher{hub: hub}
}

func (p *ServerEventPublisher) PublishServerMemberJoined(
	serverID int64,
	member server.ServerMember,
) {
	p.publishMemberChanged(
		EventServerMemberJoined,
		serverID,
		member,
	)
}

func (p *ServerEventPublisher) PublishServerMemberRoleUpdated(
	serverID int64,
	member server.ServerMember,
) {
	p.publishMemberChanged(
		EventServerMemberRoleUpdated,
		serverID,
		member,
	)
}

func (p *ServerEventPublisher) publishMemberChanged(
	eventType EventType,
	serverID int64,
	member server.ServerMember,
) {
	p.hub.PublishToServer(
		serverID,
		OutgoingEvent{
			Type: eventType,
			Data: ServerMemberChangedData{
				ServerID: serverID,
				Member: ServerMemberData{
					UserID:      member.UserID,
					Username:    member.Username,
					CreatedAt:   member.CreatedAt,
					Role:        string(member.Role),
					JoinedAt:    member.JoinedAt,
					About:       member.About,
					CountryCode: member.CountryCode,
					LastSeenAt:  member.LastSeenAt,
				},
			},
		},
	)
}
