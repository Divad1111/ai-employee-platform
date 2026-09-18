//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LoopbackTransport Windows：127.0.0.1 TCP + 端口文件（仅本机）。
// 权限说明：监听绑定 loopback；端口文件写在用户数据目录，ACL 由 OS 目录权限约束。
// （后续可升级为 Named Pipe + 显式 SDDL。）
type LoopbackTransport struct {
	EndpointFile string
}

func defaultEndpointFile() string {
	base := os.Getenv("AIE_DATA_DIR")
	if base == "" {
		base = filepath.Join(os.Getenv("ProgramData"), "AIEmployee", "data")
	}
	return filepath.Join(base, "aew.ipc")
}

func (t *LoopbackTransport) Addr() string {
	if t.EndpointFile == "" {
		return defaultEndpointFile()
	}
	return t.EndpointFile
}

func (t *LoopbackTransport) Listen(ctx context.Context) (net.Listener, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	path := t.Addr()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		_ = ln.Close()
		return nil, err
	}
	addr := ln.Addr().String()
	if err := os.WriteFile(path, []byte(addr), 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(path)
	}()
	return ln, nil
}

func (t *LoopbackTransport) Dial(ctx context.Context) (net.Conn, error) {
	b, err := os.ReadFile(t.Addr())
	if err != nil {
		return nil, fmt.Errorf("读取 IPC 端点失败（Daemon 未启动？）: %w", err)
	}
	addr := strings.TrimSpace(string(b))
	if _, err := strconv.Atoi(strings.TrimPrefix(addr[strings.LastIndex(addr, ":")+1:], "")); err != nil && !strings.Contains(addr, ":") {
		return nil, fmt.Errorf("无效端点: %s", addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

// DefaultTransport 返回平台默认传输。
func DefaultTransport() Transport {
	return &LoopbackTransport{}
}
