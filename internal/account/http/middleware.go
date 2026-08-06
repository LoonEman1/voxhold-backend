package accounthttp

import (
	"errors"
	"log"
	"net/http"

	"voxhold-backend/internal/account"
)

func (h *Handler) RequireAuth(
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				writeError(
					w,
					http.StatusUnauthorized,
					"authorization token is required",
				)
				return
			}

			userID, err := h.service.Authenticate(
				r.Context(),
				token,
			)
			if err != nil {
				if errors.Is(err, account.ErrUnauthorized) {
					writeError(
						w,
						http.StatusUnauthorized,
						"invalid or expired session",
					)
					return
				}

				log.Printf("authenticate session: %v", err)

				writeError(
					w,
					http.StatusInternalServerError,
					"internal server error",
				)
				return
			}

			ctx := account.ContextWithUserID(
				r.Context(),
				userID,
			)

			next.ServeHTTP(
				w,
				r.WithContext(ctx),
			)
		},
	)
}
