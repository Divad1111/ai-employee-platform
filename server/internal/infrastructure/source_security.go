package infrastructure

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"
)

func allowedSoftwareURL(raw string) error {
	if !strings.EqualFold(os.Getenv("AIE_ENV"), "production") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalid
	}
	for _, host := range strings.Split(os.Getenv("AIE_SOFTWARE_ALLOWED_HOSTS"), ",") {
		if strings.TrimSpace(host) != "" && strings.EqualFold(u.Hostname(), strings.TrimSpace(host)) && u.Scheme == "https" && (u.Port() == "" || u.Port() == "443") {
			return nil
		}
	}
	return fmt.Errorf("软件源主机不在 AIE_SOFTWARE_ALLOWED_HOSTS 允许列表")
}

// ReleaseSigningPayload is eight UTF-8 fields separated by LF, independent of JSON formatting.
func ReleaseSigningPayload(r Release) []byte {
	return []byte(strings.Join([]string{"AIE-RELEASE-V1", r.Version, r.OS, r.Arch, r.URL, strings.ToLower(r.SHA256), r.Format, r.Entry}, "\n"))
}
func verifyReleaseSignature(r Release) error {
	if !strings.EqualFold(os.Getenv("AIE_ENV"), "production") {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("AIE_SOFTWARE_SIGNING_KEY"))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("生产远程软件源需要配置 Ed25519 发布公钥")
	}
	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil || !ed25519.Verify(key, ReleaseSigningPayload(r), sig) {
		return fmt.Errorf("远程发布签名缺失或无效；请使用已签名清单，或二次认证后上传经审核的软件包")
	}
	return nil
}
