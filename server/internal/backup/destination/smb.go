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

	"github.com/hirochachacha/go-smb2"
)

// SMBDestination SMB / CIFS 共享存储目标 (支持 SMB2 / SMB3)
type SMBDestination struct {
	server     string
	share      string
	username   string
	password   string
	domain     string
	remotePath string
}

// SMBDestOptions SMB 参数
type SMBDestOptions struct {
	Server     string
	Share      string
	Username   string
	Password   string
	Domain     string
	RemotePath string
}

// NewSMBDestination 初始化 SMB 目标
func NewSMBDestination(opts SMBDestOptions) *SMBDestination {
	srv := opts.Server
	// 去除开头的 \\ 或 //
	srv = strings.TrimPrefix(srv, "\\\\")
	srv = strings.TrimPrefix(srv, "//")
	if !strings.Contains(srv, ":") {
		srv = srv + ":445"
	}
	rPath := strings.Trim(opts.RemotePath, "/\\")
	return &SMBDestination{
		server:     srv,
		share:      opts.Share,
		username:   opts.Username,
		password:   opts.Password,
		domain:     opts.Domain,
		remotePath: rPath,
	}
}

func (s *SMBDestination) connect() (net.Conn, *smb2.Share, error) {
	conn, err := net.DialTimeout("tcp", s.server, 10*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("连接 SMB 服务器失败 (%s): %w", s.server, err)
	}

	d := &smb2.Dialer{
		Initiator: &smb2.NTLMInitiator{
			User:     s.username,
			Password: s.password,
			Domain:   s.domain,
		},
	}

	session, err := d.Dial(conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("SMB 身份验证握手失败: %w", err)
	}

	share, err := session.Mount(s.share)
	if err != nil {
		_ = session.Logoff()
		_ = conn.Close()
		return nil, nil, fmt.Errorf("挂载 SMB 共享 %q 失败: %w", s.share, err)
	}

	return conn, share, nil
}

func (s *SMBDestination) sanitizeRemote(backupID string) string {
	cleaned := filepath.Base(filepath.Clean(backupID))
	if !strings.HasSuffix(cleaned, ".backup") {
		cleaned = cleaned + ".backup"
	}
	if s.remotePath != "" {
		return filepath.Join(s.remotePath, cleaned)
	}
	return cleaned
}

func (s *SMBDestination) Validate(_ context.Context) error {
	if s.server == "" {
		return errors.New("SMB 服务器地址不能为空")
	}
	if s.share == "" {
		return errors.New("SMB 共享名称 (Share) 不能为空")
	}
	if s.username == "" {
		return errors.New("SMB 用户名不能为空")
	}
	return nil
}

func (s *SMBDestination) TestConnection(ctx context.Context) error {
	if err := s.Validate(ctx); err != nil {
		return err
	}
	conn, share, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	defer share.Umount()

	if s.remotePath != "" {
		_ = share.MkdirAll(s.remotePath, 0o755)
	}

	testFile := filepath.Join(s.remotePath, fmt.Sprintf(".test_probe_%d.tmp", time.Now().UnixNano()))
	f, err := share.Create(testFile)
	if err != nil {
		return fmt.Errorf("SMB 写入测试文件失败: %w", err)
	}
	_, _ = f.Write([]byte("ok"))
	_ = f.Close()

	_ = share.Remove(testFile)
	return nil
}

func (s *SMBDestination) Put(ctx context.Context, backupID string, reader io.Reader, _ int64) (string, error) {
	conn, share, err := s.connect()
	if err != nil {
		return "", err
	}
	defer conn.Close()
	defer share.Umount()

	if s.remotePath != "" {
		_ = share.MkdirAll(s.remotePath, 0o755)
	}

	target := s.sanitizeRemote(backupID)
	tmp := fmt.Sprintf("%s.%d.uploading", target, time.Now().UnixNano())

	f, err := share.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("创建 SMB 临时文件失败: %w", err)
	}

	if _, err := io.Copy(f, reader); err != nil {
		_ = f.Close()
		_ = share.Remove(tmp)
		return "", fmt.Errorf("写入 SMB 文件失败: %w", err)
	}
	_ = f.Close()

	_ = share.Remove(target) // 覆盖
	if err := share.Rename(tmp, target); err != nil {
		_ = share.Remove(tmp)
		return "", fmt.Errorf("SMB 重命名文件失败: %w", err)
	}

	return fmt.Sprintf("smb://%s/%s/%s", s.server, s.share, strings.ReplaceAll(target, "\\", "/")), nil
}

func (s *SMBDestination) Exists(ctx context.Context, backupID string) (bool, error) {
	conn, share, err := s.connect()
	if err != nil {
		return false, err
	}
	defer conn.Close()
	defer share.Umount()

	target := s.sanitizeRemote(backupID)
	info, err := share.Stat(target)
	if err != nil {
		return false, nil
	}
	return !info.IsDir(), nil
}

func (s *SMBDestination) Get(ctx context.Context, backupID string) (io.ReadCloser, error) {
	conn, share, err := s.connect()
	if err != nil {
		return nil, err
	}
	target := s.sanitizeRemote(backupID)
	f, err := share.Open(target)
	if err != nil {
		_ = share.Umount()
		_ = conn.Close()
		return nil, err
	}

	return &smbReadCloser{
		f:     f,
		share: share,
		conn:  conn,
	}, nil
}

type smbReadCloser struct {
	f     *smb2.File
	share *smb2.Share
	conn  net.Conn
}

func (r *smbReadCloser) Read(p []byte) (int, error) {
	return r.f.Read(p)
}

func (r *smbReadCloser) Close() error {
	err1 := r.f.Close()
	err2 := r.share.Umount()
	err3 := r.conn.Close()
	if err1 != nil {
		return err1
	}
	if err2 != nil {
		return err2
	}
	return err3
}

func (s *SMBDestination) Delete(ctx context.Context, backupID string) error {
	conn, share, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	defer share.Umount()

	target := s.sanitizeRemote(backupID)
	return share.Remove(target)
}

func (s *SMBDestination) List(ctx context.Context) ([]BackupObject, error) {
	conn, share, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	defer share.Umount()

	dirPath := s.remotePath
	if dirPath == "" {
		dirPath = "."
	}
	entries, err := share.ReadDir(dirPath)
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
			RemotePath: filepath.Join(dirPath, e.Name()),
			Size:       e.Size(),
			ModTime:    e.ModTime(),
		})
	}
	return out, nil
}

func (s *SMBDestination) GetUsage(ctx context.Context) (*StorageUsage, error) {
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
