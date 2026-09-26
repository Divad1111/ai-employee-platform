package backup

import (
	"encoding/json"
	"time"
)

// 常量定义
const (
	FormatVersion = 1

	ScopeCenterFull = "CENTER_FULL"

	DestTypeLocal = "LOCAL"
	DestTypeS3    = "S3"
	DestTypeSFTP  = "SFTP"
	DestTypeSMB   = "SMB"

	ScheduleTypeManual = "MANUAL"
	ScheduleTypeCron   = "CRON"

	CompressZstd = "zstd"
	CompressGzip = "gzip"

	EncryptAES256GCM = "AES-256-GCM"
	EncryptNone      = "NONE"

	StatusPending        = "PENDING"
	StatusRunning        = "RUNNING"
	StatusPackaging      = "PACKAGING"
	StatusEncrypting     = "ENCRYPTING"
	StatusUploading      = "UPLOADING"
	StatusVerifying      = "VERIFYING"
	StatusSuccess        = "SUCCESS"
	StatusPartialSuccess = "PARTIAL_SUCCESS"
	StatusFailed         = "FAILED"
	StatusCancelled      = "CANCELLED"

	RestoreStatusPending       = "PENDING"
	RestoreStatusRunning       = "RUNNING"
	RestoreStatusRestoringDB   = "RESTORING_DB"
	RestoreStatusRestoringData = "RESTORING_DATA"
	RestoreStatusVerifying     = "VERIFYING"
	RestoreStatusSuccess       = "SUCCESS"
	RestoreStatusFailed        = "FAILED"
)

// Destination 表示备份存储目标
type Destination struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Type            string    `json:"type"` // LOCAL | S3 | SFTP | SMB
	ConfigEncrypted string    `json:"-"`    // 加密凭据
	Config          any       `json:"config,omitempty"`
	Enabled         bool      `json:"enabled"`
	LastTestAt      *time.Time `json:"last_test_at,omitempty"`
	LastTestStatus  string    `json:"last_test_status,omitempty"` // SUCCESS | FAILED
	LastTestMessage string    `json:"last_test_message,omitempty"`
	CreatedBy       string    `json:"created_by,omitempty"`
	UpdatedBy       string    `json:"updated_by,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// LocalConfig 本地目录存储配置
type LocalConfig struct {
	Path string `json:"path"` // 例如 /data/backups
}

// S3Config S3 兼容对象存储配置
type S3Config struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key,omitempty"` // 仅写入/更新时提供，响应时脱敏
	PathStyle bool   `json:"path_style"`
	UseSSL    bool   `json:"use_ssl"`
}

// SFTPConfig SFTP 存储配置
type SFTPConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	RemotePath string `json:"remote_path"`
}

// SMBConfig SMB/CIFS 存储配置
type SMBConfig struct {
	Server     string `json:"server"`
	Share      string `json:"share"`
	Username   string `json:"username"`
	Password   string `json:"password,omitempty"`
	Domain     string `json:"domain,omitempty"`
	RemotePath string `json:"remote_path"`
}

