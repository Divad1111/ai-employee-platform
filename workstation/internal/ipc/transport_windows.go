//go:build windows

package ipc

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ai-employee-platform/workstation/internal/platform"
)

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

type tokenListener struct {
	net.Listener
	token string
}

func (l *tokenListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		br := bufio.NewReader(conn)
		line, err := br.ReadString('\n')
		if err != nil || strings.TrimSpace(line) != l.token {
			_ = conn.Close()
			continue
		}
		_ = conn.SetDeadline(time.Time{})
		return &bufferedConn{Conn: conn, r: br}, nil
	}
}

// LoopbackTransport Windows：127.0.0.1 TCP + 端口文件（仅本机）。
// 权限说明：监听绑定 loopback 并生成随机会话 Token；端口文件写在用户数据目录，ACL 由 OS 目录权限约束。
// （后续可升级为 Named Pipe + 显式 SDDL。）
type LoopbackTransport struct {
	EndpointFile string
}

func defaultEndpointFile() string {
	// 与 platform.Detect() 对齐，避免 CLI 读 ProgramData、Daemon 写 ~/.aie 导致假 OFFLINE
	if root := os.Getenv("AIE_DATA_DIR"); root != "" {
		return filepath.Join(root, "aew.ipc")
	}
	p := platform.Detect()
	// 用户态 ~/.aie：ipc 放在根目录（与历史上已写入的 aew.ipc 兼容）
	// 系统态 ProgramData/.../data：ipc 放在 DataDir 内
	data := p.DataDir()
	if filepath.Base(data) == "data" {
		parent := filepath.Dir(data)
		if filepath.Base(parent) == ".aie" {
			return filepath.Join(parent, "aew.ipc")
		}
	}
	return filepath.Join(data, "aew.ipc")
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
	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)
	data := fmt.Sprintf("%s|%s", addr, token)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(path)
	}()
	return &tokenListener{Listener: ln, token: token}, nil
}

func (t *LoopbackTransport) Dial(ctx context.Context) (net.Conn, error) {
	b, err := os.ReadFile(t.Addr())
	if err != nil {
		return nil, fmt.Errorf("读取 IPC 端点失败（Daemon 未启动？）: %w", err)
	}
	content := strings.TrimSpace(string(b))
	var addr, token string
	if strings.Contains(content, "|") {
		parts := strings.SplitN(content, "|", 2)
		addr = strings.TrimSpace(parts[0])
		token = strings.TrimSpace(parts[1])
	} else {
		addr = content
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(addr[strings.LastIndex(addr, ":")+1:], "")); err != nil && !strings.Contains(addr, ":") {
		return nil, fmt.Errorf("无效端点: %s", addr)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if token != "" {
		if _, err := fmt.Fprintf(conn, "%s\n", token); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("IPC 握手失败: %w", err)
		}
	}
	return conn, nil
}

// DefaultTransport 返回平台默认传输。
func DefaultTransport() Transport {
	return &LoopbackTransport{}
}
