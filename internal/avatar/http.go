package avatar

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"voxhold-backend/internal/httpapi"
	"voxhold-backend/internal/profile"
)

type Visibility interface {
	GetVisibleByUserID(context.Context, int64, int64) (profile.Profile, error)
}
type Handler struct {
	db       *sql.DB
	profiles Visibility
	publish  func(userID int64, version *string)
	slots    chan struct{}
}

// publish receives the new ETag, or nil after removal, so clients never probe for missing avatars.
func NewHandler(db *sql.DB, profiles Visibility, publish func(userID int64, version *string)) *Handler {
	return &Handler{db: db, profiles: profiles, publish: publish, slots: make(chan struct{}, 2)}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("PUT /api/v1/me/avatar", auth(http.HandlerFunc(h.put)))
	mux.Handle("DELETE /api/v1/me/avatar", auth(http.HandlerFunc(h.remove)))
	mux.Handle("GET /api/v1/users/{userID}/avatar", auth(http.HandlerFunc(h.get)))
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	default:
		httpapi.WriteError(w, 429, "image processing is busy; try again")
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(time.Minute))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(90 * time.Second))
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		httpapi.WriteError(w, 413, "avatar upload exceeds 5 MB")
		return
	}
	optimized, err := Optimize(data)
	if err != nil {
		httpapi.WriteError(w, 400, ErrImage.Error())
		return
	}
	checksum := sha256.Sum256(optimized)
	etag := hex.EncodeToString(checksum[:])
	_, err = h.db.ExecContext(r.Context(), `INSERT INTO user_avatars(user_id,content,etag) VALUES(?,?,?) ON CONFLICT(user_id) DO UPDATE SET content=excluded.content,etag=excluded.etag,updated_at=unixepoch()`, userID, optimized, etag)
	if err != nil {
		httpapi.WriteError(w, 500, "could not save avatar")
		return
	}
	if h.publish != nil {
		h.publish(userID, &etag)
	}
	httpapi.WriteJSON(w, 200, map[string]any{"etag": etag, "bytes": len(optimized), "width": Size, "height": Size})
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	userID, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	if _, err := h.db.ExecContext(r.Context(), "DELETE FROM user_avatars WHERE user_id=?", userID); err != nil {
		httpapi.WriteError(w, 500, "could not remove avatar")
		return
	}
	if h.publish != nil {
		h.publish(userID, nil)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	requester, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	target, err := strconv.ParseInt(r.PathValue("userID"), 10, 64)
	if err != nil || target <= 0 {
		httpapi.WriteError(w, 404, "avatar not found")
		return
	}
	if _, err = h.profiles.GetVisibleByUserID(r.Context(), requester, target); err != nil {
		if errors.Is(err, profile.ErrProfileNotFound) {
			httpapi.WriteError(w, 404, "avatar not found")
		} else {
			httpapi.WriteError(w, 500, "could not check avatar access")
		}
		return
	}
	var content []byte
	var etag string
	err = h.db.QueryRowContext(r.Context(), "SELECT content,etag FROM user_avatars WHERE user_id=?", target).Scan(&content, &etag)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.WriteError(w, 404, "avatar not found")
		return
	}
	if err != nil {
		httpapi.WriteError(w, 500, "could not read avatar")
		return
	}
	// Clients address avatars by version (?v=<etag>), so a matching URL never changes.
	if r.URL.Query().Get("v") == etag {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "private, max-age=300, must-revalidate")
	}
	etag = `"` + etag + `"`
	w.Header().Set("ETag", etag)
	w.Header().Add("Vary", "Authorization")
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	_, _ = w.Write(content)
}
