// Package idgen 生成带前缀的业务 ID。
package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New 返回 prefix-随机十六进制。
func New(prefix string) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b[:]))
}

// Raw 返回无前缀随机 ID。
func Raw() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
