// Package community implements personal conversations, roles and attention views.
package community

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"
	"voxhold-backend/internal/httpapi"
	messageDomain "voxhold-backend/internal/message"
	"voxhold-backend/internal/storagebudget"
)

var ErrForbidden = errors.New("access denied")
var ErrInvalid = errors.New("invalid request")
var ErrConflict = errors.New("request conflicts with existing data")

type Service struct {
	db             *sql.DB
	messages       *messageDomain.Service
	copyAttachment func(context.Context, int64, string, string, int64, string) (string, error)
	workMu         sync.Mutex
	changed        func(int64)
	notify         func(int64, string, any)
}

func New(db *sql.DB, changed func(int64), notify func(int64, string, any)) *Service {
	return &Service{db: db, changed: changed, notify: notify}
}

type endpoint func(*http.Request, int64) (any, error)

func (s *Service) wrap(fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpapi.AuthenticatedUserID(w, r)
		if !ok {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		result, err := fn(r, id)
		if err != nil {
			code, message := 500, "internal server error"
			switch {
			case errors.Is(err, messageDomain.ErrForbidden), errors.Is(err, ErrForbidden):
				code, message = 403, err.Error()
			case errors.Is(err, messageDomain.ErrPollInvalid), errors.Is(err, messageDomain.ErrClientMessageIDInvalid), errors.Is(err, messageDomain.ErrForwardInvalid), errors.Is(err, messageDomain.ErrContentRequired), errors.Is(err, messageDomain.ErrContentTooLong), errors.Is(err, messageDomain.ErrContentInvalid), errors.Is(err, messageDomain.ErrReplyInvalid), errors.Is(err, ErrInvalid):
				code, message = 400, err.Error()
			case errors.Is(err, messageDomain.ErrIdempotencyConflict), errors.Is(err, ErrConflict):
				code, message = 409, err.Error()
			case errors.Is(err, storagebudget.ErrBudget):
				code, message = 507, "Недостаточно места для копирования вложения"
			case errors.Is(err, messageDomain.ErrMessageNotFound), errors.Is(err, messageDomain.ErrChannelNotFound), errors.Is(err, sql.ErrNoRows):
				code, message = 404, "not found"
			default:
				log.Printf("community %s: %v", r.Pattern, err)
			}
			httpapi.WriteError(w, code, message)
			return
		}
		httpapi.WriteJSON(w, 200, result)
	}
}
func path(r *http.Request, key string) int64 {
	v, _ := strconv.ParseInt(r.PathValue(key), 10, 64)
	return v
}
func decode(r *http.Request, v any) error {
	// Use the shared decoder without writing a second response from an endpoint.
	var sink responseSink
	if !httpapi.DecodeJSON(&sink, r, v) {
		return ErrInvalid
	}
	return nil
}

type responseSink struct{ header http.Header }

func (s *responseSink) Header() http.Header {
	if s.header == nil {
		s.header = http.Header{}
	}
	return s.header
}
func (*responseSink) Write(p []byte) (int, error) { return len(p), nil }
func (*responseSink) WriteHeader(int)             {}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func owner(ctx context.Context, q queryer, serverID, userID int64) error {
	var yes bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM server_members WHERE server_id=? AND user_id=? AND role='owner')`, serverID, userID).Scan(&yes)
	if err != nil {
		return err
	}
	if !yes {
		return ErrForbidden
	}
	return nil
}
func accessible(ctx context.Context, q queryer, channelID, userID int64) error {
	var yes bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM channel_accessible_members WHERE channel_id=? AND user_id=?)`, channelID, userID).Scan(&yes)
	if err != nil {
		return err
	}
	if !yes {
		return ErrForbidden
	}
	return nil
}
func (s *Service) RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler) {
	s.registerFeatures(mux, auth)
	routes := map[string]endpoint{
		"GET /api/v1/servers/{serverID}/roles":                            s.roles,
		"POST /api/v1/servers/{serverID}/roles":                           s.saveRole,
		"PUT /api/v1/servers/{serverID}/roles/{roleID}":                   s.saveRole,
		"DELETE /api/v1/servers/{serverID}/roles/{roleID}":                s.deleteRole,
		"PUT /api/v1/servers/{serverID}/members/{userID}/roles":           s.assignRoles,
		"GET /api/v1/servers/{serverID}/roles/{roleID}/preview":           s.previewRole,
		"PUT /api/v1/servers/{serverID}/channels/{channelID}/role-grants": s.roleGrants,
		"GET /api/v1/servers/{serverID}/channels/{channelID}/role-grants": s.roleGrants,
		"GET /api/v1/me/inbox":                                            s.inbox,
		"PUT /api/v1/me/inbox/{messageID}":                                s.readInbox,
		"GET /api/v1/channels/{channelID}/threads/{messageID}":            s.thread,
		"PUT /api/v1/channels/{channelID}/threads/{messageID}":            s.setThread,
		"GET /api/v1/me/message-privacy":                                  s.privacy,
		"PUT /api/v1/me/message-privacy":                                  s.privacy,
		"PUT /api/v1/me/blocks/{userID}":                                  s.block,
		"DELETE /api/v1/me/blocks/{userID}":                               s.block,
		"GET /api/v1/me/conversations":                                    s.conversations,
		"POST /api/v1/me/conversations":                                   s.createConversation,
		"GET /api/v1/conversations/{conversationID}/messages":             s.conversationMessages,
		"POST /api/v1/conversations/{conversationID}/messages":            s.sendConversation,
		"PUT /api/v1/conversations/{conversationID}/read":                 s.readConversation,
		"DELETE /api/v1/conversations/{conversationID}/members/me":        s.leaveConversation,
	}
	for pattern, fn := range routes {
		mux.Handle(pattern, auth(s.wrap(fn)))
	}
}
