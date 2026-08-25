package realtimehttp

import (
	"testing"

	"voxhold-backend/internal/realtime"
	"voxhold-backend/internal/stream"
)

func TestQueueStreamStateErrorReportsRenditionFailures(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		code    realtime.ErrorCode
		message string
	}{
		{
			name:    "invalid metadata",
			err:     realtime.ErrStreamRenditionsInvalid,
			code:    realtime.ErrorInvalidPayload,
			message: "stream renditions are invalid",
		},
		{
			name:    "no fallback",
			err:     realtime.ErrStreamRenditionUnavailable,
			code:    realtime.ErrorInvalidState,
			message: "compatible stream rendition is not available",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := realtime.NewClient(1, "viewer", []int64{10})
			if err := queueStreamStateError(client, "request", test.err); err != nil {
				t.Fatal(err)
			}
			event := <-client.Outgoing()
			data, ok := event.Data.(realtime.ErrorData)
			if !ok || event.Type != realtime.EventError ||
				data.Code != test.code || data.Message != test.message {

				t.Fatalf("unexpected error event: %+v", event)
			}
		})
	}
}

func TestStreamMediaHelpersForwardRenditionContract(t *testing.T) {
	media := &renditionMediaStub{}
	renditions := []stream.Rendition{
		{ID: "sdr", Codec: stream.CodecVP9, Profile: "0", DynamicRange: "sdr"},
		{ID: "hdr", Codec: stream.CodecVP9, Profile: "2", DynamicRange: "hdr10"},
	}
	if err := startStreamMedia(media, "publisher", 1, 10, 20, stream.CodecVP9, true, renditions); err != nil {
		t.Fatal(err)
	}
	if err := watchStreamMedia(media, "viewer", 2, 10, 20, "hdr"); err != nil {
		t.Fatal(err)
	}
	if len(media.renditions) != 2 || media.renditions[1].ID != "hdr" {
		t.Fatalf("renditions were not forwarded: %#v", media.renditions)
	}
	if media.selectedRenditionID != "hdr" {
		t.Fatalf("selected rendition was not forwarded: %q", media.selectedRenditionID)
	}
}

type renditionMediaStub struct {
	renditions          []stream.Rendition
	selectedRenditionID string
}

func (*renditionMediaStub) Start(string, int64, int64, int64, stream.Codec, bool) error {
	return nil
}
func (*renditionMediaStub) Watch(string, int64, int64, int64) error { return nil }
func (*renditionMediaStub) AcceptAnswer(string, string) error       { return nil }
func (*renditionMediaStub) AddICECandidate(string, stream.ICECandidate) error {
	return nil
}
func (*renditionMediaStub) RequestRecovery(string, string) error { return nil }
func (m *renditionMediaStub) StartWithRenditions(
	_ string,
	_ int64,
	_ int64,
	_ int64,
	_ stream.Codec,
	_ bool,
	renditions []stream.Rendition,
) error {
	m.renditions = append([]stream.Rendition(nil), renditions...)
	return nil
}
func (m *renditionMediaStub) WatchRendition(
	_ string,
	_ int64,
	_ int64,
	_ int64,
	selectedRenditionID string,
) error {
	m.selectedRenditionID = selectedRenditionID
	return nil
}
