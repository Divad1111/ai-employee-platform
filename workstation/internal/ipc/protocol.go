// Package ipc 提供 CLI ↔ Daemon 本地 IPC。
// Windows：Named Pipe；Unix：UDS。仅本机进程可连。
// 设计依据：设计文档 §44、§122。
package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
)

// Request IPC 请求。
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response IPC 响应。
type Response struct {
	OK     bool            `json:"ok"`
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

// Handler 处理请求。
type Handler func(ctx context.Context, req Request) Response

// Transport 本地传输。
type Transport interface {
	Listen(ctx context.Context) (net.Listener, error)
	Dial(ctx context.Context) (net.Conn, error)
	Addr() string
}

// Server IPC 服务端（Daemon 侧）。
type Server struct {
	Transport Transport
	Handler   Handler
}

// Serve 接受连接并处理换行分隔 JSON。
func (s *Server) Serve(ctx context.Context) error {
	ln, err := s.Transport.Listen(ctx)
	if err != nil {
		return err
	}
	defer ln.Close()
	var wg sync.WaitGroup
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				wg.Wait()
				return nil
			default:
				return err
			}
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			defer c.Close()
			s.serveConn(ctx, c)
		}(conn)
	}
}

func (s *Server) serveConn(ctx context.Context, c net.Conn) {
	rd := bufio.NewReader(c)
	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			writeResp(c, Response{OK: false, Error: "无效 JSON"})
			continue
		}
		resp := s.Handler(ctx, req)
		writeResp(c, resp)
	}
}

func writeResp(c net.Conn, resp Response) {
	b, _ := json.Marshal(resp)
	_, _ = c.Write(append(b, '\n'))
}

// Client CLI 侧。
type Client struct {
	Transport Transport
}

// Call 同步调用。
func (c *Client) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	conn, err := c.Transport.Dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("无法连接 Daemon（是否已 aew daemon？）: %w", err)
	}
	defer conn.Close()
	var raw json.RawMessage
	if params != nil {
		raw, _ = json.Marshal(params)
	}
	req := Request{Method: method, Params: raw}
	b, _ := json.Marshal(req)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return resp.Result, nil
}

// MemoryTransport 测试用内存管道。
type MemoryTransport struct {
	mu   sync.Mutex
	ln   *memListener
	addr string
}

func NewMemoryTransport(addr string) *MemoryTransport {
	return &MemoryTransport{addr: addr}
}

func (m *MemoryTransport) Addr() string { return m.addr }

func (m *MemoryTransport) Listen(context.Context) (net.Listener, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ln = newMemListener(m.addr)
	return m.ln, nil
}

func (m *MemoryTransport) Dial(context.Context) (net.Conn, error) {
	m.mu.Lock()
	ln := m.ln
	m.mu.Unlock()
	if ln == nil {
		return nil, fmt.Errorf("未监听")
	}
	return ln.dial()
}

type memListener struct {
	addr    string
	ch      chan net.Conn
	closed  chan struct{}
	closeOnce sync.Once
}

func newMemListener(addr string) *memListener {
	return &memListener{addr: addr, ch: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *memListener) Accept() (net.Conn, error) {
	select {
	case <-l.closed:
		return nil, fmt.Errorf("closed")
	case c := <-l.ch:
		return c, nil
	}
}

func (l *memListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *memListener) Addr() net.Addr { return memAddr(l.addr) }

func (l *memListener) dial() (net.Conn, error) {
	c1, c2 := net.Pipe()
	select {
	case <-l.closed:
		_ = c1.Close()
		_ = c2.Close()
		return nil, fmt.Errorf("closed")
	case l.ch <- c1:
		return c2, nil
	}
}

type memAddr string

func (m memAddr) Network() string { return "memory" }
func (m memAddr) String() string  { return string(m) }
