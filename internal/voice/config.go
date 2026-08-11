package voice

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

const DefaultUDPPort = 50000

var (
	ErrUDPPortInvalid  = errors.New("WebRTC UDP port is invalid")
	ErrPublicIPInvalid = errors.New(
		"WebRTC public IP is invalid",
	)
)

type Config struct {
	UDPPort       int
	PublicIP      string
	ICEServerURLs []string
	ICEUsername   string
	ICECredential string
}

func NewConfig(
	rawUDPPort string,
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

	publicIP = strings.TrimSpace(publicIP)
	if publicIP != "" && net.ParseIP(publicIP) == nil {
		return Config{}, ErrPublicIPInvalid
	}

	config := Config{
		UDPPort:       udpPort,
		PublicIP:      publicIP,
		ICEServerURLs: splitNonEmpty(rawICEServerURLs),
		ICEUsername:   strings.TrimSpace(iceUsername),
		ICECredential: strings.TrimSpace(iceCredential),
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
