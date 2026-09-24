// 工作站制品上传签名校验（出站 HTTP + 设备证书，无私钥上传）。
package artifact

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// CanonicalSignPayload 与工作站侧约定的签名原文。
func CanonicalSignPayload(wsID, timestamp, jobID, name, contentSHA256 string, size int64) string {
	return strings.Join([]string{
		"aie-artifact-v1",
		wsID,
		timestamp,
		jobID,
		name,
		contentSHA256,
		strconv.FormatInt(size, 10),
	}, "\n")
}

// VerifyECDSASignatureASN1 校验 ECDSA ASN.1 签名（P-256）。
func VerifyECDSASignatureASN1(pub *ecdsa.PublicKey, payload string, sigHex string) error {
	if pub == nil {
		return errors.New("缺少公钥")
	}
	sigRaw, err := hex.DecodeString(strings.TrimSpace(sigHex))
	if err != nil || len(sigRaw) == 0 {
		return errors.New("无效签名编码")
	}
	var parsed struct {
		R, S *big.Int
	}
	if _, err := asn1.Unmarshal(sigRaw, &parsed); err != nil {
		return errors.New("无效 ASN.1 签名")
	}
	sum := sha256.Sum256([]byte(payload))
	if !ecdsa.Verify(pub, sum[:], parsed.R, parsed.S) {
		return errors.New("签名校验失败")
	}
	return nil
}

// CheckTimestampSkew 时间戳防重放窗口。
func CheckTimestampSkew(ts string, now time.Time, skew time.Duration) error {
	if skew <= 0 {
		skew = 5 * time.Minute
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return fmt.Errorf("无效时间戳")
	}
	delta := now.UTC().Sub(t.UTC())
	if delta < 0 {
		delta = -delta
	}
	if delta > skew {
		return errors.New("时间戳超出允许窗口")
	}
	return nil
}
