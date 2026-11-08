package api

import (
	"net/http"

	"github.com/ai-employee-platform/server/internal/auth"
)

func (d Deps) checkReauthRate(w http.ResponseWriter, r *http.Request, s *auth.Session) bool {
	switch r.URL.Path {
	case "/api/auth/step-up", "/api/security/step-up", "/api/auth/totp/enroll", "/api/auth/totp/disable", "/api/auth/totp/confirm":
		if !d.Auth.AllowReauthentication(s.UserID, clientIP(r)) {
			w.Header().Set("Retry-After", "60")
			writeErr(w, 429, auth.ErrReauthLimited.Error())
			return false
		}
	}
	return true
}
