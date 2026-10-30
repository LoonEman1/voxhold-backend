package attachments

import (
	"bytes"
	"context"
	"golang.org/x/image/draw"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"time"
	"voxhold-backend/internal/httpapi"
)

type Changed struct {
	ChannelID      int64    `json:"channel_id,omitempty"`
	ConversationID int64    `json:"conversation_id,omitempty"`
	AttachmentIDs  []string `json:"attachment_ids"`
}

func (v Changed) EventChannelID() int64 { return v.ChannelID }

func (s *Service) contentMetadata(ctx context.Context, uid int64, id string) (name, typ string, deleted bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT a.filename,a.mime_type,a.deleted_at IS NOT NULL FROM attachments a WHERE a.id=? AND ((a.channel_id IS NOT NULL AND EXISTS(SELECT 1 FROM channel_accessible_members c WHERE c.channel_id=a.channel_id AND c.user_id=?)) OR (a.conversation_id IS NOT NULL AND EXISTS(SELECT 1 FROM conversation_members m WHERE m.conversation_id=a.conversation_id AND m.user_id=?))) AND (a.message_id IS NOT NULL OR a.direct_message_id IS NOT NULL OR a.owner_user_id=?)`, id, uid, uid, uid).Scan(&name, &typ, &deleted)
	return
}

// Thumbnails are bounded in-memory derivatives; original files remain the only
// persistent storage reservation. Every cache hit still rechecks current ACL.
func (s *Service) thumbnail(w http.ResponseWriter, r *http.Request) {
	uid, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	id := r.PathValue("attachmentID")
	if !validID(id) {
		fail(w, 404, "attachment not found")
		return
	}
	name, typ, deleted, err := s.contentMetadata(r.Context(), uid, id)
	if err != nil {
		fail(w, 404, "attachment not found")
		return
	}
	if deleted {
		fail(w, 410, "attachment removed by server")
		return
	}
	if typ != "image/jpeg" && typ != "image/png" {
		fail(w, 404, "thumbnail unavailable")
		return
	}
	s.thumbMu.Lock()
	data := s.thumbnails[id]
	s.thumbMu.Unlock()
	if data == nil {
		select {
		case s.thumbnailSlots <- struct{}{}:
			defer func() { <-s.thumbnailSlots }()
		default:
			w.Header().Set("Retry-After", "1")
			fail(w, 429, "thumbnails busy; retry shortly")
			return
		}
		f, err := s.root.Open(id)
		if err != nil {
			fail(w, 410, "attachment removed by server")
			return
		}
		defer f.Close()
		config, _, err := image.DecodeConfig(f)
		if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 30_000_000 {
			fail(w, 400, "invalid image")
			return
		}
		if _, err = f.Seek(0, 0); err != nil {
			fail(w, 500, "thumbnail unavailable")
			return
		}
		img, _, err := image.Decode(f)
		if err != nil {
			fail(w, 400, "invalid image")
			return
		}
		ratio := min(1.0, 480/float64(config.Width), 480/float64(config.Height))
		dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(config.Width)*ratio)), max(1, int(float64(config.Height)*ratio))))
		draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
		var encoded bytes.Buffer
		if err = jpeg.Encode(&encoded, dst, &jpeg.Options{Quality: 70}); err != nil {
			fail(w, 500, "thumbnail unavailable")
			return
		}
		data = encoded.Bytes()
		s.thumbMu.Lock()
		if s.thumbnails[id] == nil {
			if len(s.thumbnailOrder) >= 64 {
				delete(s.thumbnails, s.thumbnailOrder[0])
				s.thumbnailOrder = s.thumbnailOrder[1:]
			}
			s.thumbnailOrder = append(s.thumbnailOrder, id)
			s.thumbnails[id] = data
		}
		s.thumbMu.Unlock()
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
