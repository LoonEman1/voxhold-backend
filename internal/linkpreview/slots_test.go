package linkpreview

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"voxhold-backend/internal/account"
)

func TestOneUserCannotTakeEverySlot(t *testing.T) {
	s := New()
	defer s.Close()
	s.userWait = 20 * time.Millisecond
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
	if len(s.users) != 0 {
		t.Fatalf("user slots leaked: %d", len(s.users))
	}
}

func TestSameUserWaitsForItsEarlierFetch(t *testing.T) {
	s := New()
	defer s.Close()
	request := func() (*httptest.ResponseRecorder, *http.Request) {
		r := httptest.NewRequest("GET", "/api/v1/link-preview", nil)
		return httptest.NewRecorder(), r.WithContext(account.ContextWithUserID(r.Context(), 1))
	}
	w, r := request()
	release, ok := s.acquire(w, r)
	if !ok {
		t.Fatal("first slot refused")
	}
	done := make(chan bool, 1)
	go func() {
		w, r := request()
		second, ok := s.acquire(w, r)
		if ok {
			second()
		}
		done <- ok
	}()
	time.Sleep(50 * time.Millisecond)
	release()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("queued request of the same user was rejected")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued request never got the slot")
	}
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
