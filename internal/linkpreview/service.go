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
	"voxhold-backend/internal/account"
	"voxhold-backend/internal/imagelimit"

	"golang.org/x/image/draw"
	"golang.org/x/time/rate"
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
	active map[int64]int
	rates  map[int64]*userRate
	client *http.Client
}

func New() *Service {
	return &Service{cache: map[string]entry{}, slots: make(chan struct{}, 4), active: map[int64]int{}, rates: map[int64]*userRate{}, client: newClient()}
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
// maxSlotsPerUser keeps one user from occupying every global fetch slot.
const maxSlotsPerUser = 1

// acquire reserves a global slot and a per-user slot; release frees both.
// userRate limits uncached preview fetches to 30 per minute per user.
type userRate struct {
	limiter *rate.Limiter
	seen    time.Time
}

func (s *Service) allowUser(userID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if len(s.rates) > 1000 {
		for id, entry := range s.rates {
			if now.Sub(entry.seen) > 10*time.Minute {
				delete(s.rates, id)
			}
		}
	}
	entry, ok := s.rates[userID]
	if !ok {
		entry = &userRate{limiter: rate.NewLimiter(rate.Every(2*time.Second), 5)}
		s.rates[userID] = entry
	}
	entry.seen = now
	return entry.limiter.AllowN(now, 1)
}

func (s *Service) acquire(w http.ResponseWriter, r *http.Request) (release func(), ok bool) {
	userID, _ := account.UserIDFromContext(r.Context())
	s.mu.Lock()
	if s.active[userID] >= maxSlotsPerUser {
		s.mu.Unlock()
		w.Header().Set("Retry-After", "2")
		httpapi.WriteError(w, 429, "preview busy")
		return nil, false
	}
	s.active[userID]++
	s.mu.Unlock()
	select {
	case s.slots <- struct{}{}:
		return func() {
			<-s.slots
			s.mu.Lock()
			if s.active[userID]--; s.active[userID] <= 0 {
				delete(s.active, userID)
			}
			s.mu.Unlock()
		}, true
	default:
		s.mu.Lock()
		if s.active[userID]--; s.active[userID] <= 0 {
			delete(s.active, userID)
		}
		s.mu.Unlock()
		w.Header().Set("Retry-After", "2")
		httpapi.WriteError(w, 429, "preview busy")
		return nil, false
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
		userID, _ := account.UserIDFromContext(r.Context())
		if !s.allowUser(userID) {
			w.Header().Set("Retry-After", "2")
			httpapi.WriteError(w, 429, "too many previews")
			return
		}
		release, ok := s.acquire(w, r)
		if !ok {
			return
		}
		defer release()
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
		release, ok := s.acquire(w, r)
		if !ok {
			return
		}
		defer release()
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
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 8_000_000 || imagelimit.DecodedBytes(config) > imagelimit.MaxDecodedBytes {
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
