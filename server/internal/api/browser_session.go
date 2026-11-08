package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/config"
)

const sessionCookie = "aie_session"

func cookieSecure(r *http.Request, c *config.Config) bool {
	return r.TLS != nil || c != nil && (strings.EqualFold(c.Env, "production") || strings.HasPrefix(c.PublicServerURL, "https://"))
}

// browserSession 不将浏览器会话密钥暴露给 JavaScript；CLI 继续支持 Bearer。
func (d Deps) browserSession(w http.ResponseWriter, r *http.Request, s *auth.Session) string {
	if r.Header.Get("X-AIE-Browser") != "1" {
		return s.Token
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: s.Token, Path: "/", HttpOnly: true, Secure: cookieSecure(r, d.Config), SameSite: http.SameSiteStrictMode, Expires: s.ExpiresAt, MaxAge: int(time.Until(s.ExpiresAt).Seconds())})
	return ""
}

func clearBrowserSession(w http.ResponseWriter, r *http.Request, c *config.Config) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: cookieSecure(r, c), SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

// 自定义请求头触发跨站预检；本服务不开放 CORS。Origin 和 Fetch Metadata 再次收紧来源。
func validCookieRequest(r *http.Request) bool {
	_, err := r.Cookie(sessionCookie)
	browser := r.Header.Get("X-AIE-Browser") == "1"
	if err != nil && !browser {
		return true
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, e := url.Parse(origin)
		if e != nil || (u.Scheme != "https" && u.Scheme != "http") || !strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	return r.Method == "GET" || r.Method == "HEAD" || browser
}
