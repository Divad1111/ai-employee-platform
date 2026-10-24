package infrastructure

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSHCredential 只在保险箱和短时连接过程中持有明文。
type SSHCredential struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

// Remote 允许使用不同执行器，也便于在测试中避免连接真实机器。
type Remote interface {
	Connect(context.Context, Host, string) (RemoteSession, error)
	Probe(context.Context, Host) (string, error)
}
type RemoteSession interface {
	Run(context.Context, string) (string, error)
	Upload(context.Context, string, io.Reader) error
	Close() error
}

type SSHRemote struct {
	Resolve func(context.Context, string, string, string) (string, error)
}
type sshSession struct {
	client *ssh.Client
	redact []string
}

func dialSSH(ctx context.Context, h Host, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	dialer := net.Dialer{Timeout: 15 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", hostAddress(h))
	if err != nil {
		return nil, fmt.Errorf("SSH 网络连接失败")
	}
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	cancelled := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-cancelled:
		}
	}()
	c, ch, reqs, err := ssh.NewClientConn(conn, hostAddress(h), cfg)
	close(cancelled)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH 握手失败，请检查主机指纹、账号和凭证")
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, ch, reqs), nil
}

// Probe 仅探测公开主机指纹，不向未验证主机发送密码或私钥认证。
func (s *SSHRemote) Probe(ctx context.Context, h Host) (string, error) {
	var fingerprint string
	cfg := &ssh.ClientConfig{User: "fingerprint-probe", HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		fingerprint = ssh.FingerprintSHA256(k)
		return fmt.Errorf("probe complete")
	}}
	_, _ = dialSSH(ctx, h, cfg)
	if fingerprint == "" {
		return "", fmt.Errorf("无法探测 SSH 指纹，请检查地址和端口")
	}
	return fingerprint, nil
}

func (s *SSHRemote) Connect(ctx context.Context, h Host, actor string) (RemoteSession, error) {
	if h.HostKey == "" {
		return nil, fmt.Errorf("请先探测并核对 SSH 主机指纹")
	}
	raw, err := s.Resolve(ctx, h.SecretID, actor, "ssh.connect")
	if err != nil {
		return nil, err
	}
	var c SSHCredential
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, fmt.Errorf("SSH 凭证格式无效")
	}
	auth := []ssh.AuthMethod{}
	if c.Password != "" {
		auth = append(auth, ssh.Password(c.Password))
	}
	if c.PrivateKey != "" {
		var signer ssh.Signer
		if c.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(c.PrivateKey), []byte(c.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(c.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("SSH 私钥无法解析")
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("SSH 凭证为空")
	}
	cfg := &ssh.ClientConfig{User: h.SSHUser, Auth: auth, HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		if subtle.ConstantTimeCompare([]byte(ssh.FingerprintSHA256(k)), []byte(h.HostKey)) != 1 {
			return fmt.Errorf("SSH 指纹不匹配")
		}
		return nil
	}}
	client, err := dialSSH(ctx, h, cfg)
	if err != nil {
		return nil, err
	}
	return &sshSession{client: client, redact: []string{raw, c.Password, c.PrivateKey, c.Passphrase}}, nil
}

// limitedBuffer 截断超量输出但保持写入成功，避免远端进程因日志上限阻塞。
type limitedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if b.b.Len() < 32768 {
		left := 32768 - b.b.Len()
		if len(p) > left {
			p = p[:left]
		}
		_, _ = b.b.Write(p)
	}
	return n, nil
}
func (b *limitedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }
func redact(value string, secrets ...string) string {
	for _, v := range secrets {
		if v != "" {
			value = strings.ReplaceAll(value, v, "[已脱敏]")
		}
	}
	return value
}

func (s *sshSession) Run(ctx context.Context, script string) (string, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	var output limitedBuffer
	sess.Stdout = &output
	sess.Stderr = &output
	sess.Stdin = strings.NewReader(script)
	done := make(chan error, 1)
	go func() { done <- sess.Run("sh -s") }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = sess.Close()
		return redact(output.String(), s.redact...), ctx.Err()
	}
	out := redact(output.String(), s.redact...)
	if err != nil {
		return out, fmt.Errorf("远端命令执行失败")
	}
	return out, nil
}
func (s *sshSession) Upload(ctx context.Context, path string, src io.Reader) error {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.client.Close()
		case <-done:
		}
	}()
	defer close(done)
	client, err := sftp.NewClient(s.client)
	if err != nil {
		return err
	}
	defer client.Close()
	f, err := client.OpenFile(path, 0x1|0x40|0x80)
	if err != nil {
		return fmt.Errorf("无法创建远端安装包")
	}
	_, copyErr := io.Copy(f, src)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
func (s *sshSession) Close() error { return s.client.Close() }

// shellQuote 对每个独立参数做 POSIX 转义，用户输入不直接拼入命令。
func shellQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }

const remoteEnv = "set -eu\nexport PATH=\"$HOME/.local/bin:$PATH\"\nexport AIE_DATA_DIR=\"$HOME/.aie\"\nif [ \"$(id -u)\" -ne 0 ]; then export XDG_RUNTIME_DIR=\"/run/user/$(id -u)\"; fi\n"

type terminalTicket struct {
	HostID  string
	Actor   string
	Expires time.Time
}

// TerminalTicket 返回短时、一次性连接票据，URL 中不携带管理员会话令牌。
func (s *Service) TerminalTicket(ctx context.Context, host, actor string) (string, error) {
	h, err := s.Host(ctx, host)
	if err != nil {
		return "", err
	}
	if h.HostKey == "" || h.SecretID == "" {
		return "", fmt.Errorf("请配置 SSH 凭证并核对主机指纹")
	}
	ticket := newID()
	s.ticketMu.Lock()
	defer s.ticketMu.Unlock()
	for id, t := range s.tickets {
		if time.Now().After(t.Expires) {
			delete(s.tickets, id)
		}
	}
	s.tickets[ticket] = terminalTicket{HostID: host, Actor: actor, Expires: time.Now().Add(30 * time.Second)}
	return ticket, nil
}

// OpenTerminal 消耗票据并返回 SSH PTY；调用方负责关闭会话和网络连接。
func (s *Service) OpenTerminal(ctx context.Context, ticket string) (*ssh.Client, string, error) {
	s.ticketMu.Lock()
	t, ok := s.tickets[ticket]
	delete(s.tickets, ticket)
	s.ticketMu.Unlock()
	if !ok || time.Now().After(t.Expires) {
		return nil, "", fmt.Errorf("终端票据无效或已过期")
	}
	h, err := s.Host(ctx, t.HostID)
	if err != nil {
		return nil, "", err
	}
	remote, err := s.Remote.Connect(ctx, h, t.Actor)
	if err != nil {
		return nil, "", err
	}
	session, ok := remote.(*sshSession)
	if !ok {
		_ = remote.Close()
		return nil, "", fmt.Errorf("当前执行器不支持终端")
	}
	return session.client, t.Actor, nil
}
