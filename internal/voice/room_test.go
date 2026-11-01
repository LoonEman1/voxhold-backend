package voice

import "testing"

func TestRoomParticipantReservationsRespectLimit(t *testing.T) {
	voiceRoom := newRoom(100)

	for range 2 {
		if !voiceRoom.reserve(2) {
			t.Fatal("room rejected a participant below its limit")
		}
	}
	if voiceRoom.reserve(2) {
		t.Fatal("room accepted a participant above its limit")
	}

	voiceRoom.cancelReservation()
	if !voiceRoom.reserve(2) {
		t.Fatal("released room slot was not reusable")
	}
}
