package avatar

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/profile"
	profilesqlite "voxhold-backend/internal/profile/sqlite"
	"voxhold-backend/internal/testdb"
)

func TestAvatarOptimizationPrivateCacheAndRemoval(t *testing.T) {
	db := testdb.Open(t)
	var events int
	var version *string
	handler := NewHandler(db, profile.NewService(profilesqlite.NewRepository(db)), func(_ int64, next *string) { events++; version = next })
	user := int64(1)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(account.ContextWithUserID(r.Context(), user)))
		})
	})
	input := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			input.Set(x, y, color.RGBA{10, 80, 150, 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, input); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("PUT", "/api/v1/me/avatar", &data))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	user = 2
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/users/1/avatar", nil))
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	config, kind, err := image.DecodeConfig(bytes.NewReader(response.Body.Bytes()))
	if err != nil || kind != "jpeg" || config.Width != 256 || config.Height != 256 || response.Body.Len() > 131072 {
		t.Fatal(config, kind, err)
	}
	etag := response.Header().Get("ETag")
	if version == nil || `"`+*version+`"` != etag {
		t.Fatalf("event version %v, etag %s", version, etag)
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/users/1/avatar?v="+*version, nil))
	if response.Code != 200 || response.Header().Get("Cache-Control") != "private, max-age=31536000, immutable" {
		t.Fatal(response.Code, response.Header().Get("Cache-Control"))
	}
	request := httptest.NewRequest("GET", "/api/v1/users/1/avatar", nil)
	request.Header.Set("If-None-Match", etag)
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 304 {
		t.Fatal(response.Code)
	}
	user = 3
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != 404 {
		t.Fatalf("cache must not bypass visibility: %d", response.Code)
	}
	user = 1
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("DELETE", "/api/v1/me/avatar", nil))
	if response.Code != 204 || events != 2 || version != nil {
		t.Fatal(response.Code, events)
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/users/1/avatar", nil))
	if response.Code != 404 {
		t.Fatal(response.Code)
	}
}
