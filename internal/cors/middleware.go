package cors

import (
	"net/http"
	"net/url"
	"strings"

	"voxhold-backend/internal/httpapi"
)

const allowedMethods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
const allowedHeaders = "Authorization, Content-Type"

var preflightMethods = map[string]struct{}{
	http.MethodGet:    {},
	http.MethodPost:   {},
	http.MethodPut:    {},
	http.MethodPatch:  {},
	http.MethodDelete: {},
}

var preflightHeaders = map[string]struct{}{
	"authorization": {},
	"content-type":  {},
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		addVary(w.Header(), "Origin")

		rawOrigin := r.Header.Get("Origin")
		if rawOrigin == "" {
			next.ServeHTTP(w, r)
			return
		}

		origin, err := normalizeOrigin(rawOrigin)
		if err != nil || (!sameHost(origin, r.Host) && !m.originAllowed(origin)) {
			httpapi.WriteError(w, http.StatusForbidden, "origin is not allowed")
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", rawOrigin)

		requestedMethod := strings.ToUpper(strings.TrimSpace(
			r.Header.Get("Access-Control-Request-Method"),
		))
		if r.Method != http.MethodOptions || requestedMethod == "" {
			next.ServeHTTP(w, r)
			return
		}

		addVary(w.Header(), "Access-Control-Request-Method")
		addVary(w.Header(), "Access-Control-Request-Headers")

		if _, ok := preflightMethods[requestedMethod]; !ok ||
			!requestedHeadersAllowed(r.Header.Get("Access-Control-Request-Headers")) {
			httpapi.WriteError(w, http.StatusForbidden, "CORS preflight is not allowed")
			return
		}

		w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
		w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
		w.Header().Set("Access-Control-Max-Age", "600")
		w.WriteHeader(http.StatusNoContent)
	})
}

func sameHost(origin string, requestHost string) bool {
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, requestHost)
}

func requestedHeadersAllowed(value string) bool {
	for _, header := range strings.Split(value, ",") {
		header = strings.ToLower(strings.TrimSpace(header))
		if header == "" {
			continue
		}
		if _, ok := preflightHeaders[header]; !ok {
			return false
		}
	}
	return true
}

func addVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, item := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
