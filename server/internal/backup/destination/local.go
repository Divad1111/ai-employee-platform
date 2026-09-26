package destination

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	ErrPathTraversal = errors.New("非法路径：检测到路径穿越")
	ErrDestNotFound  = errors.New("备份文件在存储目标中不存在")
)

// LocalDestination 本地文件系统存储目标
type LocalDestination struct {
	baseDir string
}

// NewLocalDestination 初始化本地存储目标
func NewLocalDestination(dir string) *LocalDestination {
	if dir == "" {
		dir = "/data/backups"
	}
	return &LocalDestination{baseDir: filepath.Clean(dir)}
}

func (l *LocalDestination) Validate(_ context.Context) error {
	if strings.TrimSpace(l.baseDir) == "" {
		return errors.New("本地备份目录不能为空")
	}
	return nil
}

func (l *LocalDestination) sanitizePath(backupID string) (string, error) {
	// 防止任何 ../ 或反斜杠穿越
	cleanedID := filepath.Base(filepath.Clean(backupID))
	if cleanedID == "." || cleanedID == "/" || cleanedID == "\\" || strings.Contains(cleanedID, "..") {
		return "", ErrPathTraversal
	}
	if !strings.HasSuffix(cleanedID, ".backup") {
		cleanedID = cleanedID + ".backup"
	}
	targetPath := filepath.Join(l.baseDir, cleanedID)
	// 确保最终路径仍在 baseDir 内
	rel, err := filepath.Rel(l.baseDir, targetPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrPathTraversal
	}
	return targetPath, nil
}

func (l *LocalDestination) TestConnection(_ context.Context) error {
	if err := os.MkdirAll(l.baseDir, 0o750); err != nil {
		return fmt.Errorf("创建本地备份目录失败: %w", err)
	}
	testFile := filepath.Join(l.baseDir, fmt.Sprintf(".test_probe_%d.tmp", time.Now().UnixNano()))
	if err := os.WriteFile(testFile, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("写入测试文件失败: %w", err)
	}
	_ = os.Remove(testFile)
	return nil
}

func (l *LocalDestination) Put(_ context.Context, backupID string, reader io.Reader, _ int64) (string, error) {
	if err := os.MkdirAll(l.baseDir, 0o750); err != nil {
		return "", fmt.Errorf("创建存储目录失败: %w", err)
	}
	finalPath, err := l.sanitizePath(backupID)
	if err != nil {
		return "", err
	}
	tmpPath := fmt.Sprintf("%s.%d.uploading", finalPath, time.Now().UnixNano())

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("创建临时备份文件失败: %w", err)
	}

	_, copyErr := io.Copy(f, reader)
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("写入备份文件失败: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("关闭备份文件失败: %w", closeErr)
	}

	// 原子替换（跨平台兼容，目标若已存在先清理）
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(finalPath)
		if retryErr := os.Rename(tmpPath, finalPath); retryErr != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("完成备份文件原子重命名失败: %w", retryErr)
		}
	}

	return finalPath, nil
}

func (l *LocalDestination) Exists(_ context.Context, backupID string) (bool, error) {
	p, err := l.sanitizePath(backupID)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

func (l *LocalDestination) Get(_ context.Context, backupID string) (io.ReadCloser, error) {
	p, err := l.sanitizePath(backupID)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrDestNotFound
		}
		return nil, err
	}
	return f, nil
}

func (l *LocalDestination) Delete(_ context.Context, backupID string) error {
	p, err := l.sanitizePath(backupID)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (l *LocalDestination) List(_ context.Context) ([]BackupObject, error) {
	entries, err := os.ReadDir(l.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []BackupObject
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".backup") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		bID := strings.TrimSuffix(e.Name(), ".backup")
		out = append(out, BackupObject{
			BackupID:   bID,
			RemotePath: filepath.Join(l.baseDir, e.Name()),
			Size:       info.Size(),
			ModTime:    info.ModTime(),
		})
	}
	return out, nil
}

func (l *LocalDestination) GetUsage(_ context.Context) (*StorageUsage, error) {
	// 汇总该目录下已占用的备份大小
	entries, err := os.ReadDir(l.baseDir)
	if err != nil {
		return &StorageUsage{}, nil
	}
	var used uint64
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			used += uint64(info.Size())
		}
	}
	return &StorageUsage{UsedBytes: used}, nil
}
