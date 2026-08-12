package stream

import (
	"errors"
	"testing"
)

func TestNewConfig(t *testing.T) {
	config, err := NewConfig(
		"51001",
		"24",
		"6",
		"10000",
		"256",
		"203.0.113.10",
		"stun:stun.example.com:3478",
		"stream-user",
		"stream-password",
	)
	if err != nil {
		t.Fatalf("create stream config: %v", err)
	}
	if config.UDPPort != 51001 || config.MaxViewers != 24 ||
		config.MaxP2PViewers != 6 ||
		config.MaxVideoBitrateKbps != 10000 ||
		config.MaxAudioBitrateKbps != 256 ||
		config.PublicIP != "203.0.113.10" ||
		len(config.ICEServerURLs) != 1 {

		t.Fatalf("unexpected stream config: %+v", config)
	}
}

func TestNewConfigRejectsInvalidLimits(t *testing.T) {
	tests := []struct {
		name   string
		values [5]string
		target error
	}{
		{name: "port", values: [5]string{"70000"}, target: ErrUDPPortInvalid},
		{name: "viewers", values: [5]string{"", "101"}, target: ErrMaxViewersInvalid},
		{name: "P2P viewers", values: [5]string{"", "", "17"}, target: ErrMaxP2PViewersInvalid},
		{name: "video bitrate", values: [5]string{"", "", "", "20001"}, target: ErrMaxVideoBitrateInvalid},
		{name: "audio bitrate", values: [5]string{"", "", "", "", "511"}, target: ErrMaxAudioBitrateInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewConfig(
				test.values[0], test.values[1], test.values[2],
				test.values[3], test.values[4], "", "", "", "",
			)
			if !errors.Is(err, test.target) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
