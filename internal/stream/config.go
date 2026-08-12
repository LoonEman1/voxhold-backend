package stream

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

const (
	DefaultUDPPort             = 50001
	DefaultMaxViewers          = 32
	DefaultMaxP2PViewers       = 8
	DefaultMaxVideoBitrateKbps = 12000
	DefaultMaxAudioBitrateKbps = 320
)

var (
	ErrUDPPortInvalid         = errors.New("stream WebRTC UDP port is invalid")
	ErrPublicIPInvalid        = errors.New("stream WebRTC public IP is invalid")
	ErrMaxViewersInvalid      = errors.New("stream viewer limit is invalid")
	ErrMaxP2PViewersInvalid   = errors.New("P2P stream viewer limit is invalid")
	ErrMaxVideoBitrateInvalid = errors.New("stream video bitrate limit is invalid")
	ErrMaxAudioBitrateInvalid = errors.New("stream audio bitrate limit is invalid")
)

type Config struct {
	UDPPort             int
	MaxViewers          int
	MaxP2PViewers       int
	MaxVideoBitrateKbps int
	MaxAudioBitrateKbps int
	PublicIP            string
	ICEServerURLs       []string
	ICEUsername         string
	ICECredential       string
}

func NewConfig(
	rawUDPPort string,
	rawMaxViewers string,
	rawMaxP2PViewers string,
	rawMaxVideoBitrateKbps string,
	rawMaxAudioBitrateKbps string,
	publicIP string,
	rawICEServerURLs string,
	iceUsername string,
	iceCredential string,
) (Config, error) {
	udpPort, err := parseBounded(rawUDPPort, DefaultUDPPort, 1, 65535, ErrUDPPortInvalid)
	if err != nil {
		return Config{}, err
	}
	maxViewers, err := parseBounded(rawMaxViewers, DefaultMaxViewers, 1, 100, ErrMaxViewersInvalid)
	if err != nil {
		return Config{}, err
	}
	maxP2PViewers, err := parseBounded(rawMaxP2PViewers, DefaultMaxP2PViewers, 1, 16, ErrMaxP2PViewersInvalid)
	if err != nil {
		return Config{}, err
	}
	maxVideo, err := parseBounded(rawMaxVideoBitrateKbps, DefaultMaxVideoBitrateKbps, 500, 20000, ErrMaxVideoBitrateInvalid)
	if err != nil {
		return Config{}, err
	}
	maxAudio, err := parseBounded(rawMaxAudioBitrateKbps, DefaultMaxAudioBitrateKbps, 32, 510, ErrMaxAudioBitrateInvalid)
	if err != nil {
		return Config{}, err
	}

	publicIP = strings.TrimSpace(publicIP)
	if publicIP != "" && net.ParseIP(publicIP) == nil {
		return Config{}, ErrPublicIPInvalid
	}

	config := Config{
		UDPPort:             udpPort,
		MaxViewers:          maxViewers,
		MaxP2PViewers:       maxP2PViewers,
		MaxVideoBitrateKbps: maxVideo,
		MaxAudioBitrateKbps: maxAudio,
		PublicIP:            publicIP,
		ICEServerURLs:       splitNonEmpty(rawICEServerURLs),
		ICEUsername:         strings.TrimSpace(iceUsername),
		ICECredential:       strings.TrimSpace(iceCredential),
	}
	if (config.ICEUsername == "") != (config.ICECredential == "") {
		return Config{}, errors.New("stream ICE username and credential must be configured together")
	}
	return config, nil
}

func parseBounded(raw string, fallback, minimum, maximum int, invalid error) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, invalid
	}
	return value, nil
}

func splitNonEmpty(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}
