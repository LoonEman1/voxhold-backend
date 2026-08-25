package cors

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareAllowsConfiguredOriginAndPreflight(t *testing.T) {
	manager := &Manager{}
	manager.store([]string{"https://client.example.com"})
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	request := httptest.NewRequest(http.MethodGet, "https://api.example.com/api/v1/instance", nil)
	request.Header.Set("Origin", "https://client.example.com")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTeapot {
		t.Fatalf("request status = %d, want %d", response.Code, http.StatusTeapot)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://client.example.com" {
		t.Fatalf("allow origin = %q", got)
	}

	preflight := httptest.NewRequest(http.MethodOptions, "https://api.example.com/api/v1/auth/login", nil)
	preflight.Header.Set("Origin", "https://client.example.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "content-type, authorization")
	preflightResponse := httptest.NewRecorder()
	handler.ServeHTTP(preflightResponse, preflight)

	if preflightResponse.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", preflightResponse.Code, http.StatusNoContent)
	}
	if got := preflightResponse.Header().Get("Access-Control-Allow-Headers"); got != allowedHeaders {
		t.Fatalf("allow headers = %q, want %q", got, allowedHeaders)
	}
}

func TestMiddlewareRejectsUnknownOrigin(t *testing.T) {
	manager := &Manager{}
	manager.store(nil)
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "https://api.example.com/api/v1/instance", nil)
	request.Header.Set("Origin", "https://evil.example.com")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("disallowed response has allow origin %q", got)
	}
}

func TestMiddlewareAllowsSameHostWithoutConfiguration(t *testing.T) {
	manager := &Manager{}
	manager.store(nil)
	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "https://voxhold.example.com/api/v1/instance", nil)
	request.Header.Set("Origin", "https://voxhold.example.com")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}
