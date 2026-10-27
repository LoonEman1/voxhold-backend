package main

import (
	"errors"
	"strings"
)

type iceEnvironment struct{ servers, username, credential string }

// Select the whole namespace, including explicitly empty values. Credentials
// from legacy configuration must never be attached to a different set of URLs.
func resolveICE(prefix string, lookup func(string) (string, bool)) (iceEnvironment, error) {
	keys := []string{"SERVERS", "USERNAME", "CREDENTIAL"}
	values := make([]string, len(keys))
	explicit := false
	for i, key := range keys {
		value, set := lookup(prefix + key)
		values[i] = strings.TrimSpace(value)
		explicit = explicit || set
	}
	if !explicit {
		for i, key := range keys {
			values[i], _ = lookup("WEBRTC_ICE_" + key)
			values[i] = strings.TrimSpace(values[i])
		}
	}
	result := iceEnvironment{values[0], values[1], values[2]}
	if (result.username == "") != (result.credential == "") {
		return iceEnvironment{}, errors.New("ICE username and credential must be configured together")
	}
	if len(splitICEURLs(result.servers)) == 0 {
		result = iceEnvironment{}
	}
	return result, nil
}

func splitICEURLs(raw string) []string {
	urls := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(part); value != "" {
			urls = append(urls, value)
		}
	}
	return urls
}
