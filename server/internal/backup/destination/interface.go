package destination

import (
	"context"
	"io"
	"time"
)

// BackupObject 存储目标中的备份对象信息
type BackupObject struct {
	BackupID   string    `json:"backup_id"`
	RemotePath string    `json:"remote_path"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
}

// StorageUsage 存储空间占用统计
type StorageUsage struct {
	TotalBytes     uint64 `json:"total_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
}

// BackupDestination 统一存储目标适配器接口
// 对齐设计文档 §8
type BackupDestination interface {
	// Validate 校验配置格式合法性
	Validate(ctx context.Context) error
	// TestConnection 执行端到端连接验证（包含建立连接、鉴权、写测试临时文件、删除清理）
	TestConnection(ctx context.Context) error
	// Put 上传备份包（带尺寸与流）
	Put(ctx context.Context, backupID string, reader io.Reader, size int64) (remotePath string, err error)
	// Exists 检查备份包是否存在
	Exists(ctx context.Context, backupID string) (bool, error)
	// Get 读取备份包输入流（用于校验或恢复）
	Get(ctx context.Context, backupID string) (io.ReadCloser, error)
	// Delete 删除指定备份包
	Delete(ctx context.Context, backupID string) error
	// List 列出该存储目标中的所有备份对象
	List(ctx context.Context) ([]BackupObject, error)
	// GetUsage 获取存储空间信息（若支持）
	GetUsage(ctx context.Context) (*StorageUsage, error)
}
