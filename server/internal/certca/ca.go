// Package certca 管理 Control Plane 内部 CA：签发与吊销 Workstation 客户端证书。
// 设计依据：设计文档 §19、§20、§126。
package certca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 错误定义。
var (
	ErrRevoked     = errors.New("证书已吊销")
	ErrNotFound    = errors.New("证书不存在")
	ErrInvalidCSR  = errors.New("无效的 CSR")
)

// CertificateStore 证书持久化存储接口。
type CertificateStore interface {
	SaveRecord(ctx context.Context, r *Record) error
	ListRecords(ctx context.Context) ([]*Record, error)
}

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
	store    CertificateStore
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

// LoadOrNewAuthority 从指定目录加载已有 CA 证书与私钥；若不存在则自动生成并持久化存储。
func LoadOrNewAuthority(dir string) (*Authority, error) {
	if dir == "" {
		return NewDevAuthority()
	}
	certPath := filepath.Join(dir, "ca.crt")
	keyPath := filepath.Join(dir, "ca.key")

	certPEM, errCert := os.ReadFile(certPath)
	keyPEM, errKey := os.ReadFile(keyPath)
	if errCert == nil && errKey == nil && len(certPEM) > 0 && len(keyPEM) > 0 {
		blockCert, _ := pem.Decode(certPEM)
		if blockCert == nil {
			return nil, fmt.Errorf("解析 ca.crt 失败: 无有效 PEM 数据")
		}
		cert, err := x509.ParseCertificate(blockCert.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析 CA 证书失败: %w", err)
		}

		blockKey, _ := pem.Decode(keyPEM)
		if blockKey == nil {
			return nil, fmt.Errorf("解析 ca.key 失败: 无有效 PEM 数据")
		}
		var key *ecdsa.PrivateKey
		if blockKey.Type == "EC PRIVATE KEY" {
			key, err = x509.ParseECPrivateKey(blockKey.Bytes)
		} else {
			parsedKey, errPkcs8 := x509.ParsePKCS8PrivateKey(blockKey.Bytes)
			if errPkcs8 == nil {
				var ok bool
				key, ok = parsedKey.(*ecdsa.PrivateKey)
				if !ok {
					err = fmt.Errorf("CA 私钥非 ECDSA 类型")
				}
			} else {
				err = errPkcs8
			}
		}
		if err != nil {
			return nil, fmt.Errorf("解析 CA 私钥失败: %w", err)
		}

		return &Authority{
			caKey:     key,
			caCert:    cert,
			caCertPEM: certPEM,
			caKeyPEM:  keyPEM,
			byFP:      map[string]*Record{},
			byWS:      map[string]string{},
		}, nil
	}

	// 目录不存在或文件不完整，新建并持久化
	auth, err := NewDevAuthority()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("创建 CA 存储目录失败: %w", err)
	}
	if err := os.WriteFile(certPath, auth.caCertPEM, 0644); err != nil {
		return nil, fmt.Errorf("持久化 ca.crt 失败: %w", err)
	}
	if err := os.WriteFile(keyPath, auth.caKeyPEM, 0600); err != nil {
		return nil, fmt.Errorf("持久化 ca.key 失败: %w", err)
	}
	return auth, nil
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
	a.byFP[fp] = rec
	a.byWS[workstationID] = fp
	st := a.store
	a.mu.Unlock()
	if st != nil {
		_ = st.SaveRecord(context.Background(), rec)
	}
	return rec, nil
}

// SetStore 设置证书持久化存储，并从存储中预加载已有证书记录。
func (a *Authority) SetStore(store CertificateStore) {
	a.mu.Lock()
	a.store = store
	a.mu.Unlock()
	if store != nil {
		if records, err := store.ListRecords(context.Background()); err == nil {
			a.mu.Lock()
			for _, r := range records {
				a.byFP[r.Fingerprint] = r
				if r.Status != "REVOKED" || a.byWS[r.WorkstationID] == "" {
					a.byWS[r.WorkstationID] = r.Fingerprint
				}
			}
			a.mu.Unlock()
		}
	}
}

// Revoke 吊销证书。
func (a *Authority) Revoke(fingerprint string) error {
	a.mu.Lock()
	rec := a.byFP[fingerprint]
	if rec == nil {
		a.mu.Unlock()
		return ErrNotFound
	}
	rec.Status = "REVOKED"
	rec.RevokedAt = time.Now().UTC()
	st := a.store
	a.mu.Unlock()
	if st != nil {
		_ = st.SaveRecord(context.Background(), rec)
	}
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

// DeleteWorkstation 吊销并删除指定 Workstation 的全部证书记录。
func (a *Authority) DeleteWorkstation(workstationID string) error {
	a.mu.Lock()
	fp, ok := a.byWS[workstationID]
	if ok {
		if rec := a.byFP[fp]; rec != nil {
			rec.Status = "REVOKED"
			rec.RevokedAt = time.Now().UTC()
			if a.store != nil {
				_ = a.store.SaveRecord(context.Background(), rec)
			}
		}
		delete(a.byWS, workstationID)
	}
	a.mu.Unlock()
	return nil
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
// 验签通过后若内存或存储中缺少该证书，自动恢复登记为有效证书记录（自愈机制）。
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
	workstationID = cert.Subject.CommonName

	a.mu.Lock()
	rec, exists := a.byFP[fp]
	if !exists {
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
		rec = &Record{
			WorkstationID: workstationID,
			Fingerprint:   fp,
			CertPEM:       string(certPEM),
			Status:        "ACTIVE",
			IssuedAt:      cert.NotBefore.UTC(),
			ExpiresAt:     cert.NotAfter.UTC(),
		}
		a.byFP[fp] = rec
		a.byWS[workstationID] = fp
		st := a.store
		a.mu.Unlock()
		if st != nil {
			_ = st.SaveRecord(context.Background(), rec)
		}
	} else {
		if a.byWS[workstationID] == "" {
			a.byWS[workstationID] = fp
		}
		a.mu.Unlock()
	}

	return workstationID, fp, nil
}

func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
