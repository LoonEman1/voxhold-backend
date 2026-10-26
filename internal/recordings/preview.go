package recordings

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"voxhold-backend/internal/httpapi"
)

const MaxPreviewUpload = 128 << 10

var ErrPreview = errors.New("use a JPEG, PNG or WebP preview up to 128 KiB and 1280x720 pixels")

func optimizePreview(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxPreviewUpload {
		return nil, ErrPreview
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") || config.Width <= 0 || config.Height <= 0 || config.Width > 1280 || config.Height > 720 {
		return nil, ErrPreview
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrPreview
	}
	output := image.NewRGBA(image.Rect(0, 0, 320, 180))
	draw.Draw(output, output.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	w, h := 320, 180
	if config.Width*180 > config.Height*320 {
		h = max(1, config.Height*320/config.Width)
	} else {
		w = max(1, config.Width*180/config.Height)
	}
	rect := image.Rect((320-w)/2, (180-h)/2, (320+w)/2, (180+h)/2)
	draw.CatmullRom.Scale(output, rect, source, source.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, output, &jpeg.Options{Quality: 75}); err != nil {
		return nil, err
	}
	if encoded.Len() > 65536 {
		return nil, ErrPreview
	}
	return encoded.Bytes(), nil
}

func (s *Service) SavePreview(ctx context.Context, id string, userID int64, data []byte) (Record, error) {
	// Check access before decoding, and again under the mutation lock afterwards.
	r, err := s.Get(ctx, id, userID)
	if err != nil {
		return r, err
	}
	if r.OwnerUserID != userID {
		return r, ErrNotFound
	}
	if r.Status != "ready" {
		return r, ErrConflict
	}
	content, err := optimizePreview(data)
	if err != nil {
		return r, err
	}
	checksum := sha256.Sum256(content)
	etag := hex.EncodeToString(checksum[:])
	s.mu.Lock()
	err = func() error {
		current, err := s.Get(ctx, id, userID)
		if err != nil {
			return err
		}
		if current.OwnerUserID != userID || current.Status != "ready" {
			return ErrConflict
		}
		used, err := s.used(ctx, id)
		if err != nil {
			return err
		}
		if used+current.SizeBytes+int64(len(content)) > s.quota.Load() {
			return ErrLimit
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, `INSERT INTO recording_previews(recording_id,content,etag) VALUES(?,?,?) ON CONFLICT(recording_id) DO UPDATE SET content=excluded.content,etag=excluded.etag`, id, content, etag); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE channel_recordings SET updated_at=max(updated_at+1,?) WHERE id=?`, s.now().UnixMilli(), id); err != nil {
			return err
		}
		return tx.Commit()
	}()
	s.mu.Unlock()
	if err != nil {
		return r, err
	}
	r, err = s.Get(ctx, id, userID)
	if err == nil {
		s.emit(r)
	}
	return r, err
}

func (h *Handler) putPreview(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	select {
	case h.previews <- struct{}{}:
		defer func() { <-h.previews }()
	default:
		httpapi.WriteError(w, 429, "preview processing is busy")
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Second))
	r.Body = http.MaxBytesReader(w, r.Body, MaxPreviewUpload)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		httpapi.WriteError(w, 413, ErrPreview.Error())
		return
	}
	record, err := h.service.SavePreview(r.Context(), r.PathValue("recordingID"), user, data)
	if err != nil {
		if errors.Is(err, ErrPreview) {
			httpapi.WriteError(w, 400, err.Error())
		} else {
			h.fail(w, err)
		}
		return
	}
	httpapi.WriteJSON(w, 200, record)
}

func (h *Handler) getPreview(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	record, err := h.service.Get(r.Context(), r.PathValue("recordingID"), user)
	if err != nil {
		h.fail(w, err)
		return
	}
	if record.Status != "ready" || record.PreviewVersion == nil {
		h.fail(w, ErrNotFound)
		return
	}
	var content []byte
	var etag string
	if err := h.service.db.QueryRowContext(r.Context(), `SELECT content,etag FROM recording_previews WHERE recording_id=?`, record.ID).Scan(&content, &etag); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		h.fail(w, err)
		return
	}
	etag = `"` + etag + `"`
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Add("Vary", "Authorization")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(304)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	_, _ = w.Write(content)
}
