package recordings

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
	"voxhold-backend/internal/httpapi"
)

type Handler struct {
	service   *Service
	uploads   chan struct{}
	downloads chan struct{}
	previews  chan struct{}
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service, uploads: make(chan struct{}, 4), downloads: make(chan struct{}, 4), previews: make(chan struct{}, 2)}
}
func (h *Handler) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/servers/{serverID}/recordings", auth(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/v1/servers/{serverID}/channels/{channelID}/recordings", auth(http.HandlerFunc(h.start)))
	mux.Handle("POST /api/v1/recordings/{recordingID}/heartbeat", auth(http.HandlerFunc(h.heartbeat)))
	mux.Handle("POST /api/v1/recordings/{recordingID}/stop", auth(http.HandlerFunc(h.stop)))
	mux.Handle("GET /api/v1/recordings/{recordingID}/upload", auth(http.HandlerFunc(h.offset)))
	mux.Handle("PATCH /api/v1/recordings/{recordingID}/content", auth(http.HandlerFunc(h.append)))
	mux.Handle("POST /api/v1/recordings/{recordingID}/complete", auth(http.HandlerFunc(h.complete)))
	mux.Handle("PUT /api/v1/recordings/{recordingID}/preview", auth(http.HandlerFunc(h.putPreview)))
	mux.Handle("GET /api/v1/recordings/{recordingID}/preview", auth(http.HandlerFunc(h.getPreview)))
	mux.Handle("POST /api/v1/recordings/{recordingID}/download", auth(http.HandlerFunc(h.ticket)))
	mux.HandleFunc("GET /api/v1/recording-downloads/{ticket}", h.download)
	mux.Handle("GET /api/v1/instance/recording-storage", auth(http.HandlerFunc(h.storage)))
	mux.Handle("PUT /api/v1/instance/recording-storage", auth(http.HandlerFunc(h.setStorage)))
}
func pathInt(r *http.Request, key string) int64 {
	value, _ := strconv.ParseInt(r.PathValue(key), 10, 64)
	return value
}
func (h *Handler) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, 404, ErrNotFound.Error())
	case errors.Is(err, ErrConflict):
		httpapi.WriteError(w, 409, ErrConflict.Error())
	case errors.Is(err, ErrLimit):
		httpapi.WriteError(w, 507, ErrLimit.Error())
	case errors.Is(err, ErrForbidden):
		httpapi.WriteError(w, 403, ErrForbidden.Error())
	case errors.Is(err, ErrInvalidQuota):
		httpapi.WriteError(w, 400, ErrInvalidQuota.Error())
	default:
		httpapi.WriteError(w, 500, "recording operation failed")
	}
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	records, err := h.service.List(r.Context(), pathInt(r, "serverID"), user)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.WriteJSON(w, 200, map[string]any{"recordings": records, "max_file_bytes": MaxFileBytes, "retention_seconds": int64(Retention / time.Second)})
}
func (h *Handler) storage(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	storage, err := h.service.Storage(r.Context(), user)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.WriteJSON(w, 200, storage)
}
func (h *Handler) setStorage(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	var input struct {
		QuotaBytes int64 `json:"quota_bytes"`
	}
	if !httpapi.DecodeJSON(w, r, &input) {
		return
	}
	storage, err := h.service.SetQuota(r.Context(), user, input.QuotaBytes)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.WriteJSON(w, 200, storage)
}
func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	var input struct {
		MIMEType string `json:"mime_type"`
	}
	if !httpapi.DecodeJSON(w, r, &input) {
		return
	}
	record, err := h.service.Start(r.Context(), pathInt(r, "serverID"), pathInt(r, "channelID"), user, input.MIMEType)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.WriteJSON(w, 201, record)
}
func (h *Handler) heartbeat(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	if err := h.service.Heartbeat(r.Context(), r.PathValue("recordingID"), user); err != nil {
		h.fail(w, err)
		return
	}
	w.WriteHeader(204)
}
func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	var input struct {
		Size      int64 `json:"size_bytes"`
		Duration  int64 `json:"duration_ms"`
		LocalOnly bool  `json:"local_only"`
	}
	if !httpapi.DecodeJSON(w, r, &input) {
		return
	}
	record, err := h.service.Stop(r.Context(), r.PathValue("recordingID"), user, input.Size, input.Duration, input.LocalOnly)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.WriteJSON(w, 200, record)
}
func (h *Handler) offset(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	offset, err := h.service.Offset(r.Context(), r.PathValue("recordingID"), user)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.WriteJSON(w, 200, map[string]int64{"offset": offset})
}
func (h *Handler) append(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	select {
	case h.uploads <- struct{}{}:
		defer func() { <-h.uploads }()
	default:
		httpapi.WriteError(w, 429, "recording uploads are busy; try again")
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil || offset < 0 {
		httpapi.WriteError(w, 400, "invalid upload offset")
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(2 * time.Minute))
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(150 * time.Second))
	r.Body = http.MaxBytesReader(w, r.Body, ChunkBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		httpapi.WriteError(w, 413, "recording chunk exceeds 1 MiB")
		return
	}
	next, err := h.service.Append(r.Context(), r.PathValue("recordingID"), user, offset, data)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.WriteJSON(w, 200, map[string]int64{"offset": next})
}
func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	record, err := h.service.Complete(r.Context(), r.PathValue("recordingID"), user)
	if err != nil {
		h.fail(w, err)
		return
	}
	httpapi.WriteJSON(w, 200, record)
}
func (h *Handler) ticket(w http.ResponseWriter, r *http.Request) {
	user, ok := httpapi.AuthenticatedUserID(w, r)
	if !ok {
		return
	}
	ticket, err := h.service.Ticket(r.Context(), r.PathValue("recordingID"), user)
	if err != nil {
		h.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpapi.WriteJSON(w, 200, map[string]string{"url": "/api/v1/recording-downloads/" + ticket})
}

type downloadWriter struct {
	http.ResponseWriter
	request *http.Request
	service *Service
}

func (w downloadWriter) Write(p []byte) (int, error) {
	// One shared budget bounds recording download traffic independently of RTP.
	written := 0
	for len(p) > 0 {
		n := min(len(p), 32<<10)
		if err := w.service.downloadRate.WaitN(w.request.Context(), n); err != nil {
			return written, err
		}
		_ = http.NewResponseController(w.ResponseWriter).SetWriteDeadline(time.Now().Add(20 * time.Second))
		count, err := w.ResponseWriter.Write(p[:n])
		written += count
		if err != nil {
			return written, err
		}
		if count != n {
			return written, io.ErrShortWrite
		}
		p = p[n:]
	}
	return written, nil
}
func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	select {
	case h.downloads <- struct{}{}:
		defer func() { <-h.downloads }()
	default:
		httpapi.WriteError(w, 429, "recording downloads are busy; try again")
		return
	}
	record, file, err := h.service.OpenDownload(r.Context(), r.PathValue("ticket"))
	if err != nil {
		h.fail(w, err)
		return
	}
	defer file.Close()
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Disposition", `attachment; filename="`+record.Filename()+`"`)
	w.Header().Set("Content-Type", record.MIMEType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(downloadWriter{w, r, h.service}, r, record.Filename(), time.Unix(record.CreatedAt, 0), file)
}
