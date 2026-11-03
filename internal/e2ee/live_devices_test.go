package e2ee

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/realtime"
	"voxhold-backend/internal/testdb"
)

func TestRevokedAndEvictedDevicesLoseLiveVoice(t *testing.T) {
	for _, eviction := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit revoke", true: "LRU eviction"}[eviction], func(t *testing.T) {
			db := testdb.Open(t)
			repo := NewRepository(db)
			key := publicKey(t)
			if err := repo.Register(context.Background(), 2, device(1), key); err != nil {
				t.Fatal(err)
			}
			hub := realtime.NewHub()
			defer hub.Close()
			hub.SetVoiceDeviceValidator(func(uid int64, id string) (bool, error) { return repo.DeviceActive(context.Background(), uid, id) })
			client := realtime.NewClient(2, "device", []int64{1})
			other := realtime.NewClient(1, "other", []int64{1})
			hub.Register(client)
			hub.Register(other)
			hub.JoinVoice(other, 1, 1, false, false)
			if _, ok := hub.JoinVoiceWithDevice(client, 1, 1, false, false, device(1)); !ok {
				t.Fatal("join")
			}
			mux := http.NewServeMux()
			NewHandler(repo, nil).WithDeviceChanges(hub.RevalidateVoiceE2EE).RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
			method, path, body := "DELETE", "/api/v1/me/e2ee/devices/"+device(1), ""
			if eviction {
				for id := 2; id <= MaxActiveDevices; id++ {
					if err := repo.Register(context.Background(), 2, device(id), key); err != nil {
						t.Fatal(err)
					}
				}
				method, path, body = "PUT", "/api/v1/me/e2ee/devices/"+device(MaxActiveDevices+1), `{"public_key":"`+base64.StdEncoding.EncodeToString(key)+`"}`
			}
			request := httptest.NewRequest(method, path, strings.NewReader(body))
			request = request.WithContext(account.ContextWithUserID(request.Context(), 2))
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("%d %s", response.Code, response.Body)
			}
			if _, ok := hub.UpdateVoiceState(client, false, false); ok {
				t.Fatal("revoked device remains in voice")
			}
			if _, ok := hub.JoinVoiceWithDevice(client, 1, 1, false, false, device(1)); ok {
				t.Fatal("late join reused a revoked device")
			}
			if _, ok := hub.UpdateVoiceState(other, false, false); !ok {
				t.Fatal("unrelated plain call was interrupted")
			}
		})
	}
}
