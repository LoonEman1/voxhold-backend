package realtime

import (
	"encoding/json"
	"sync"
)

type eventEncoding struct {
	once sync.Once
	data []byte
	err  error
}

// shareEncoding applies only to an immutable broadcast snapshot. Copies of the
// event share the encoded document, never a client-specific filtered snapshot.
func (event OutgoingEvent) shareEncoding() OutgoingEvent {
	if event.encoding == nil {
		event.encoding = &eventEncoding{}
	}
	return event
}

// SharedJSON returns an immutable JSON document for a shared broadcast. Writers
// must not modify the returned bytes. Ordinary replies retain their existing
// wsjson encoder path, avoiding a new allocation for single-recipient events.
func (event OutgoingEvent) SharedJSON() ([]byte, bool, error) {
	cache := event.encoding
	if cache == nil {
		return nil, false, nil
	}
	cache.once.Do(func() {
		cache.data, cache.err = json.Marshal(event)
		if cache.err == nil {
			cache.data = append(cache.data, '\n')
		}
	})
	return cache.data, true, cache.err
}
