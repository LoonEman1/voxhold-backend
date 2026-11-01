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
