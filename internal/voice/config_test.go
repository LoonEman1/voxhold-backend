package voice

import (
	"errors"
	"testing"
)

func TestNewConfig(t *testing.T) {
	config, err := NewConfig(
		"51000",
		"24",
		"96",
		"203.0.113.10",
		"stun:stun.example.com:3478, turn:turn.example.com:3478",
		"voice-user",
		"voice-password",
	)
	if err != nil {
		t.Fatalf("create voice config: %v", err)
	}

	if config.UDPPort != 51000 ||
		config.MaxParticipants != 24 ||
		config.MaxAudioBitrateKbps != 96 ||
		config.PublicIP != "203.0.113.10" ||
		len(config.ICEServerURLs) != 2 {

		t.Fatalf("unexpected voice config: %+v", config)
	}
}

func TestNewConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name            string
		udpPort         string
		maxParticipants string
		maxAudioBitrate string
		publicIP        string
		username        string
		credential      string
		target          error
	}{
		{
			name:    "invalid UDP port",
			udpPort: "70000",
			target:  ErrUDPPortInvalid,
		},
		{
			name:            "invalid participant limit",
			udpPort:         "50000",
			maxParticipants: "1",
			target:          ErrMaxParticipantsInvalid,
		},
		{
			name:            "invalid audio bitrate limit",
			maxAudioBitrate: "129",
			target:          ErrMaxAudioBitrateInvalid,
		},
		{
			name:     "invalid public IP",
			publicIP: "not-an-ip",
			target:   ErrPublicIPInvalid,
		},
		{
			name:     "incomplete TURN credentials",
			username: "voice-user",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewConfig(
				test.udpPort,
				test.maxParticipants,
				test.maxAudioBitrate,
				test.publicIP,
				"",
				test.username,
				test.credential,
			)
			if err == nil {
				t.Fatal("invalid config unexpectedly accepted")
			}

			if test.target != nil &&
				!errors.Is(err, test.target) {

				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
