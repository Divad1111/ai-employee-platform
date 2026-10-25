package api

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/ai-employee-platform/server/internal/config"
)

type addressKey struct{}

func SecureHTTP(next http.Handler, cfg *config.Config) http.Handler {
	slots := make(chan struct{}, 32)
	large := make(chan struct{}, 2)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		effective := host
		trusted := false
		if cfg != nil {
			for _, network := range cfg.TrustedProxies {
				if network.Contains(ip) {
					trusted = true
					break
				}
			}
		}
		if trusted {
			// Walk right to left, stopping at the first untrusted hop.
			forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
			for i := len(forwarded) - 1; i >= 0; i-- {
				candidate := net.ParseIP(strings.TrimSpace(forwarded[i]))
				if candidate == nil {
					continue
				}
				effective = candidate.String()
				hopTrusted := false
				for _, network := range cfg.TrustedProxies {
					if network.Contains(candidate) {
						hopTrusted = true
						break
					}
				}
				if !hopTrusted {
					break
				}
			}
		}
		if !trusted {
			r.Header.Del("X-Real-IP")
			r.Header.Del("X-Forwarded-For")
			r.Header.Del("X-Forwarded-Proto")
		}
		r = r.WithContext(context.WithValue(r.Context(), addressKey{}, effective))
		maxBody := int64(2 << 20)
		path := r.URL.Path
		switch {
		case path == "/api/infrastructure/releases":
			maxBody = 514 << 20
		case path == "/mcp":
			maxBody = 8 << 20
		case path == "/api/workflow-mcp/import":
			maxBody = 32 << 20
		case path == "/api/artifacts" || strings.HasPrefix(path, "/api/integrations/workstation/artifacts") || strings.Contains(path, "/attachments"):
			maxBody = 128 << 20
		case strings.HasPrefix(path, "/api/integrations/feishu"):
			maxBody = 1 << 20
		}
		if maxBody > 8<<20 && r.Method != "GET" {
			select {
			case large <- struct{}{}:
				defer func() { <-large }()
			default:
				http.Error(w, "large upload capacity exhausted", 429)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if r.Method != "GET" && r.Method != "HEAD" {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				w.Header().Set("Retry-After", "2")
				http.Error(w, "too many requests", 429)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if r.TLS != nil || trusted && r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(path, "/api/auth/") || strings.HasPrefix(path, "/api/setup/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
