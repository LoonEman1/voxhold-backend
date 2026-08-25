package stream

import (
	"sync"

	"github.com/pion/webrtc/v4"
)

type roomTrack struct {
	key       roomTrackKey
	track     *relayTrack
	codec     webrtc.RTPCodecCapability
	rendition Rendition
}

type roomTrackKey struct {
	renditionID string
	kind        webrtc.RTPCodecType
}

type room struct {
	id         int64
	renditions map[string]Rendition
	order      []string

	mu           sync.RWMutex
	publisher    *session
	viewers      map[string]*session
	tracks       map[roomTrackKey]roomTrack
	reservations int
}

func newRoom(id int64, codec Codec) *room {
	return newRoomWithRenditions(id, []Rendition{legacySDRRendition(codec)})
}

func newRoomWithRenditions(id int64, renditions []Rendition) *room {
	byID := make(map[string]Rendition, len(renditions))
	order := make([]string, 0, len(renditions))
	for _, rendition := range renditions {
		byID[rendition.ID] = rendition
		order = append(order, rendition.ID)
	}
	return &room{
		id:         id,
		renditions: byID,
		order:      order,
		viewers:    make(map[string]*session),
		tracks:     make(map[roomTrackKey]roomTrack),
	}
}

func (r *room) hasRendition(id string) bool {
	_, exists := r.renditions[id]
	return exists
}

func (r *room) rendition(id string) (Rendition, bool) {
	value, exists := r.renditions[id]
	return value, exists
}

func (r *room) defaultSDRRenditionID() string {
	for _, id := range r.order {
		if r.renditions[id].DynamicRange == "sdr" {
			return id
		}
	}
	return ""
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
	if _, exists := r.tracks[value.key]; exists {
		return false
	}
	r.tracks[value.key] = value
	return true
}

func (r *room) removeTrack(
	key roomTrackKey,
	track *relayTrack,
) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	current, exists := r.tracks[key]
	if !exists || current.track != track {
		return false
	}
	delete(r.tracks, key)
	return true
}

func (r *room) trackSnapshot(selectedRenditionID string) map[roomTrackKey]roomTrack {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(
		map[roomTrackKey]roomTrack,
		len(r.tracks),
	)
	for key, track := range r.tracks {
		if key.kind == webrtc.RTPCodecTypeAudio || key.renditionID == selectedRenditionID {
			result[key] = track
		}
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

func (r *room) requestKeyFrame(renditionID string) {
	r.mu.RLock()
	publisher := r.publisher
	r.mu.RUnlock()
	if publisher != nil {
		publisher.requestKeyFrame(renditionID)
	}
}

func (r *room) disconnectViewersForRendition(renditionID, reason string) {
	for _, viewer := range r.viewerSnapshot() {
		if viewer.selectedRenditionID == renditionID {
			viewer.scheduleFail(reason)
		}
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
			viewer.scheduleFail("stream WebRTC renegotiation failed")
		}
	}
}
