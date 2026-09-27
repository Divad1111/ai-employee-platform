// Package grpcclient 是 Workstation → Control Plane 的 gRPC 出站客户端。
// 设计依据：设计文档 §20、§64。
package grpcclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Client 封装 mTLS gRPC 连接。
type Client struct {
	conn   *grpc.ClientConn
	worker aiev1.WorkerServiceClient
	wsID   string
}

// Dial 使用本地身份主动连接 Control Plane。
func Dial(ctx context.Context, endpoint string, b *identity.Bundle) (*Client, error) {
	if len(b.CertPEM) == 0 || len(b.KeyPEM) == 0 || len(b.CAPEM) == 0 {
		return nil, fmt.Errorf("本地身份不完整，请先 aew register")
	}
	cert, err := tls.X509KeyPair(b.CertPEM, b.KeyPEM)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(b.CAPEM) {
		return nil, fmt.Errorf("解析 CA 失败")
	}
	host := endpoint
	if h, _, err := net.SplitHostPort(endpoint); err == nil {
		host = h
	}
	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   host,
	}
	creds := credentials.NewTLS(tlsCfg)
	conn, err := grpc.DialContext(ctx, endpoint, grpc.WithTransportCredentials(creds), grpc.WithBlock())
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, worker: aiev1.NewWorkerServiceClient(conn), wsID: b.WorkstationID}, nil
}

// Ping 往返探测。
func (c *Client) Ping(ctx context.Context, nonce string) (*aiev1.PingResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return c.worker.Ping(ctx, &aiev1.PingRequest{WorkstationId: c.wsID, Nonce: nonce})
}

// Close 关闭连接。
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// WorkstationID 返回证书对应 ID。
func (c *Client) WorkstationID() string { return c.wsID }
