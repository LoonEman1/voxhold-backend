package linkpreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"voxhold-backend/internal/httpapi"
)

type entry struct {
	preview  Preview
	imageURL string
	image    []byte
	expires  time.Time
	failed   bool
}
type Service struct {
	mu     sync.Mutex
	cache  map[string]entry
	slots  chan struct{}
	client *http.Client
}

func New() *Service {
	return &Service{cache: map[string]entry{}, slots: make(chan struct{}, 4), client: newClient()}
}
func (s *Service) Close() { s.client.CloseIdleConnections() }
func (s *Service) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/link-preview", auth(http.HandlerFunc(s.preview)))
	mux.Handle("GET /api/v1/link-preview/images/{key}", auth(http.HandlerFunc(s.image)))
}
func (s *Service) get(key string) (entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.cache[key]
	if ok && time.Now().Before(value.expires) {
		return value, true
	}
	delete(s.cache, key)
	return entry{}, false
}
func (s *Service) put(key string, value entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.cache {
		if time.Now().After(v.expires) {
			delete(s.cache, k)
		}
	}
	if len(s.cache) >= 256 {
		var oldest string
		var at time.Time
		for k, v := range s.cache {
			if oldest == "" || v.expires.Before(at) {
				oldest = k
				at = v.expires
			}
		}
		delete(s.cache, oldest)
	}
	s.cache[key] = value
}
func (s *Service) acquire(w http.ResponseWriter) bool {
	select {
	case s.slots <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "2")
		httpapi.WriteError(w, 429, "preview busy")
		return false
	}
}
func (s *Service) preview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	u, err := safeURL(r.URL.Query().Get("url"))
	if err != nil {
		httpapi.WriteError(w, 400, "public HTTPS URL required")
		return
	}
	hash := sha256.Sum256([]byte(u.String()))
	key := hex.EncodeToString(hash[:])
	value, ok := s.get(key)
	if !ok {
		if !s.acquire(w) {
			return
		}
		defer func() { <-s.slots }()
		p, image, err := metadata(r.Context(), s.client, u)
		value = entry{preview: p, imageURL: image, expires: time.Now().Add(10 * time.Minute), failed: err != nil}
		if image != "" {
			value.preview.ImagePath = "/api/v1/link-preview/images/" + key
		}
		if err != nil {
			value.expires = time.Now().Add(time.Minute)
		}
		s.put(key, value)
	}
	if value.failed {
		httpapi.WriteJSON(w, 200, nil)
		return
	}
	httpapi.WriteJSON(w, 200, value.preview)
}
func (s *Service) image(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, max-age=300")
	key := r.PathValue("key")
	if len(key) != 64 {
		httpapi.WriteError(w, 404, "preview not found")
		return
	}
	value, ok := s.get(key)
	if !ok || value.failed || value.imageURL == "" {
		httpapi.WriteError(w, 404, "preview not found")
		return
	}
	data := value.image
	if data == nil {
		if !s.acquire(w) {
			return
		}
		defer func() { <-s.slots }()
		raw, typ, _, err := fetch(r.Context(), s.client, value.imageURL, 2<<20)
		if err != nil || !strings.HasPrefix(typ, "image/") {
			httpapi.WriteError(w, 404, "preview image unavailable")
			return
		}
		data, err = thumbnail(raw)
		if err != nil {
			httpapi.WriteError(w, 404, "preview image unavailable")
			return
		}
		// Never resurrect an expired/replaced metadata entry after an image fetch.
		s.mu.Lock()
		if current, ok := s.cache[key]; ok && current.expires.Equal(value.expires) {
			current.image = data
			s.cache[key] = current
		}
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(data)
}
func thumbnail(data []byte) ([]byte, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 8_000_000 {
		return nil, errUnavailable
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	ratio := min(1.0, 640/float64(config.Width), 480/float64(config.Height))
	out := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(config.Width)*ratio)), max(1, int(float64(config.Height)*ratio))))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(out, out.Bounds(), img, img.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	err = jpeg.Encode(&encoded, out, &jpeg.Options{Quality: 80})
	return encoded.Bytes(), err
}
