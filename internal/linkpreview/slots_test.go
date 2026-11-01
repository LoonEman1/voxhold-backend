package linkpreview

import (
	"net/http/httptest"
	"testing"

	"voxhold-backend/internal/account"
)


func TestOneUserCannotTakeEverySlot(t *testing.T) {
	s := New()
	defer s.Close()
	acquire := func(user int64) (func(), bool) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/link-preview", nil)
		r = r.WithContext(account.ContextWithUserID(r.Context(), user))
		return s.acquire(w, r)
	}
	release, ok := acquire(1)
	if !ok {
		t.Fatal("first slot refused")
	}
	if _, ok = acquire(1); ok {
		t.Fatal("same user obtained a second slot")
	}
	other, ok := acquire(2)
	if !ok {
		t.Fatal("another user must still get a slot")
	}
	release()
	again, ok := acquire(1)
	if !ok {
		t.Fatal("slot not freed")
	}
	again()
	other()
}

func TestUncachedPreviewsAreRateLimitedPerUser(t *testing.T) {
	s := New()
	defer s.Close()
	allowed := 0
	for i := 0; i < 20; i++ {
		if s.allowUser(1) {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("burst allowed %d requests, want 5", allowed)
	}
	if !s.allowUser(2) {
		t.Fatal("another user must not be throttled")
	}
}
