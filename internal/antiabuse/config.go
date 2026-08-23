package antiabuse

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	TrustProxyHeaders bool

	HTTPRequestsPerSecond   int
	HTTPBurst               int
	HTTPWritesPerSecond     int
	HTTPWriteBurst          int
	AuthRequestsPerMinute   int
	AuthBurst               int
	InviteRequestsPerMinute int
	InviteBurst             int

	LoginMaxFailures   int
	LoginBlockDuration time.Duration

	WebSocketConnectsPerMinute int
	WebSocketConnectBurst      int
	WebSocketEventsPerSecond   int
	WebSocketEventBurst        int
	WebSocketMaxPerIP          int
	WebSocketMaxPerUser        int

	EntryTTL       time.Duration
	MaxRateEntries int
}

func DefaultConfig() Config {
	return Config{
		HTTPRequestsPerSecond:      25,
		HTTPBurst:                  50,
		HTTPWritesPerSecond:        8,
		HTTPWriteBurst:             16,
		AuthRequestsPerMinute:      10,
		AuthBurst:                  5,
		InviteRequestsPerMinute:    20,
		InviteBurst:                10,
		LoginMaxFailures:           5,
		LoginBlockDuration:         20 * time.Minute,
		WebSocketConnectsPerMinute: 10,
		WebSocketConnectBurst:      5,
		WebSocketEventsPerSecond:   30,
		WebSocketEventBurst:        60,
		WebSocketMaxPerIP:          20,
		WebSocketMaxPerUser:        5,
		EntryTTL:                   30 * time.Minute,
		MaxRateEntries:             20_000,
	}
}

func ConfigFromEnv() (Config, error) {
	config := DefaultConfig()

	if raw := os.Getenv("TRUST_PROXY_HEADERS"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf(
				"parse TRUST_PROXY_HEADERS: %w",
				err,
			)
		}
		config.TrustProxyHeaders = value
	}

	values := []struct {
		name        string
		destination *int
	}{
		{"HTTP_RATE_LIMIT_RPS", &config.HTTPRequestsPerSecond},
		{"HTTP_RATE_LIMIT_BURST", &config.HTTPBurst},
		{"HTTP_WRITE_RATE_LIMIT_RPS", &config.HTTPWritesPerSecond},
		{"HTTP_WRITE_RATE_LIMIT_BURST", &config.HTTPWriteBurst},
		{"AUTH_RATE_LIMIT_PER_MINUTE", &config.AuthRequestsPerMinute},
		{"AUTH_RATE_LIMIT_BURST", &config.AuthBurst},
		{"INVITE_RATE_LIMIT_PER_MINUTE", &config.InviteRequestsPerMinute},
		{"INVITE_RATE_LIMIT_BURST", &config.InviteBurst},
		{"LOGIN_MAX_FAILURES", &config.LoginMaxFailures},
		{"LOGIN_BLOCK_MINUTES", nil},
		{"WS_CONNECT_RATE_PER_MINUTE", &config.WebSocketConnectsPerMinute},
		{"WS_CONNECT_RATE_BURST", &config.WebSocketConnectBurst},
		{"WS_EVENT_RATE_PER_SECOND", &config.WebSocketEventsPerSecond},
		{"WS_EVENT_RATE_BURST", &config.WebSocketEventBurst},
		{"WS_MAX_CONNECTIONS_PER_IP", &config.WebSocketMaxPerIP},
		{"WS_MAX_CONNECTIONS_PER_USER", &config.WebSocketMaxPerUser},
		{"RATE_LIMIT_MAX_ENTRIES", &config.MaxRateEntries},
	}

	for _, value := range values {
		raw := os.Getenv(value.name)
		if raw == "" {
			continue
		}

		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf(
				"%s must be a positive integer",
				value.name,
			)
		}

		if value.name == "LOGIN_BLOCK_MINUTES" {
			config.LoginBlockDuration =
				time.Duration(parsed) * time.Minute
			continue
		}

		*value.destination = parsed
	}

	return config, nil
}
