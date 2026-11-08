package automation

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// webhookLimiter 简单滑动窗口限流（按 path_token）。
type webhookLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newWebhookLimiter() *webhookLimiter {
	return &webhookLimiter{hits: map[string][]time.Time{}}
}

func (l *webhookLimiter) allow(key string, limitPerMin int, now time.Time) bool {
	if limitPerMin <= 0 {
		limitPerMin = 60
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	windowStart := now.Add(-time.Minute)
	arr := l.hits[key]
	n := 0
	for _, t := range arr {
		if t.After(windowStart) {
			arr[n] = t
			n++
		}
	}
	arr = arr[:n]
	if len(arr) >= limitPerMin {
		l.hits[key] = arr
		return false
	}
	arr = append(arr, now)
	l.hits[key] = arr
	return true
}

// clientIP only uses the address resolved by the trusted HTTP adapter.
func clientIP(remoteAddr, _ string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func ipAllowed(ip string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return true
	}
	ip = strings.TrimSpace(ip)
	for _, a := range allowlist {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if a == ip {
			return true
		}
		if _, cidr, err := net.ParseCIDR(a); err == nil {
			if parsed := net.ParseIP(ip); parsed != nil && cidr.Contains(parsed) {
				return true
			}
		}
	}
	return false
}

func hasAuthMode(modes []string, want string) bool {
	for _, m := range modes {
		if strings.EqualFold(strings.TrimSpace(m), want) {
			return true
		}
	}
	return false
}

// verifyHMAC 校验 X-AIE-Signature = hex(hmac_sha256(secret, timestamp+"."+body))
func verifyHMAC(secret, timestamp, signature, body string, now time.Time, skew time.Duration) error {
	if secret == "" || signature == "" || timestamp == "" {
		return ErrForbiddenWebhook
	}
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		// 也接受 unix 秒
		var unix int64
		if _, e2 := fmtSscanf(timestamp, &unix); e2 != nil {
			return ErrForbiddenWebhook
		}
		ts = time.Unix(unix, 0).UTC()
	}
	if skew <= 0 {
		skew = 5 * time.Minute
	}
	diff := now.Sub(ts)
	if diff < 0 {
		diff = -diff
	}
	if diff > skew {
		return ErrForbiddenWebhook
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + body))
	expect := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(strings.ToLower(expect)), []byte(strings.ToLower(strings.TrimSpace(signature)))) {
		return ErrForbiddenWebhook
	}
	return nil
}

func verifyBearer(expected, header string) error {
	if expected == "" {
		return ErrForbiddenWebhook
	}
	h := strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		h = strings.TrimSpace(h[7:])
	}
	if h == "" || !hmac.Equal([]byte(h), []byte(expected)) {
		return ErrForbiddenWebhook
	}
	return nil
}

func fmtSscanf(s string, unix *int64) (int, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, ErrForbiddenWebhook
		}
		n = n*10 + int64(c-'0')
	}
	*unix = n
	return 1, nil
}

func computeHMAC(secret, timestamp, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + body))
	return hex.EncodeToString(mac.Sum(nil))
}

// ErrWebhookReplay rejects a previously authenticated request.
var ErrWebhookReplay = errors.New("webhook request already used")

func (s *Service) claimWebhook(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if s.ClaimReplay != nil {
		return s.ClaimReplay(ctx, key, ttl)
	}
	s.replayMu.Lock()
	defer s.replayMu.Unlock()
	now := time.Now()
	for k, expires := range s.replays {
		if !expires.After(now) {
			delete(s.replays, k)
		}
	}
	if _, exists := s.replays[key]; exists {
		return false, nil
	}
	// Fail closed rather than evict live replay protection under load.
	if len(s.replays) >= 10000 {
		return false, ErrRateLimited
	}
	if s.replays == nil {
		s.replays = make(map[string]time.Time)
	}
	s.replays[key] = now.Add(ttl)
	return true, nil
}
