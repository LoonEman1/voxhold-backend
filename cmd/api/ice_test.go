package main

import "testing"

func TestResolveICENamespaces(t *testing.T) {
	for _, prefix := range []string{"WEBRTC_CLIENT_ICE_", "WEBRTC_SERVER_ICE_"} {
		for _, tc := range []struct {
			name    string
			env     map[string]string
			want    iceEnvironment
			invalid bool
		}{
			{"legacy", map[string]string{}, iceEnvironment{"turn:legacy", "legacy-user", "legacy-secret"}, false},
			{"empty", map[string]string{"SERVERS": ""}, iceEnvironment{}, false},
			{"new-urls", map[string]string{"SERVERS": "stun:new"}, iceEnvironment{servers: "stun:new"}, false},
			{"new-turn", map[string]string{"SERVERS": "turn:new", "USERNAME": "new-user", "CREDENTIAL": "new-secret"}, iceEnvironment{"turn:new", "new-user", "new-secret"}, false},
			{"partial", map[string]string{"SERVERS": "turn:new", "USERNAME": "new-user"}, iceEnvironment{}, true},
			{"credentials-only", map[string]string{"USERNAME": "u", "CREDENTIAL": "s"}, iceEnvironment{}, false},
		} {
			t.Run(prefix+tc.name, func(t *testing.T) {
				env := map[string]string{"WEBRTC_ICE_SERVERS": "turn:legacy", "WEBRTC_ICE_USERNAME": "legacy-user", "WEBRTC_ICE_CREDENTIAL": "legacy-secret"}
				for key, value := range tc.env {
					env[prefix+key] = value
				}
				got, err := resolveICE(prefix, func(key string) (string, bool) { v, ok := env[key]; return v, ok })
				if (err != nil) != tc.invalid {
					t.Fatalf("error=%v, invalid=%v", err, tc.invalid)
				}
				if got != tc.want {
					t.Fatal("incorrect ICE namespace selection")
				}
			})
		}
	}
}
