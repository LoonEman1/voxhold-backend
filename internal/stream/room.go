package stream

import (
	"sync"

	"github.com/pion/webrtc/v4"
)

type roomTrack struct {
	kind  webrtc.RTPCodecType
	track *webrtc.TrackLocalStaticRTP
}

type room struct {
	id int64

	mu           sync.RWMutex
	publisher    *session
	viewers      map[string]*session
	tracks       map[webrtc.RTPCodecType]roomTrack
	reservations int
}

func newRoom(id int64) *room {
	return &room{
		id:      id,
		viewers: make(map[string]*session),
		tracks:  make(map[webrtc.RTPCodecType]roomTrack),
	}
}

func (r *room) reserveViewer(maximum int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.viewers)+r.reservations >= maximum {
		return false
	}
	r.reservations++
	return true
}

func (r *room) cancelViewerReservation() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.reservations > 0 {
		r.reservations--
	}
}

func (r *room) addViewer(value *session) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.reservations > 0 {
		r.reservations--
	}
	r.viewers[value.connectionID] = value
}

func (r *room) removeViewer(connectionID string) {
	r.mu.Lock()
	delete(r.viewers, connectionID)
	r.mu.Unlock()
}

func (r *room) setPublisher(value *session) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.publisher != nil {
		return false
	}
	r.publisher = value
	return true
}

func (r *room) addTrack(value roomTrack) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.publisher == nil {
		return false
	}
	if _, exists := r.tracks[value.kind]; exists {
		return false
	}
	r.tracks[value.kind] = value
	return true
}

func (r *room) removeTrack(
	kind webrtc.RTPCodecType,
	track *webrtc.TrackLocalStaticRTP,
) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, exists := r.tracks[kind]
	if !exists || current.track != track {
		return false
	}
	delete(r.tracks, kind)
	return true
}

func (r *room) trackSnapshot() map[webrtc.RTPCodecType]roomTrack {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(
		map[webrtc.RTPCodecType]roomTrack,
		len(r.tracks),
	)
	for kind, track := range r.tracks {
		result[kind] = track
	}
	return result
}

func (r *room) viewerSnapshot() []*session {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*session, 0, len(r.viewers))
	for _, value := range r.viewers {
		result = append(result, value)
	}
	return result
}

func (r *room) hasViewers() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.viewers) > 0
}

func (r *room) requestKeyFrame() {
	r.mu.RLock()
	publisher := r.publisher
	r.mu.RUnlock()
	if publisher != nil {
		publisher.requestKeyFrame()
	}
}

func (r *room) closeSnapshot() []*session {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make([]*session, 0, len(r.viewers)+1)
	if r.publisher != nil {
		result = append(result, r.publisher)
	}
	for _, value := range r.viewers {
		result = append(result, value)
	}
	r.publisher = nil
	clear(r.viewers)
	clear(r.tracks)
	return result
}

func (r *room) synchronizeViewers() {
	for _, viewer := range r.viewerSnapshot() {
		if err := viewer.synchronizeViewerTracks(false); err != nil {
			go viewer.manager.failSession(
				viewer,
				"stream WebRTC renegotiation failed",
			)
		}
	}
}
