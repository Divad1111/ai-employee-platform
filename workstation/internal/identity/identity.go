// Package identity 管理 Workstation 本地身份（ID、私钥、证书）。
// 私钥永远不能上传 Control Plane。
// 设计依据：设计文档 §19、§125。
package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ai-employee-platform/workstation/internal/platform"
	"github.com/google/uuid"
)

// Bundle 本地身份材料。
type Bundle struct {
	WorkstationID string
	KeyPEM        []byte
	CSRPEM        []byte
	CertPEM       []byte
	CAPEM         []byte
}

// Generate 生成新的 workstation_id、私钥与 CSR（不上传私钥）。
func Generate() (*Bundle, *ecdsa.PrivateKey, error) {
	id := "WS-" + uuid.NewString()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: id},
	}, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	b := &Bundle{
		WorkstationID: id,
		KeyPEM:        pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		CSRPEM:        pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}),
	}
	return b, key, nil
}

// Save 将身份写入 PlatformPaths.IdentityDir（私钥仅本地）。
func Save(paths platform.Paths, b *Bundle) error {
	dir := paths.IdentityDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	writes := map[string][]byte{
		"workstation-id": []byte(b.WorkstationID),
		"client.key":     b.KeyPEM,
	}
	if len(b.CertPEM) > 0 {
		writes["client.crt"] = b.CertPEM
	}
	if len(b.CAPEM) > 0 {
		writes["ca.crt"] = b.CAPEM
	}
	for name, data := range writes {
		mode := os.FileMode(0o600)
		if name == "workstation-id" || name == "client.crt" || name == "ca.crt" {
			mode = 0o644
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, mode); err != nil {
			return err
		}
	}
	return nil
}

// Load 从本地目录加载身份。
func Load(paths platform.Paths) (*Bundle, error) {
	dir := paths.IdentityDir()
	id, err := os.ReadFile(filepath.Join(dir, "workstation-id"))
	if err != nil {
		return nil, fmt.Errorf("未注册：%w", err)
	}
	key, err := os.ReadFile(filepath.Join(dir, "client.key"))
	if err != nil {
		return nil, err
	}
	b := &Bundle{WorkstationID: string(id), KeyPEM: key}
	if c, err := os.ReadFile(filepath.Join(dir, "client.crt")); err == nil {
		b.CertPEM = c
	}
	if c, err := os.ReadFile(filepath.Join(dir, "ca.crt")); err == nil {
		b.CAPEM = c
	}
	return b, nil
}

// Clear 删除本地身份文件（unregister）。
func Clear(paths platform.Paths) error {
	dir := paths.IdentityDir()
	for _, name := range []string{"workstation-id", "client.key", "client.crt", "ca.crt"} {
		_ = os.Remove(filepath.Join(dir, name))
	}
	return nil
}
