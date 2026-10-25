// Package infrastructure 管理 PVE 资源、软件发布和可恢复的部署队列。
// PVE 电源状态、SSH 连通性、软件安装和工作站在线状态分别记录。
package infrastructure

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("记录不存在")
	ErrConflict = errors.New("记录冲突，或该主机已有未结束的部署任务")
	ErrInvalid  = errors.New("参数无效")
)

// Cluster 保存 API 入口与保险箱引用，证书只允许附加信任，不关闭 TLS 校验。
type Cluster struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	SecretID  string    `json:"secret_id"`
	CAPEM     string    `json:"ca_pem,omitempty"`
	Nodes     []Node    `json:"nodes"`
	LastSync  time.Time `json:"last_sync,omitempty"`
	LastError string    `json:"last_error,omitempty"`
}

// Node 是 PVE 返回的节点资源概况。
type Node struct {
	Name      string  `json:"node"`
	Status    string  `json:"status"`
	CPU       float64 `json:"cpu"`
	MaxCPU    int     `json:"maxcpu"`
	Memory    int64   `json:"mem"`
	MaxMemory int64   `json:"maxmem"`
}

// Host 使用 cluster/vmid 作为稳定身份，节点迁移不会生成另一台主机。
type Host struct {
	ID            string    `json:"id"`
	ClusterID     string    `json:"cluster_id,omitempty"`
	VMID          int       `json:"vmid,omitempty"`
	Node          string    `json:"node,omitempty"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	Template      bool      `json:"template"`
	CPU           float64   `json:"cpu"`
	Cores         int       `json:"cores"`
	Memory        int64     `json:"memory"`
	MaxMemory     int64     `json:"max_memory"`
	Disk          int64     `json:"disk"`
	MaxDisk       int64     `json:"max_disk"`
	Address       string    `json:"address"`
	SSHPort       int       `json:"ssh_port"`
	SSHUser       string    `json:"ssh_user"`
	SecretID      string    `json:"secret_id"`
	HostKey       string    `json:"host_key"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	WorkstationID string    `json:"workstation_id,omitempty"`
	ProvisionedBy string    `json:"provisioned_by,omitempty"`
	LastSync      time.Time `json:"last_sync,omitempty"`
}

// Software 的更新源可为空；安装方案独立于软件包来源。
type Software struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Recipe         string    `json:"recipe"` // binary | aew | codex
	Binary         string    `json:"binary"`
	VersionArgs    []string  `json:"version_args"`
	HealthCommand  string    `json:"health_command,omitempty"`
	SourceType     string    `json:"source_type"` // 空 | manifest | github
	SourceURL      string    `json:"source_url"`
	SourceSecretID string    `json:"source_secret_id,omitempty"`
	AssetPattern   string    `json:"asset_pattern,omitempty"` // github: {arch} -> x86_64/aarch64
	SourceOS       string    `json:"source_os,omitempty"`
	SourceArch     string    `json:"source_arch,omitempty"`
	LastChecked    time.Time `json:"last_checked,omitempty"`
	LastError      string    `json:"last_error,omitempty"`
}

// Release 一经发布不覆盖，同版本新构建须使用另一个版本号。
type Release struct {
	ID         string    `json:"id"`
	SoftwareID string    `json:"software_id"`
	Version    string    `json:"version"`
	OS         string    `json:"os"`
	Arch       string    `json:"arch"`
	Format     string    `json:"format"` // binary | tar.gz | zip
	Entry      string    `json:"entry,omitempty"`
	URL        string    `json:"url,omitempty"`
	SHA256     string    `json:"sha256"`
	Signature  string    `json:"signature,omitempty"`
	Size       int64     `json:"size"`
	Cached     bool      `json:"cached"`
	Notes      string    `json:"notes,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Installation 同时保留当前与上一发布，认证状态由远程命令探测。
type Installation struct {
	ID                string    `json:"id"`
	HostID            string    `json:"host_id"`
	SoftwareID        string    `json:"software_id"`
	ReleaseID         string    `json:"release_id"`
	PreviousReleaseID string    `json:"previous_release_id,omitempty"`
	Version           string    `json:"version"`
	DetectedVersion   string    `json:"detected_version,omitempty"`
	Status            string    `json:"status"`
	AuthStatus        string    `json:"auth_status"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// Policy 把发现版本与自动部署分开；锁定版本时不追踪最新版。
type Policy struct {
	ID                 string    `json:"id"`
	HostID             string    `json:"host_id"`
	SoftwareID         string    `json:"software_id"`
	Enabled            bool      `json:"enabled"`
	AutoInstall        bool      `json:"auto_install"`
	PinnedReleaseID    string    `json:"pinned_release_id"`
	Start              string    `json:"start"`
	End                string    `json:"end"`
	Timezone           string    `json:"timezone"`
	IntervalMinutes    int       `json:"interval_minutes"`
	LastChecked        time.Time `json:"last_checked,omitempty"`
	AvailableReleaseID string    `json:"available_release_id,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
	LastOperationID    string    `json:"last_operation_id,omitempty"`
	Revision           int64     `json:"revision"`
}

// VMCreate 只克隆现有 Cloud-init 模板；明确指定 VMID 便于幂等恢复。
type VMCreate struct {
	ClusterID    string   `json:"cluster_id"`
	TemplateNode string   `json:"template_node"`
	TemplateID   int      `json:"template_id"`
	VMID         int      `json:"vmid"`
	Node         string   `json:"node"`
	Name         string   `json:"name"`
	Cores        int      `json:"cores"`
	MemoryMB     int      `json:"memory_mb"`
	DiskGB       int      `json:"disk_gb"`
	DiskDevice   string   `json:"disk_device"`
	Arch         string   `json:"arch"`
	Storage      string   `json:"storage"`
	Bridge       string   `json:"bridge"`
	IPConfig     string   `json:"ip_config"`
	SSHUser      string   `json:"ssh_user"`
	PublicKey    string   `json:"public_key"`
	Address      string   `json:"address"`
	SecretID     string   `json:"secret_id"`
	Releases     []string `json:"releases"`
	ServerURL    string   `json:"server_url"`
}

// Spec 是持久化任务参数，只允许保存引用，禁止放入密码或令牌。
type Spec struct {
	Action         string    `json:"action"`
	HostID         string    `json:"host_id,omitempty"`
	ReleaseID      string    `json:"release_id,omitempty"`
	SoftwareID     string    `json:"software_id,omitempty"`
	SecretID       string    `json:"secret_id,omitempty"`
	ServerURL      string    `json:"server_url,omitempty"`
	Automatic      bool      `json:"automatic,omitempty"`
	PolicyRevision int64     `json:"policy_revision,omitempty"`
	Create         *VMCreate `json:"create,omitempty"`
}

// Operation 租约过期后标为 INTERRUPTED，必须人工重试，不盲目重复远端副作用。
type Operation struct {
	ID         string    `json:"id"`
	HostID     string    `json:"host_id"`
	Actor      string    `json:"actor"`
	Key        string    `json:"-"`
	Spec       Spec      `json:"spec"`
	Status     string    `json:"status"`
	Step       string    `json:"step"`
	Log        string    `json:"log"`
	Error      string    `json:"error,omitempty"`
	Owner      string    `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	NotBefore  time.Time `json:"not_before"`
	LeaseUntil time.Time `json:"-"`
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
