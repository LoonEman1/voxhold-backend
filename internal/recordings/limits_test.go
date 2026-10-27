package recordings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"voxhold-backend/internal/account"
)

func TestRecordingListReportsConfiguredQuotaToMembers(t *testing.T) {
	s := setup(t)
	user := int64(2)
	mux := http.NewServeMux()
	NewHandler(s).RegisterRoutes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(account.ContextWithUserID(r.Context(), user)))
		})
	})
	for _, quota := range []int64{8 << 30, 3 << 30} {
		if _, err := s.SetQuota(ctx, 1, quota); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/servers/1/recordings", nil))
		var limits struct {
			QuotaBytes       int64 `json:"quota_bytes"`
			MaxFileBytes     int64 `json:"max_file_bytes"`
			RetentionSeconds int64 `json:"retention_seconds"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &limits); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || limits.QuotaBytes != quota || limits.MaxFileBytes != MaxFileBytes || limits.RetentionSeconds != 86400 {
			t.Fatal(response.Code, response.Body.String())
		}
	}
	user = 3 // A nonmember cannot read the recording list or its limits.
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/servers/1/recordings", nil))
	if response.Code != http.StatusNotFound {
		t.Fatal(response.Code, response.Body.String())
	}
}
