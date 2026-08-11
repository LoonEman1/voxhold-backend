package voice

import (
	"sync"

	"github.com/pion/webrtc/v4"
)

type roomTrack struct {
	ownerConnectionID string
	track             *webrtc.TrackLocalStaticRTP
}

type room struct {
	id int64

	mu           sync.RWMutex
	sessions     map[string]*session
	tracks       map[string]roomTrack
	reservations int
}

func newRoom(id int64) *room {
	return &room{
		id:       id,
		sessions: make(map[string]*session),
		tracks:   make(map[string]roomTrack),
	}
}

func (r *room) addSession(value *session) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.reservations > 0 {
		r.reservations--
	}
	r.sessions[value.connectionID] = value
}

func (r *room) reserve(maxParticipants int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.sessions)+r.reservations >= maxParticipants {
		return false
	}

	r.reservations++
	return true
}

func (r *room) cancelReservation() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.reservations > 0 {
		r.reservations--
	}
}

func (r *room) removeSession(
	connectionID string,
) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.sessions, connectionID)

	trackRemoved := false
	for trackID, track := range r.tracks {
		if track.ownerConnectionID != connectionID {
			continue
		}

		delete(r.tracks, trackID)
		trackRemoved = true
	}

	return trackRemoved
}

func (r *room) addTrack(value roomTrack) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sessions[value.ownerConnectionID]; !exists {
		return false
	}

	for _, track := range r.tracks {
		if track.ownerConnectionID == value.ownerConnectionID {
			return false
		}
	}

	r.tracks[value.track.ID()] = value
	return true
}

func (r *room) removeTrack(
	trackID string,
	ownerConnectionID string,
) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	track, exists := r.tracks[trackID]
	if !exists || track.ownerConnectionID != ownerConnectionID {
		return false
	}

	delete(r.tracks, trackID)
	return true
}

func (r *room) sessionSnapshot() []*session {
	r.mu.RLock()
	defer r.mu.RUnlock()

	values := make([]*session, 0, len(r.sessions))
	for _, value := range r.sessions {
		values = append(values, value)
	}

	return values
}

func (r *room) trackSnapshot() map[string]roomTrack {
	r.mu.RLock()
	defer r.mu.RUnlock()

	values := make(map[string]roomTrack, len(r.tracks))
	for trackID, value := range r.tracks {
		values[trackID] = value
	}

	return values
}

func (r *room) empty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.sessions) == 0 && r.reservations == 0
}

func (r *room) synchronizeSessions() {
	for _, value := range r.sessionSnapshot() {
		if err := value.synchronizeTracks(false); err != nil {
			go value.manager.failSession(
				value,
				"WebRTC renegotiation failed",
			)
		}
	}
}
