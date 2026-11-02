package accounthttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"voxhold-backend/internal/account"
)

// validatingService runs only the input validation of a real registration.
type validatingService struct{}

func (validatingService) Register(_ context.Context, input account.RegisterInput) (account.LoginResult, error) {
	return account.LoginResult{}, input.Normalize().Validate()
}
func (validatingService) Login(context.Context, account.LoginInput) (account.LoginResult, error) {
	return account.LoginResult{}, nil
}
func (validatingService) Logout(context.Context, string) error { return nil }
func (validatingService) Authenticate(context.Context, string) (int64, error) {
	return 0, account.ErrUnauthorized
}
func (validatingService) Refresh(context.Context, string) (account.SessionInfo, error) {
	return account.SessionInfo{}, nil
}

func TestRegisterRejectsForbiddenUsernameCharactersAsBadRequest(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(validatingService{}).RegisterRoutes(mux)
	body := `{"username":"bad@name","password":"long-enough","password_confirm":"long-enough","invite_token":"x"}`
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
}
