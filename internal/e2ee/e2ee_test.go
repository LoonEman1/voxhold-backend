package e2ee

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voxhold-backend/internal/account"
	"voxhold-backend/internal/testdb"
)

func publicKey(t *testing.T) []byte {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key.PublicKey().Bytes()
}

func device(n int) string { return fmt.Sprintf("%032x", n) }

func TestValidatePublicKey(t *testing.T) {
	valid := publicKey(t)
	if err := ValidatePublicKey(valid); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
	offCurve := append([]byte(nil), valid...)
	offCurve[64] ^= 1
	for name, raw := range map[string][]byte{"empty": nil, "compressed": valid[:33], "prefix": append([]byte{0x05}, valid[1:]...), "off curve": offCurve} {
		if err := ValidatePublicKey(raw); !errors.Is(err, ErrInvalidPublicKey) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
}

func TestRegisterIsIdempotentButRejectsKeyChangesAndAnEleventhDevice(t *testing.T) {
	repo := NewRepository(testdb.Open(t))
	ctx := context.Background()
	key := publicKey(t)
	if err := repo.Register(ctx, 1, device(1), key); err != nil {
		t.Fatal(err)
	}
	if err := repo.Register(ctx, 1, device(1), key); err != nil {
		t.Fatalf("same key again: %v", err)
	}
	if err := repo.Register(ctx, 1, device(1), publicKey(t)); !errors.Is(err, ErrKeyConflict) {
		t.Fatalf("key change err=%v", err)
	}
	for n := 2; n <= MaxActiveDevices; n++ {
		if err := repo.Register(ctx, 1, device(n), publicKey(t)); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Register(ctx, 1, device(99), publicKey(t)); !errors.Is(err, ErrTooManyDevices) {
		t.Fatalf("11th device err=%v", err)
	}
	if err := repo.Revoke(ctx, 1, device(2)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Register(ctx, 1, device(99), publicKey(t)); err != nil {
		t.Fatalf("slot freed by revocation: %v", err)
	}
	if err := repo.Register(ctx, 1, device(2), publicKey(t)); !errors.Is(err, ErrKeyConflict) {
		t.Fatalf("revoked device id reused: %v", err)
	}
	if active, _ := repo.DeviceActive(ctx, 1, device(2)); active {
		t.Fatal("revoked device still active")
	}
}

func TestDevicesAreVisibleOnlyToPeopleWhoShareSomething(t *testing.T) {
	db := testdb.Open(t)
	repo := NewRepository(db)
	ctx := context.Background()
	if err := repo.Register(ctx, 2, device(5), publicKey(t)); err != nil {
		t.Fatal(err)
	}
	if devices, err := repo.Devices(ctx, 1, 2); err != nil || len(devices) != 1 || devices[0].DeviceID != device(5) {
		t.Fatalf("server peer: %v %v", devices, err)
	}
	if _, err := repo.Devices(ctx, 3, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stranger err=%v", err)
	}
}

func TestChannelPolicyNeedsAManagerAndAVoiceChannel(t *testing.T) {
	repo := NewRepository(testdb.Open(t))
	ctx := context.Background()
	if _, err := repo.SetChannelPolicy(ctx, 1, 1, 2, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member err=%v", err)
	}
	if _, err := repo.SetChannelPolicy(ctx, 1, 2, 1, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("text channel err=%v", err)
	}
	if changed, err := repo.SetChannelPolicy(ctx, 1, 1, 1, true); err != nil || !changed {
		t.Fatalf("owner: changed=%v err=%v", changed, err)
	}
	if changed, err := repo.SetChannelPolicy(ctx, 1, 1, 1, true); err != nil || changed {
		t.Fatalf("repeat: changed=%v err=%v", changed, err)
	}
	if required, err := repo.ChannelPolicy(ctx, 1, 1, 2); err != nil || !required {
		t.Fatalf("member read: %v %v", required, err)
	}
	if _, err := repo.ChannelPolicy(ctx, 1, 1, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("outsider read err=%v", err)
	}
}

func TestHTTPRegistrationAndPolicyChange(t *testing.T) {
	db := testdb.Open(t)
	var changed []int64
	mux := http.NewServeMux()
	NewHandler(NewRepository(db), func(_, channelID int64, _ bool) { changed = append(changed, channelID) }).
		RegisterRoutes(mux, func(next http.Handler) http.Handler { return next })
	call := func(user int64, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r = r.WithContext(account.ContextWithUserID(r.Context(), user))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	encoded := base64.StdEncoding.EncodeToString(publicKey(t))
	if w := call(2, "PUT", "/api/v1/me/e2ee/devices/"+device(7), `{"public_key":"`+encoded+`"}`); w.Code != http.StatusNoContent {
		t.Fatalf("register %d %s", w.Code, w.Body)
	}
	if w := call(2, "PUT", "/api/v1/me/e2ee/devices/"+device(8), `{"public_key":"AAAA"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid key %d", w.Code)
	}
	if w := call(2, "PUT", "/api/v1/me/e2ee/devices/NOT-HEX", `{"public_key":"`+encoded+`"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid device %d", w.Code)
	}
	w := call(1, "GET", "/api/v1/users/2/e2ee/devices", "")
	var listed struct{ Devices []Device }
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed.Devices) != 1 {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	if w := call(3, "GET", "/api/v1/users/2/e2ee/devices", ""); w.Code != http.StatusNotFound {
		t.Fatalf("stranger %d", w.Code)
	}
	if w := call(2, "PUT", "/api/v1/servers/1/channels/1/e2ee", `{"required":true}`); w.Code != http.StatusForbidden {
		t.Fatalf("member policy %d", w.Code)
	}
	if w := call(1, "PUT", "/api/v1/servers/1/channels/1/e2ee", `{"required":true}`); w.Code != http.StatusOK || len(changed) != 1 {
		t.Fatalf("owner policy %d changed=%v", w.Code, changed)
	}
	if w := call(2, "GET", "/api/v1/servers/1/channels/1/e2ee", ""); !strings.Contains(w.Body.String(), `"required":true`) {
		t.Fatalf("policy read %s", w.Body)
	}
}
