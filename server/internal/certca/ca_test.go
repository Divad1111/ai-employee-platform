package certca

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"sync"
	"testing"
)

type mockCertStore struct {
	mu      sync.Mutex
	records []*Record
}

func (m *mockCertStore) SaveRecord(_ context.Context, rec *Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rec)
	return nil
}

func (m *mockCertStore) ListRecords(_ context.Context) ([]*Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Record, len(m.records))
	copy(out, m.records)
	return out, nil
}

func TestAuthority_SelfHealingAndStore(t *testing.T) {
	tmpDir := t.TempDir()
	ca, err := LoadOrNewAuthority(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrNewAuthority: %v", err)
	}

	mockStore := &mockCertStore{}
	ca.SetStore(mockStore)

	// 1. 生成客户端密钥与 CSR
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "WS-test-node-1"},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		t.Fatalf("CreateCertificateRequest: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	rec, err := ca.SignCSR("WS-test-node-1", csrPEM, 30)
	if err != nil {
		t.Fatalf("SignCSR: %v", err)
	}
	if rec == nil {
		t.Fatalf("expected rec not nil")
	}

	// 确认 FindByWorkstation 查得到
	found := ca.FindByWorkstation("WS-test-node-1")
	if found == nil {
		t.Fatalf("expected FindByWorkstation to find record")
	}
	if found.Status != "ACTIVE" {
		t.Errorf("expected status ACTIVE, got %s", found.Status)
	}

	// 2. 模拟容器重启：新建 CA 实例（仅从文件读取 CA 根证书，内存无客户端记录）
	ca2, err := LoadOrNewAuthority(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrNewAuthority 2: %v", err)
	}
	if r := ca2.FindByWorkstation("WS-test-node-1"); r != nil {
		t.Fatalf("ca2 should have empty memory cache initially")
	}

	// 解码客户端证书 DER
	pBlock, _ := pem.Decode([]byte(rec.CertPEM))
	if pBlock == nil {
		t.Fatalf("failed to decode client cert PEM")
	}

	// 模拟 mTLS 握手触发 VerifyClientRaw
	mockStore2 := &mockCertStore{}
	ca2.SetStore(mockStore2)

	cn, fp, err := ca2.VerifyClientRaw(pBlock.Bytes)
	if err != nil {
		t.Fatalf("VerifyClientRaw failed: %v", err)
	}
	if cn != "WS-test-node-1" {
		t.Errorf("expected cn WS-test-node-1, got %s", cn)
	}
	if fp == "" {
		t.Errorf("expected non-empty fingerprint")
	}

	// 验证自愈恢复成功
	rec2 := ca2.FindByWorkstation("WS-test-node-1")
	if rec2 == nil {
		t.Fatalf("expected FindByWorkstation to recover record after VerifyClientRaw")
	}
	if rec2.Fingerprint != fp {
		t.Errorf("expected fingerprint %s, got %s", fp, rec2.Fingerprint)
	}
	if rec2.Status != "ACTIVE" {
		t.Errorf("expected status ACTIVE, got %s", rec2.Status)
	}
}
