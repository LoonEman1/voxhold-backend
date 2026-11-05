package linkpreview

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnavailableSiteReturnsCachedURLCard(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		s := New()
		calls := 0
		s.client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("unavailable")), Header: http.Header{}, Request: r}, nil
		})}
		for range 2 {
			w := httptest.NewRecorder()
			s.preview(w, httptest.NewRequest("GET", "/api/v1/link-preview?url=https%3A%2F%2Fwww.gosuslugi.ru%2F", nil))
			var p Preview
			if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || p.URL != "https://www.gosuslugi.ru/" || p.Title != "www.gosuslugi.ru" || !p.Unavailable || p.ImagePath != "" {
				t.Fatalf("unexpected fallback: %d %s", w.Code, w.Body.String())
			}
		}
		if calls != 1 {
			t.Fatalf("uncached failure: %d fetches", calls)
		}
		s.Close()
	}
}
