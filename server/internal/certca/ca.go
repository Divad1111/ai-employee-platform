// Package certca 管理 Control Plane 内部 CA：签发与吊销 Workstation 客户端证书。
// 设计依据：设计文档 §19、§20、§126。
package certca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"sync"
	"time"
)

// 错误定义。
var (
	ErrRevoked     = errors.New("证书已吊销")
	ErrNotFound    = errors.New("证书不存在")
	ErrInvalidCSR  = errors.New("无效的 CSR")
)

// Record 已签发证书记录。
type Record struct {
	WorkstationID string
	Fingerprint   string
	CertPEM       string
	Status        string // ACTIVE / REVOKED / EXPIRED
	IssuedAt      time.Time
	ExpiresAt     time.Time
	RevokedAt     time.Time
}

// Authority 简易 CA。
type Authority struct {
	mu       sync.RWMutex
	caKey    *ecdsa.PrivateKey
	caCert   *x509.Certificate
	caCertPEM []byte
	caKeyPEM  []byte
	byFP     map[string]*Record
	byWS     map[string]string // workstation -> fingerprint
}

// NewDevAuthority 生成开发用自签 CA。
func NewDevAuthority() (*Authority, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "AI Employee Dev CA", Organization: []string{"AI Employee"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return &Authority{
		caKey:     key,
		caCert:    cert,
		caCertPEM: certPEM,
		caKeyPEM:  keyPEM,
		byFP:      map[string]*Record{},
		byWS:      map[string]string{},
	}, nil
}

// CAPEM 返回 CA 证书 PEM。
func (a *Authority) CAPEM() []byte {
	return append([]byte{}, a.caCertPEM...)
}

// SignServerCertificate 签发 gRPC/HTTPS 服务端证书（开发用）。
func (a *Authority) SignServerCertificate(commonName string, hosts []string, validDays int) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	if validDays <= 0 {
		validDays = 365
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Duration(validDays) * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     hosts,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.caCert, &key.PublicKey, a.caKey)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// SignCSR 使用 CA 签发客户端证书（ENROLL → ACTIVE）。
func (a *Authority) SignCSR(workstationID string, csrPEM []byte, validDays int) (*Record, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, ErrInvalidCSR
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, ErrInvalidCSR
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, ErrInvalidCSR
	}
	if validDays <= 0 {
		validDays = 365
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: workstationID, OrganizationalUnit: []string{"workstation"}},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Duration(validDays) * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.caCert, csr.PublicKey, a.caKey)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	fp := fingerprint(der)
	rec := &Record{
		WorkstationID: workstationID,
		Fingerprint:   fp,
		CertPEM:       string(certPEM),
		Status:        "ACTIVE",
		IssuedAt:      time.Now().UTC(),
		ExpiresAt:     tmpl.NotAfter.UTC(),
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.byFP[fp] = rec
	a.byWS[workstationID] = fp
	return rec, nil
}

// Revoke 吊销证书。
func (a *Authority) Revoke(fingerprint string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	rec := a.byFP[fingerprint]
	if rec == nil {
		return ErrNotFound
	}
	rec.Status = "REVOKED"
	rec.RevokedAt = time.Now().UTC()
	return nil
}

// IsRevoked 判断指纹是否已吊销。
func (a *Authority) IsRevoked(fingerprint string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	rec := a.byFP[fingerprint]
	return rec != nil && rec.Status == "REVOKED"
}

// FindByWorkstation 按 Workstation 查找证书记录。
func (a *Authority) FindByWorkstation(id string) *Record {
	a.mu.RLock()
	defer a.mu.RUnlock()
	fp := a.byWS[id]
	if fp == "" {
		return nil
	}
	return a.byFP[fp]
}

// ListRecords 返回全部证书记录副本。
func (a *Authority) ListRecords() []Record {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]Record, 0, len(a.byFP))
	for _, r := range a.byFP {
		out = append(out, *r)
	}
	return out
}

// VerifyClientRaw 校验客户端证书 DER：由本 CA 签发且未吊销。
func (a *Authority) VerifyClientRaw(raw []byte) (workstationID string, fp string, err error) {
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		return "", "", err
	}
	roots := x509.NewCertPool()
	roots.AddCert(a.caCert)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return "", "", err
	}
	fp = fingerprint(raw)
	if a.IsRevoked(fp) {
		return "", fp, ErrRevoked
	}
	return cert.Subject.CommonName, fp, nil
}

func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
