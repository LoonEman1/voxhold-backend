package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type decodeJSONRequest struct {
	Name string `json:"name"`
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantOK     bool
		wantStatus int
	}{
		{
			name:   "valid object",
			body:   `{"name":"voxhold"}`,
			wantOK: true,
		},
		{
			name:       "empty body",
			body:       "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown field",
			body:       `{"name":"voxhold","admin":true}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "multiple JSON values",
			body:       `{"name":"first"} {"name":"second"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "body too large",
			body: `{"name":"` +
				strings.Repeat("a", int(MaxJSONBodyBytes)) +
				`"}`,
			wantStatus: http.StatusRequestEntityTooLarge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(
				http.MethodPost,
				"/",
				strings.NewReader(test.body),
			)
			response := httptest.NewRecorder()

			var destination decodeJSONRequest
			ok := DecodeJSON(response, request, &destination)

			if ok != test.wantOK {
				t.Fatalf("DecodeJSON() = %v, want %v", ok, test.wantOK)
			}

			if test.wantOK {
				if destination.Name != "voxhold" {
					t.Fatalf(
						"decoded name = %q, want %q",
						destination.Name,
						"voxhold",
					)
				}
				return
			}

			if response.Code != test.wantStatus {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					response.Code,
					test.wantStatus,
					response.Body.String(),
				)
			}
		})
	}
}
