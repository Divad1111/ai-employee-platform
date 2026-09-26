package destination

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPDestination SFTP 远程存储目标
type SFTPDestination struct {
	host       string
	port       int
	username   string
	password   string
	privateKey string
	remotePath string
}

// SFTPDestOptions SFTP 参数
type SFTPDestOptions struct {
	Host       string
	Port       int
	Username   string
	Password   string
	PrivateKey string
	RemotePath string
}

// NewSFTPDestination 初始化 SFTP 目标
func NewSFTPDestination(opts SFTPDestOptions) *SFTPDestination {
	port := opts.Port
	if port <= 0 {
		port = 22
	}
	rPath := opts.RemotePath
	if rPath == "" {
		rPath = "/backup"
	}
	return &SFTPDestination{
		host:       opts.Host,
		port:       port,
		username:   opts.Username,
		password:   opts.Password,
		privateKey: opts.PrivateKey,
		remotePath: rPath,
	}
}

func (s *SFTPDestination) connect() (*ssh.Client, *sftp.Client, error) {
	var authMethods []ssh.AuthMethod
	if s.password != "" {
		authMethods = append(authMethods, ssh.Password(s.password))
	}
	if s.privateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(s.privateKey))
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}
	if len(authMethods) == 0 {
		return nil, nil, errors.New("SFTP 缺少认证凭据（需提供密码或私钥）")
	}

	cfg := &ssh.ClientConfig{
		User:            s.username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 可根据需要配置已知 hostkey
		Timeout:         10 * time.Second,
	}

	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	sshConn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("SSH 连接失败 (%s): %w", addr, err)
	}

	sftpCli, err := sftp.NewClient(sshConn)
	if err != nil {
		_ = sshConn.Close()
		return nil, nil, fmt.Errorf("SFTP 握手失败: %w", err)
	}

	return sshConn, sftpCli, nil
}

func (s *SFTPDestination) Validate(_ context.Context) error {
	if s.host == "" {
		return errors.New("SFTP Host 不能为空")
	}
	if s.username == "" {
		return errors.New("SFTP 用户名不能为空")
	}
	if s.password == "" && s.privateKey == "" {
		return errors.New("SFTP 需提供密码或 SSH 私钥")
	}
	return nil
}

func (s *SFTPDestination) sanitizeRemote(backupID string) string {
	cleaned := filepath.Base(filepath.Clean(backupID))
	if !strings.HasSuffix(cleaned, ".backup") {
		cleaned = cleaned + ".backup"
	}
	return strings.ReplaceAll(filepath.Join(s.remotePath, cleaned), "\\", "/")
}

func (s *SFTPDestination) TestConnection(ctx context.Context) error {
	if err := s.Validate(ctx); err != nil {
		return err
	}
	sshConn, sftpCli, err := s.connect()
	if err != nil {
		return err
	}
	defer sshConn.Close()
	defer sftpCli.Close()

	if err := sftpCli.MkdirAll(s.remotePath); err != nil {
		return fmt.Errorf("SFTP 创建远程目录失败: %w", err)
	}

	testFile := strings.ReplaceAll(filepath.Join(s.remotePath, fmt.Sprintf(".test_probe_%d.tmp", time.Now().UnixNano())), "\\", "/")
	f, err := sftpCli.Create(testFile)
	if err != nil {
		return fmt.Errorf("SFTP 写入测试文件失败: %w", err)
	}
	_, _ = f.Write([]byte("ok"))
	_ = f.Close()

	_ = sftpCli.Remove(testFile)
	return nil
}

func (s *SFTPDestination) Put(ctx context.Context, backupID string, reader io.Reader, _ int64) (string, error) {
	sshConn, sftpCli, err := s.connect()
	if err != nil {
		return "", err
	}
	defer sshConn.Close()
	defer sftpCli.Close()

	_ = sftpCli.MkdirAll(s.remotePath)
	target := s.sanitizeRemote(backupID)
	tmp := fmt.Sprintf("%s.%d.uploading", target, time.Now().UnixNano())

	f, err := sftpCli.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("创建远程临时文件失败: %w", err)
	}

	if _, err := io.Copy(f, reader); err != nil {
		_ = f.Close()
		_ = sftpCli.Remove(tmp)
		return "", fmt.Errorf("SFTP 写入文件失败: %w", err)
	}
	_ = f.Close()

	// 原子重命名
	_ = sftpCli.Remove(target) // 覆盖已有
	if err := sftpCli.Rename(tmp, target); err != nil {
		_ = sftpCli.Remove(tmp)
		return "", fmt.Errorf("SFTP 重命名文件失败: %w", err)
	}

	return fmt.Sprintf("sftp://%s:%d%s", s.host, s.port, target), nil
}

func (s *SFTPDestination) Exists(ctx context.Context, backupID string) (bool, error) {
	sshConn, sftpCli, err := s.connect()
	if err != nil {
		return false, err
	}
	defer sshConn.Close()
	defer sftpCli.Close()

	target := s.sanitizeRemote(backupID)
	info, err := sftpCli.Stat(target)
	if err != nil {
		if errors.Is(err, sftp.ErrSSHFxNoSuchFile) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

func (s *SFTPDestination) Get(ctx context.Context, backupID string) (io.ReadCloser, error) {
	sshConn, sftpCli, err := s.connect()
	if err != nil {
		return nil, err
	}
	target := s.sanitizeRemote(backupID)
	f, err := sftpCli.Open(target)
	if err != nil {
		_ = sftpCli.Close()
		_ = sshConn.Close()
		return nil, err
	}

	// 包装复合关闭器
	return &sftpReadCloser{
		f:       f,
		sftpCli: sftpCli,
		sshConn: sshConn,
	}, nil
}

type sftpReadCloser struct {
	f       *sftp.File
	sftpCli *sftp.Client
	sshConn *ssh.Client
}

func (r *sftpReadCloser) Read(p []byte) (int, error) {
	return r.f.Read(p)
}

func (r *sftpReadCloser) Close() error {
	err1 := r.f.Close()
	err2 := r.sftpCli.Close()
	err3 := r.sshConn.Close()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return err3
}

func (s *SFTPDestination) Delete(ctx context.Context, backupID string) error {
	sshConn, sftpCli, err := s.connect()
	if err != nil {
		return err
	}
	defer sshConn.Close()
	defer sftpCli.Close()

	target := s.sanitizeRemote(backupID)
	return sftpCli.Remove(target)
}

func (s *SFTPDestination) List(ctx context.Context) ([]BackupObject, error) {
	sshConn, sftpCli, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer sshConn.Close()
	defer sftpCli.Close()

	entries, err := sftpCli.ReadDir(s.remotePath)
	if err != nil {
		return nil, err
	}
	var out []BackupObject
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".backup") {
			continue
		}
		bID := strings.TrimSuffix(e.Name(), ".backup")
		out = append(out, BackupObject{
			BackupID:   bID,
			RemotePath: strings.ReplaceAll(filepath.Join(s.remotePath, e.Name()), "\\", "/"),
			Size:       e.Size(),
			ModTime:    e.ModTime(),
		})
	}
	return out, nil
}

func (s *SFTPDestination) GetUsage(ctx context.Context) (*StorageUsage, error) {
	list, err := s.List(ctx)
	if err != nil {
		return &StorageUsage{}, nil
	}
	var used uint64
	for _, it := range list {
		used += uint64(it.Size)
	}
	return &StorageUsage{UsedBytes: used}, nil
}
