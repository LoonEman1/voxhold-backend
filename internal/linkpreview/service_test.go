package linkpreview

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"voxhold-backend/internal/leaktest"
)

func TestMain(m *testing.M) { os.Exit(leaktest.Run(m)) }
func TestRejectsLocalReservedAndCredentialURLs(t *testing.T) {
	for _, raw := range []string{"http://example.com", "file:///etc/passwd", "https://127.0.0.1", "https://[::1]", "https://10.0.0.1", "https://169.254.169.254/latest/meta-data", "https://100.100.100.200", "https://168.63.129.16", "https://[::ffff:127.0.0.1]", "https://[64:ff9b::7f00:1]", "https://[2001:db8::1]", "https://user:pass@example.com", "https://example.com:8443"} {
		if _, err := safeURL(raw); err == nil {
			t.Errorf("allowed %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(raw)) {
			t.Error(raw)
		}
	}
	client := newClient()
	defer client.CloseIdleConnections()
	for _, raw := range []string{"https://localhost", "https://127.0.0.1"} {
		if _, _, _, err := fetch(context.Background(), client, raw, 100); err == nil {
			t.Error("local request succeeded")
		}
	}
	req := httptest.NewRequest("GET", "https://127.0.0.1", nil)
	if err := client.CheckRedirect(req, []*http.Request{{}}); err == nil {
		t.Fatal("local redirect accepted")
	}
}
func TestUniversalOpenGraphTwitterAndHTMLMetadata(t *testing.T) {
	base, _ := url.Parse("https://publisher.example/articles/one")
	for _, tc := range []struct{ html, title, description, image string }{
		{`<head><title>Fallback</title><meta property="og:title" content="Article &amp; review"><meta property="og:description" content="Details"><meta property="og:image" content="/cover.jpg"></head><body>ignored`, "Article & review", "Details", "https://publisher.example/cover.jpg"},
		{`<meta name="twitter:title" content="Social card"><meta name="twitter:image" content="https://cdn.example/img.webp">`, "Social card", "", "https://cdn.example/img.webp"},
		{`<head><title>A normal page</title><meta name="description" content="HTML summary"></head>`, "A normal page", "HTML summary", ""},
		{`<title>Safe text</title><meta property="og:image" content="https://127.0.0.1/private">`, "Safe text", "", ""},
	} {
		p, img, err := parseHTML([]byte(tc.html), "text/html; charset=utf-8", base)
		if err != nil || p.Title != tc.title || p.Description != tc.description || img != tc.image {
			t.Fatalf("%+v image=%s err=%v", p, img, err)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestYouTubeOEmbedDoesNotUseProviderHTML(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.youtube.com" || r.URL.Path != "/oembed" {
			t.Fatal(r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"title":"Video title","thumbnail_url":"https://i.ytimg.com/vi/abcDEF123_-/hqdefault.jpg","html":"<script>ignored</script>"}`)), Request: r}, nil
	})}
	u, _ := url.Parse("https://youtu.be/abcDEF123_-")
	p, img, err := metadata(context.Background(), client, u)
	if err != nil || p.Title != "Video title" || p.Site != "YouTube" || !strings.HasPrefix(img, "https://i.ytimg.com/") {
		t.Fatalf("%+v %s %v", p, img, err)
	}
}

func TestFetchSizeAndRedirectBudgets(t *testing.T) {
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 65))), Request: r}, nil
	})}
	if _, _, _, err := fetch(context.Background(), client, "https://example.com", 64); err == nil {
		t.Fatal("oversized response accepted")
	}
	checked := newClient()
	defer checked.CloseIdleConnections()
	r := httptest.NewRequest("GET", "https://example.com", nil)
	r.Header.Set("Referer", "https://publisher.example/?private=query")
	if err := checked.CheckRedirect(r, []*http.Request{{}}); err != nil || r.Header.Get("Referer") != "" {
		t.Fatal("referer was forwarded", err)
	}
	if err := checked.CheckRedirect(r, []*http.Request{{}, {}, {}}); err == nil {
		t.Fatal("redirect budget ignored")
	}
}
func TestPreviewCacheAndProxiedBoundedThumbnail(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 1000, 600))); err != nil {
		t.Fatal(err)
	}
	s := New()
	defer s.Close()
	requests := 0
	s.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		data := `<title>Generic article</title><meta property="og:image" content="https://cdn.example/image.png">`
		typ := "text/html"
		if r.URL.Host == "cdn.example" {
			data = imageData.String()
			typ = "image/png"
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("credentials leaked")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{typ}}, Body: io.NopCloser(strings.NewReader(data)), Request: r}, nil
	})}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux, func(h http.Handler) http.Handler { return h })
	var p Preview
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/link-preview?url=https%3A%2F%2Fexample.com%2Farticle", nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 1 || !strings.HasPrefix(p.ImagePath, "/api/v1/link-preview/images/") {
		t.Fatalf("requests=%d %+v", requests, p)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", p.ImagePath, nil))
	config, _, err := image.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if w.Code != 200 || err != nil || config.Width > 640 || config.Height > 480 {
		t.Fatalf("%d %+v %v", w.Code, config, err)
	}
	if w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatal(w.Header())
	}
	// Expired entries cannot be used as a general image proxy.
	s.mu.Lock()
	for k, v := range s.cache {
		v.expires = time.Now().Add(-time.Second)
		s.cache[k] = v
	}
	s.mu.Unlock()
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", p.ImagePath, nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
