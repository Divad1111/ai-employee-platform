// Package totp 实现 RFC 6238 TOTP（CRITICAL 审批二次因子）。
// 设计依据：设计文档 §30、§78。
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// GenerateSecret 生成 Base32 密钥（16 字节 → 26 字符左右）。
func GenerateSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// Verify 校验 6 位码；允许 ±1 时间窗。
func Verify(secret, code string, now time.Time) bool {
	secret = strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	counter := now.Unix() / 30
	for _, d := range []int64{-1, 0, 1} {
		if hotp(key, uint64(counter+d)) == code {
			return true
		}
	}
	return false
}

func hotp(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", truncated%1000000)
}

// CodeAt 测试辅助：生成指定时刻码。
func CodeAt(secret string, now time.Time) (string, error) {
	secret = strings.ToUpper(strings.ReplaceAll(secret, " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return "", err
	}
	return hotp(key, uint64(now.Unix()/30)), nil
}
