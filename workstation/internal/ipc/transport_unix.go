//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// DefaultSocket 默认 UDS 路径。
func defaultSocketPath() string {
	dir := os.TempDir()
	if u := os.Getenv("USER"); u != "" {
		return filepath.Join(dir, fmt.Sprintf("aie-aew-%s.sock", u))
	}
	return filepath.Join(dir, "aie-aew.sock")
}

// UnixTransport Unix Domain Socket。
// 权限：socket 文件 0600，仅属主可连。
type UnixTransport struct {
	Path string
}

func (t *UnixTransport) Addr() string {
	if t.Path == "" {
		return defaultSocketPath()
	}
	return t.Path
}

func (t *UnixTransport) Listen(ctx context.Context) (net.Listener, error) {
	path := t.Addr()
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(path)
	}()
	return ln, nil
}

func (t *UnixTransport) Dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", t.Addr())
}

// DefaultTransport 返回平台默认传输。
func DefaultTransport() Transport {
	return &UnixTransport{}
}
