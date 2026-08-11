package voice

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const DefaultUDPPort = 50000
const DefaultMaxParticipants = 32
const DefaultMaxAudioBitrateKbps = 128

const (
	minAudioBitrateKbps = 16
	maxAudioBitrateKbps = 128
)

var (
	ErrUDPPortInvalid  = errors.New("WebRTC UDP port is invalid")
	ErrPublicIPInvalid = errors.New(
		"WebRTC public IP is invalid",
	)
	ErrMaxParticipantsInvalid = errors.New(
		"voice room participant limit is invalid",
	)
	ErrMaxAudioBitrateInvalid = errors.New(
		"voice audio bitrate limit is invalid",
	)
)

type Config struct {
	UDPPort             int
	MaxParticipants     int
	MaxAudioBitrateKbps int
	PublicIP            string
	ICEServerURLs       []string
	ICEUsername         string
	ICECredential       string
}

func NewConfig(
	rawUDPPort string,
	rawMaxParticipants string,
	rawMaxAudioBitrateKbps string,
	publicIP string,
	rawICEServerURLs string,
	iceUsername string,
	iceCredential string,
) (Config, error) {
	udpPort := DefaultUDPPort
	rawUDPPort = strings.TrimSpace(rawUDPPort)

	if rawUDPPort != "" {
		parsedPort, err := strconv.Atoi(rawUDPPort)
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return Config{}, ErrUDPPortInvalid
		}

		udpPort = parsedPort
	}

	maxParticipants := DefaultMaxParticipants
	rawMaxParticipants = strings.TrimSpace(rawMaxParticipants)

	if rawMaxParticipants != "" {
		parsedLimit, err := strconv.Atoi(rawMaxParticipants)
		if err != nil || parsedLimit < 2 || parsedLimit > 100 {
			return Config{}, ErrMaxParticipantsInvalid
		}

		maxParticipants = parsedLimit
	}

	maxAudioBitrateKbps := DefaultMaxAudioBitrateKbps
	rawMaxAudioBitrateKbps = strings.TrimSpace(
		rawMaxAudioBitrateKbps,
	)

	if rawMaxAudioBitrateKbps != "" {
		parsedLimit, err := strconv.Atoi(
			rawMaxAudioBitrateKbps,
		)
		if err != nil ||
			parsedLimit < minAudioBitrateKbps ||
			parsedLimit > maxAudioBitrateKbps {

			return Config{}, ErrMaxAudioBitrateInvalid
		}

		maxAudioBitrateKbps = parsedLimit
	}

	publicIP = strings.TrimSpace(publicIP)
	if publicIP != "" && net.ParseIP(publicIP) == nil {
		return Config{}, ErrPublicIPInvalid
	}

	config := Config{
		UDPPort:             udpPort,
		MaxParticipants:     maxParticipants,
		MaxAudioBitrateKbps: maxAudioBitrateKbps,
		PublicIP:            publicIP,
		ICEServerURLs:       splitNonEmpty(rawICEServerURLs),
		ICEUsername:         strings.TrimSpace(iceUsername),
		ICECredential:       strings.TrimSpace(iceCredential),
	}

	if (config.ICEUsername == "") !=
		(config.ICECredential == "") {

		return Config{}, fmt.Errorf(
			"ICE username and credential must be configured together",
		)
	}

	return config, nil
}

func splitNonEmpty(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}

	return values
}
