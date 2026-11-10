package realtimehttp

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"voxhold-backend/internal/channel"
	"voxhold-backend/internal/permissions"
	"voxhold-backend/internal/realtime"
	"voxhold-backend/internal/voice"
)

type VoiceModerationStore interface {
	Allowed(ctx context.Context, serverID, actorID, targetID int64, bit int) (bool, error)
	Muted(ctx context.Context, serverID, userID int64) (bool, error)
	SetMuted(ctx context.Context, serverID, actorID, userID int64, muted bool) (bool, error)
}

func (h *Handler) SetVoiceModeration(store VoiceModerationStore) {
	h.voiceModeration = store
}

func (h *Handler) moderateVoice(ctx context.Context, actor *realtime.Client, event realtime.IncomingEvent) error {
	var data realtime.VoiceModerationData
	requestID := event.RequestID
	if err := json.Unmarshal(event.Data, &data); err != nil || data.ServerID <= 0 || data.ChannelID <= 0 ||
		data.UserID <= 0 || data.ConnectionID == "" {
		return queueError(actor, requestID, realtime.ErrorInvalidPayload, "invalid voice moderation payload")
	}
	var bit int
	switch data.Action {
	case "mute":
		if data.Muted == nil {
			return queueError(actor, requestID, realtime.ErrorInvalidPayload, "muted is required")
		}
		bit = permissions.MuteVoice
	case "move":
		if data.DestinationChannelID <= 0 || data.DestinationChannelID == data.ChannelID {
			return queueError(actor, requestID, realtime.ErrorInvalidPayload, "destination must be a different voice channel")
		}
		bit = permissions.MoveVoice
	case "kick":
		bit = permissions.KickVoice
	default:
		return queueError(actor, requestID, realtime.ErrorInvalidPayload, "unknown voice moderation action")
	}
	if h.voiceModeration == nil {
		return queueError(actor, requestID, realtime.ErrorForbidden, "voice moderation is unavailable")
	}

	defer h.hub.LockChannelOperation()()
	defer h.voiceJoins.lock(data.UserID)()
	allowed, err := h.voiceModeration.Allowed(ctx, data.ServerID, actor.UserID(), data.UserID, bit)
	if err != nil {
		return voiceModerationError(actor, requestID, err)
	}
	if !allowed {
		return queueError(actor, requestID, realtime.ErrorForbidden, "voice moderation permission is required")
	}
	if err := h.channelAccess.CheckAccess(ctx, data.ServerID, data.ChannelID, actor.UserID()); err != nil {
		return voiceModerationAccessError(actor, requestID, err)
	}
	target, participant, ok := h.hub.VoiceTarget(data)
	if !ok {
		return queueError(actor, requestID, realtime.ErrorInvalidState, "participant is no longer in this voice channel")
	}

	switch data.Action {
	case "mute":
		muted := *data.Muted
		allowed, err := h.voiceModeration.SetMuted(ctx, data.ServerID, actor.UserID(), data.UserID, muted)
		if err != nil {
			return voiceModerationError(actor, requestID, err)
		}
		if !allowed {
			return queueError(actor, requestID, realtime.ErrorForbidden, "voice moderation permission is required")
		}
		if err := h.voiceMedia.SetState(target.ConnectionID(), participant.SelfMute || muted, participant.SelfDeaf); err != nil &&
			!errors.Is(err, voice.ErrSessionNotFound) {
			log.Printf("apply voice moderation state: %v", err)
		}
		h.hub.SetVoiceServerMute(target, muted)
	case "move":
		if !participant.SupportsMove {
			return queueError(actor, requestID, realtime.ErrorInvalidState, "participant client does not support moving")
		}
		if err := h.channelAccess.CheckAccess(ctx, data.ServerID, data.DestinationChannelID, actor.UserID()); err != nil {
			return voiceModerationAccessError(actor, requestID, err)
		}
		if err := h.channelAccess.CheckVoiceAccess(ctx, data.ServerID, data.DestinationChannelID, target.UserID()); err != nil {
			return voiceModerationAccessError(actor, requestID, err)
		}
		if h.voiceMedia.RoomFull(data.DestinationChannelID) {
			return queueError(actor, requestID, realtime.ErrorInvalidState, "voice channel is full")
		}
		if !h.hub.DisconnectVoiceParticipant(target, participant, "moved", data.DestinationChannelID) {
			return queueError(actor, requestID, realtime.ErrorInvalidState, "participant is no longer in this voice channel")
		}
	case "kick":
		if !h.hub.DisconnectVoiceParticipant(target, participant, "kicked", 0) {
			return queueError(actor, requestID, realtime.ErrorInvalidState, "participant is no longer in this voice channel")
		}
	}
	return queueEvent(actor, realtime.OutgoingEvent{RequestID: requestID, Type: realtime.EventVoiceModerated, Data: data})
}

func voiceModerationAccessError(client *realtime.Client, requestID string, err error) error {
	if errors.Is(err, channel.ErrNotFound) || errors.Is(err, channel.ErrForbidden) || errors.Is(err, channel.ErrVoiceRequired) {
		return queueError(client, requestID, realtime.ErrorForbidden, "voice channel is unavailable")
	}
	return voiceModerationError(client, requestID, err)
}

func voiceModerationError(client *realtime.Client, requestID string, err error) error {
	log.Printf("voice moderation: %v", err)
	return queueError(client, requestID, realtime.ErrorInternal, "failed to apply voice moderation")
}