// Policy 备份策略
type Policy struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	Enabled              bool       `json:"enabled"`
	Scope                string     `json:"scope"`
	ScheduleType         string     `json:"schedule_type"` // MANUAL | CRON
	CronExpression       string     `json:"cron_expression"`
	Timezone             string     `json:"timezone"`
	RetentionKeepLast    int        `json:"retention_keep_last"`
	CompressionAlgorithm string     `json:"compression_algorithm"`
	CompressionLevel     int        `json:"compression_level"`
	EncryptionEnabled    bool       `json:"encryption_enabled"` // 是否加密（根据用户设置，可不加密）
	EncryptionAlgorithm  string     `json:"encryption_algorithm"`
	EncryptionKeyVersion int        `json:"encryption_key_version"`
	DestinationIDs       []string   `json:"destination_ids"`
	NextRunAt            *time.Time `json:"next_run_at,omitempty"`
	LastRunAt            *time.Time `json:"last_run_at,omitempty"`
	LastRunStatus        string     `json:"last_run_status,omitempty"`
	CreatedBy            string     `json:"created_by,omitempty"`
	UpdatedBy            string     `json:"updated_by,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// BackupRun 备份执行运行记录
type BackupRun struct {
	ID           string           `json:"id"`
	PolicyID     string           `json:"policy_id,omitempty"`
	PolicyName   string           `json:"policy_name,omitempty"`
	BackupID     string           `json:"backup_id"`
	Scope        string           `json:"scope"`
	Status       string           `json:"status"`
	TriggerType  string           `json:"trigger_type"` // MANUAL | SCHEDULED
	StartedAt    *time.Time       `json:"started_at,omitempty"`
	CompletedAt  *time.Time       `json:"completed_at,omitempty"`
	DurationMS   int64            `json:"duration_ms"`
	ArtifactSize int64            `json:"artifact_size"`
	FileCount    int              `json:"file_count"`
	Checksum     string           `json:"checksum"` // SHA-256
	ManifestJSON json.RawMessage  `json:"manifest,omitempty"`
	Destinations []RunDestination `json:"destinations,omitempty"`
	ErrorCode    string           `json:"error_code,omitempty"`
	ErrorMessage string           `json:"error_message,omitempty"`
	CreatedBy    string           `json:"created_by,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// RunDestination 备份分发到各个存储目标的状态
type RunDestination struct {
	ID            string     `json:"id"`
	BackupRunID   string     `json:"backup_run_id"`
	DestinationID string     `json:"destination_id"`
	DestName      string     `json:"destination_name,omitempty"`
	DestType      string     `json:"destination_type,omitempty"`
	Status        string     `json:"status"`
	RemotePath    string     `json:"remote_path"`
	RemoteSize    int64      `json:"remote_size"`
	UploadedAt    *time.Time `json:"uploaded_at,omitempty"`
	VerifiedAt    *time.Time `json:"verified_at,omitempty"`
	ErrorCode     string     `json:"error_code,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
}

// RestoreJob 容灾恢复任务
type RestoreJob struct {
	ID                   string     `json:"id"`
	BackupRunID          string     `json:"backup_run_id"`
	EmergencyBackupRunID string     `json:"emergency_backup_run_id,omitempty"`
	Status               string     `json:"status"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	CreatedBy            string     `json:"created_by,omitempty"`
	ErrorCode            string     `json:"error_code,omitempty"`
	ErrorMessage         string     `json:"error_message,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// Manifest 备份包元数据描述
type Manifest struct {
	FormatVersion int              `json:"format_version"`
	BackupID      string           `json:"backup_id"`
	CreatedAt     string           `json:"created_at"`
	BackupType    string           `json:"backup_type"`
	ServerVersion string           `json:"server_version"`
	DatabaseType  string           `json:"database_type"`
	Encryption    EncryptionInfo   `json:"encryption"`
	Compression   CompressionInfo  `json:"compression"`
	Files         []ManifestFile   `json:"files"`
}

type EncryptionInfo struct {
	Algorithm  string `json:"algorithm"` // AES-256-GCM 或 NONE
	KeyVersion int    `json:"key_version,omitempty"`
}

type CompressionInfo struct {
	Algorithm string `json:"algorithm"` // zstd 或 gzip
	Level     int    `json:"level,omitempty"`
}

type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// OverviewStats 备份总览仪表盘统计
type OverviewStats struct {
	TotalRuns         int64      `json:"total_runs"`
	SuccessRuns       int64      `json:"success_runs"`
	FailedRuns        int64      `json:"failed_runs"`
	PartialRuns       int64      `json:"partial_runs"`
	TotalPolicies     int64      `json:"total_policies"`
	ActivePolicies    int64      `json:"active_policies"`
	TotalDestinations int64      `json:"total_destinations"`
	TotalArtifactSize int64      `json:"total_artifact_size"`
	LastRunAt         *time.Time `json:"last_run_at,omitempty"`
	LastRunStatus     string     `json:"last_run_status,omitempty"`
	LastSuccessRunAt  *time.Time `json:"last_success_run_at,omitempty"`
	NextScheduledAt   *time.Time `json:"next_scheduled_at,omitempty"`
}
