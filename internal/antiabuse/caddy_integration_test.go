package antiabuse

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Opt-in integration: execute the real deploy configs and real Caddy binary.
// Only listeners/TLS/upstream addresses are replaced in adapted JSON; request
// headers, route selection and proxy behavior remain the deployed handlers.
func TestCaddyTrustedClientIdentity(t *testing.T) {
	binary, deploy := os.Getenv("VOXHOLD_CADDY_BIN"), os.Getenv("VOXHOLD_DEPLOY_DIR")
	if binary == "" || deploy == "" {
		t.Skip("set VOXHOLD_CADDY_BIN and VOXHOLD_DEPLOY_DIR")
	}
	for _, file := range []string{"Caddyfile", "Caddyfile.ip"} {
		t.Run(file, func(t *testing.T) {
			config := DefaultConfig()
			config.TrustProxyHeaders = true
			config.HTTPBurst = 1
			config.HTTPRequestsPerSecond = 1
			config.WebSocketMaxPerIP = 1
			guard := New(config)
			identityConfig := DefaultConfig()
			identityConfig.TrustProxyHeaders = true
			identity := New(identityConfig)
			var releases []func()
			var releaseMu sync.Mutex
			defer func() {
				releaseMu.Lock()
				defer releaseMu.Unlock()
				for _, release := range releases {
					release()
				}
			}()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/limit" {
					guard.ProtectHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
					return
				}
				identity.ProtectHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/v1/ws" {
						release, ok := guard.AcquireWebSocketIP(r)
						if !ok {
							w.WriteHeader(429)
							return
						}
						releaseMu.Lock()
						releases = append(releases, release)
						releaseMu.Unlock()
					}
					_, _ = io.WriteString(w, ClientIP(r))
				})).ServeHTTP(w, r)
			}))
			defer upstream.Close()
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			_ = listener.Close()
			adapt := exec.Command(binary, "adapt", "--adapter", "caddyfile", "--config", filepath.Join(deploy, file))
			adapt.Env = append(os.Environ(), "PUBLIC_HOST=localhost", "CADDY_SITE_ADDRESS=localhost", "EDGE_UPSTREAM=backend:8080")
			out, err := adapt.Output()
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err = json.Unmarshal(out, &document); err != nil {
				t.Fatal(err)
			}
			document["admin"] = map[string]any{"disabled": true}
			apps := document["apps"].(map[string]any)
			delete(apps, "tls")
			servers := apps["http"].(map[string]any)["servers"].(map[string]any)
			for _, value := range servers {
				server := value.(map[string]any)
				server["listen"] = []string{address}
				delete(server, "tls_connection_policies")
				server["automatic_https"] = map[string]any{"disable": true}
			}
			var rewrite func(any)
			rewrite = func(value any) {
				switch v := value.(type) {
				case map[string]any:
					delete(v, "host")
					if _, ok := v["dial"]; ok {
						v["dial"] = strings.TrimPrefix(upstream.URL, "http://")
					}
					for _, child := range v {
						rewrite(child)
					}
				case []any:
					for _, child := range v {
						rewrite(child)
					}
				}
			}
			rewrite(document)
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "caddy.json")
			if err = os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(binary, "run", "--config", path)
			var logs bytes.Buffer
			command.Stdout = &logs
			command.Stderr = &logs
			if err = command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				conn, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
				if err == nil {
					_ = conn.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("Caddy did not start")
				}
				time.Sleep(10 * time.Millisecond)
			}
			request := func(ip, path, spoof string) (int, string) {
				dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(ip)}}
				transport := &http.Transport{DialContext: dialer.DialContext}
				defer transport.CloseIdleConnections()
				client := &http.Client{Transport: transport, Timeout: time.Second}
				r, _ := http.NewRequestWithContext(context.Background(), "GET", "http://"+address+path, nil)
				if spoof != "" {
					r.Header.Set("X-Real-IP", spoof)
					r.Header.Set("X-Forwarded-For", spoof)
				}
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, _ := io.ReadAll(response.Body)
				return response.StatusCode, string(body)
			}
			for _, spoof := range []string{"", "203.0.113.1", "invalid"} {
				if code, body := request("127.0.0.2", "/api/v1/identity", spoof); code != 200 || body != "127.0.0.2" {
					t.Fatalf("identity=%d %q", code, body)
				}
			}
			for _, tc := range []struct {
				ip, spoof string
				want      int
			}{{"127.0.0.3", "", 204}, {"127.0.0.3", "203.0.113.5", 429}, {"127.0.0.4", "", 204}} {
				if code, _ := request(tc.ip, "/api/v1/limit", tc.spoof); code != tc.want {
					t.Fatalf("HTTP limit: %d, want %d", code, tc.want)
				}
			}
			for _, tc := range []struct {
				ip, spoof string
				want      int
			}{{"127.0.0.5", "", 200}, {"127.0.0.5", "203.0.113.7", 429}, {"127.0.0.6", "", 200}} {
				if code, _ := request(tc.ip, "/api/v1/ws", tc.spoof); code != tc.want {
					t.Fatalf("WS cap: %d, want %d", code, tc.want)
				}
			}
		})
	}
}
