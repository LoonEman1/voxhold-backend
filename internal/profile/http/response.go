package profilehttp

import "voxhold-backend/internal/profile"

type profileResponse struct {
	UserID      int64   `json:"user_id"`
	Username    string  `json:"username"`
	About       string  `json:"about"`
	CountryCode *string `json:"country_code"`
	CreatedAt   int64   `json:"created_at"`
	LastSeenAt  *int64  `json:"last_seen_at"`
	UpdatedAt   *int64  `json:"updated_at"`
}

func newProfileResponse(
	value profile.Profile,
) profileResponse {
	return profileResponse{
		UserID:      value.UserID,
		Username:    value.Username,
		About:       value.About,
		CountryCode: value.CountryCode,
		CreatedAt:   value.CreatedAt,
		LastSeenAt:  value.LastSeenAt,
		UpdatedAt:   value.UpdatedAt,
	}
}
