package workergrpc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/workergrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func TestMTLSPingAndRevoke(t *testing.T) {
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	srvCert, err := workergrpc.LoadServerCertificate(ca, "localhost", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	gs, lis, _, err := workergrpc.ListenAndServe("127.0.0.1:0", ca, srvCert)
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Stop()

	wsID := "WS-TEST-1"
	certPEM, keyPEM := mustClientCreds(t, ca, wsID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli := dial(t, ctx, lis.Addr().String(), certPEM, keyPEM, ca.CAPEM())
	defer cli.Close()
	worker := aiev1.NewWorkerServiceClient(cli)
	resp, err := worker.Ping(ctx, &aiev1.PingRequest{WorkstationId: wsID, Nonce: "n1"})
	if err != nil {
		t.Fatalf("合法证书 Ping 失败: %v", err)
	}
	if resp.Nonce != "n1" {
		t.Fatalf("nonce 不匹配")
	}

	rec := ca.FindByWorkstation(wsID)
	if rec == nil {
		t.Fatal("缺少证书记录")
	}
	if err := ca.Revoke(rec.Fingerprint); err != nil {
		t.Fatal(err)
	}
	cli2, err := grpc.DialContext(ctx, lis.Addr().String(), grpc.WithTransportCredentials(clientCreds(t, certPEM, keyPEM, ca.CAPEM())), grpc.WithBlock())
	if err == nil {
		_, err = aiev1.NewWorkerServiceClient(cli2).Ping(ctx, &aiev1.PingRequest{WorkstationId: wsID, Nonce: "n2"})
		_ = cli2.Close()
	}
	if err == nil {
		t.Fatal("吊销后应无法 Ping")
	}
}

func mustClientCreds(t *testing.T, ca *certca.Authority, wsID string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: wsID},
	}, key)
	if err != nil {
		t.Fatal(err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	rec, err := ca.SignCSR(wsID, csrPEM, 30)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	return []byte(rec.CertPEM), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func clientCreds(t *testing.T, certPEM, keyPEM, caPEM []byte) credentials.TransportCredentials {
	t.Helper()
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	return credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   "localhost",
	})
}

func dial(t *testing.T, ctx context.Context, addr string, certPEM, keyPEM, caPEM []byte) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(clientCreds(t, certPEM, keyPEM, caPEM)), grpc.WithBlock())
	if err != nil {
		t.Fatal(err)
	}
	return conn
}
