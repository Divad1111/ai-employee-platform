package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/secret"
)

// Service 通过 PVE API 管资源，通过可替换的 SSH 执行器部署客体软件。
type Service struct {
	Store            Store
	Secrets          *secret.Manager
	Vault            secret.Store
	Root             string
	Remote           Remote
	Busy             func(context.Context, string) (bool, error)
	Enroll           func(context.Context, string, string) (string, error)
	WorkstationReady func(context.Context, string) bool
	RefreshModels    func(string) error
	sourceMu         sync.Mutex
	ticketMu         sync.Mutex
	tickets          map[string]terminalTicket
	// downloadTransport 仅用于替换下载传输层，默认使用系统 HTTPS 校验。
	downloadTransport http.RoundTripper
	packageLocks      sync.Map
}

func New(store Store, mgr *secret.Manager, vault secret.Store, root string) *Service {
	s := &Service{Store: store, Secrets: mgr, Vault: vault, Root: root, tickets: map[string]terminalTicket{}}
	s.Remote = &SSHRemote{Resolve: s.resolve}
	return s
}

func (s *Service) resolve(ctx context.Context, id, actor, purpose string) (string, error) {
	ref, err := s.Vault.Ref(id)
	if err != nil || !strings.HasPrefix(ref.Name, "infra.") {
		return "", fmt.Errorf("请选择基础设施专用的保险箱凭证")
	}
	return s.Secrets.Access(ctx, id, "USER", actor, "", purpose)
}
func (s *Service) validateSecret(id, kind string) error {
	ref, err := s.Vault.Ref(id)
	if err != nil || !strings.HasPrefix(ref.Name, "infra."+kind+".") {
		return fmt.Errorf("请选择 %s 类型的基础设施凭证", kind)
	}
	return nil
}

func (s *Service) Clusters(ctx context.Context) ([]Cluster, error) {
	return list[Cluster](ctx, s.Store, "cluster")
}
func (s *Service) Hosts(ctx context.Context) ([]Host, error) { return list[Host](ctx, s.Store, "host") }
func (s *Service) Host(ctx context.Context, id string) (Host, error) {
	return load[Host](ctx, s.Store, "host", id)
}
func (s *Service) Software(ctx context.Context) ([]Software, error) {
	return list[Software](ctx, s.Store, "software")
}
func (s *Service) Releases(ctx context.Context) ([]Release, error) {
	return list[Release](ctx, s.Store, "release")
}
func (s *Service) Installations(ctx context.Context) ([]Installation, error) {
	return list[Installation](ctx, s.Store, "installation")
}
func (s *Service) Policies(ctx context.Context) ([]Policy, error) {
	return list[Policy](ctx, s.Store, "policy")
}

func (s *Service) Seed(ctx context.Context) error {
	if err := os.MkdirAll(s.Root, 0o750); err != nil {
		return err
	}
	for _, v := range []Software{
		{ID: "aew", Name: "AI Employee Workstation", Description: "安装并注册本平台工作站；上传对应架构的 Linux aew 二进制", Recipe: "aew", Binary: "aew", VersionArgs: []string{"version"}},
		{ID: "codex", Name: "Codex CLI", Description: "安装后可通过 API Key 或 SSH 设备码完成登录", Recipe: "codex", Binary: "codex", VersionArgs: []string{"--version"}, SourceType: "github", SourceURL: "https://api.github.com/repos/openai/codex/releases/latest", AssetPattern: "codex-{arch}-unknown-linux-musl.tar.gz", SourceOS: "linux", SourceArch: "amd64"},
	} {
		if _, err := load[Software](ctx, s.Store, "software", v.ID); err == ErrNotFound {
			if err := save(ctx, s.Store, "software", v.ID, v, true); err != nil && err != ErrConflict {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SaveCluster(ctx context.Context, c Cluster) (Cluster, error) {
	if strings.TrimSpace(c.Name) == "" {
		return c, ErrInvalid
	}
	if err := validateURL(c.URL, true); err != nil {
		return c, err
	}
	if err := s.validateSecret(c.SecretID, "pve"); err != nil {
		return c, err
	}
	if _, err := newPVE(c, "validate@pve!token=value"); err != nil {
		return c, err
	}
	create := c.ID == ""
	if create {
		c.ID = newID()
		c.Nodes = []Node{}
	} else {
		old, err := load[Cluster](ctx, s.Store, "cluster", c.ID)
		if err != nil {
			return c, err
		}
		c.Nodes = old.Nodes
		c.LastSync = old.LastSync
		c.LastError = old.LastError
	}
	if create {
		return c, save(ctx, s.Store, "cluster", c.ID, c, true)
	}
	return mutate[Cluster](ctx, s.Store, "cluster", c.ID, func(current *Cluster) error {
		current.Name = c.Name
		current.URL = c.URL
		current.SecretID = c.SecretID
		current.CAPEM = c.CAPEM
		return nil
	})
}
func (s *Service) pve(ctx context.Context, id, actor string) (*PVEClient, error) {
	c, err := load[Cluster](ctx, s.Store, "cluster", id)
	if err != nil {
		return nil, err
	}
	token, err := s.resolve(ctx, c.SecretID, actor, "pve.api")
	if err != nil {
		return nil, err
	}
	return newPVE(c, token)
}

func (s *Service) Sync(ctx context.Context, id, actor string) error {
	p, err := s.pve(ctx, id, actor)
	if err != nil {
		return err
	}
	nodes, hosts, err := p.inventory(ctx)
	c := p.Cluster
	if err != nil {
		_, _ = mutate[Cluster](ctx, s.Store, "cluster", id, func(current *Cluster) error {
			if current.URL == c.URL && current.SecretID == c.SecretID {
				current.LastError = err.Error()
			}
			return nil
		})
		return err
	}
	seen := map[string]bool{}
	for _, h := range hosts {
		seen[h.ID] = true
		merge := func(old *Host) error {
			h.Address = old.Address
			h.SSHPort = old.SSHPort
			h.SSHUser = old.SSHUser
			h.SecretID = old.SecretID
			h.HostKey = old.HostKey
			h.OS = old.OS
			h.Arch = old.Arch
			h.WorkstationID = old.WorkstationID
			h.ProvisionedBy = old.ProvisionedBy
			*old = h
			return nil
		}
		if h.SSHPort == 0 {
			h.SSHPort = 22
		}
		if h.OS == "" {
			h.OS = "linux"
		}
		if h.Arch == "" {
			h.Arch = "amd64"
		}
		_, err := mutate[Host](ctx, s.Store, "host", h.ID, merge)
		if err == ErrNotFound {
			err = save(ctx, s.Store, "host", h.ID, h, true)
			if err == ErrConflict {
				_, err = mutate[Host](ctx, s.Store, "host", h.ID, merge)
			}
		}
		if err != nil {
			return err
		}
	}
	oldHosts, err := s.Hosts(ctx)
	if err != nil {
		return err
	}
	for _, h := range oldHosts {
		if h.ClusterID == id && !seen[h.ID] {
			if _, err := mutate[Host](ctx, s.Store, "host", h.ID, func(current *Host) error { current.Status = "missing"; return nil }); err != nil {
				return err
			}
		}
	}
	_, err = mutate[Cluster](ctx, s.Store, "cluster", id, func(current *Cluster) error {
		if current.URL == c.URL && current.SecretID == c.SecretID {
			current.Nodes = nodes
			current.LastSync = time.Now().UTC()
			current.LastError = ""
		}
		return nil
	})
	return err
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$`)

func (s *Service) SaveHost(ctx context.Context, h Host) (Host, error) {
	create := h.ID == ""
	if create {
		h = Host{ID: newID(), Name: h.Name, Status: "external", Address: h.Address, SSHUser: h.SSHUser, SSHPort: h.SSHPort, SecretID: h.SecretID, HostKey: h.HostKey, OS: h.OS, Arch: h.Arch}
	} else {
		old, err := s.Host(ctx, h.ID)
		if err != nil {
			return h, err
		}
		old.Name = h.Name
		old.Address = h.Address
		old.SSHUser = h.SSHUser
		old.SSHPort = h.SSHPort
		old.SecretID = h.SecretID
		old.HostKey = h.HostKey
		old.OS = h.OS
		old.Arch = h.Arch
		h = old
	}
	if h.SSHPort == 0 {
		h.SSHPort = 22
	}
	if h.OS == "" {
		h.OS = "linux"
	}
	if h.Arch == "" {
		h.Arch = "amd64"
	}
	if strings.TrimSpace(h.Name) == "" || h.SSHPort < 1 || h.SSHPort > 65535 || !safeName.MatchString(h.SSHUser) || strings.ContainsAny(h.Address, " /\\\r\n\t") || h.Address == "" || h.OS != "linux" || (h.Arch != "amd64" && h.Arch != "arm64") {
		return h, fmt.Errorf("%w: 请填写有效的 Linux 主机、SSH 用户和端口", ErrInvalid)
	}
	if strings.HasPrefix(h.Address, "-") {
		return h, ErrInvalid
	}
	if h.SecretID != "" {
		if err := s.validateSecret(h.SecretID, "ssh"); err != nil {
			return h, err
		}
	}
	if h.HostKey != "" && !strings.HasPrefix(h.HostKey, "SHA256:") {
		return h, fmt.Errorf("%w: SSH 指纹应使用 SHA256 格式", ErrInvalid)
	}
	if create {
		return h, save(ctx, s.Store, "host", h.ID, h, true)
	}
	return mutate[Host](ctx, s.Store, "host", h.ID, func(current *Host) error {
		current.Name = h.Name
		current.Address = h.Address
		current.SSHUser = h.SSHUser
		current.SSHPort = h.SSHPort
		current.SecretID = h.SecretID
		current.HostKey = h.HostKey
		current.OS = h.OS
		current.Arch = h.Arch
		return nil
	})
}

func (s *Service) SaveSoftware(ctx context.Context, v Software) (Software, error) {
	create := v.ID == ""
	if create {
		v.ID = newID()
	} else {
		if _, err := load[Software](ctx, s.Store, "software", v.ID); err != nil {
			return v, err
		}
	}
	if strings.TrimSpace(v.Name) == "" || !safeName.MatchString(v.Binary) || (v.Recipe != "binary" && v.Recipe != "aew" && v.Recipe != "codex") {
		return v, fmt.Errorf("%w: 软件名称、程序名或安装方案无效", ErrInvalid)
	}
	if v.Recipe == "aew" && v.Binary != "aew" || v.Recipe == "codex" && v.Binary != "codex" {
		return v, ErrInvalid
	}
	if len(v.VersionArgs) == 0 {
		v.VersionArgs = []string{"--version"}
	}
	if v.SourceURL == "" {
		v.SourceType = ""
	} else {
		if err := validateURL(v.SourceURL, true); err != nil {
			return v, err
		}
		if v.SourceType != "manifest" && v.SourceType != "github" {
			return v, ErrInvalid
		}
	}
	if v.SourceSecretID != "" {
		if err := s.validateSecret(v.SourceSecretID, "api"); err != nil {
			return v, err
		}
	}
	if v.SourceType == "github" {
		if v.SourceOS == "" {
			v.SourceOS = "linux"
		}
		if v.SourceArch == "" {
			v.SourceArch = "amd64"
		}
		if v.AssetPattern == "" || v.SourceOS != "linux" && v.SourceOS != "darwin" && v.SourceOS != "windows" || v.SourceArch != "amd64" && v.SourceArch != "arm64" {
			return v, fmt.Errorf("%w: 请填写 GitHub 包名和适用平台", ErrInvalid)
		}
	}
	return v, save(ctx, s.Store, "software", v.ID, v, create)
}

// DeleteCluster 保留已有主机和安装历史，需先明确移除关联主机后才能删除连接。
func (s *Service) DeleteCluster(ctx context.Context, id string) error {
	hosts, err := s.Hosts(ctx)
	if err != nil {
		return err
	}
	for _, h := range hosts {
		if h.ClusterID == id {
			return fmt.Errorf("该连接仍有关联虚拟机，请保留连接以维护资源历史")
		}
	}
	return s.Store.Delete(ctx, "cluster", id)
}

func (s *Service) Credentials() []secret.Ref {
	out := []secret.Ref{}
	for _, r := range s.Vault.List() {
		if strings.HasPrefix(r.Name, "infra.") {
			out = append(out, r)
		}
	}
	return out
}
func (s *Service) SaveCredential(ctx context.Context, kind, name, value, actor, ip string) (secret.Meta, error) {
	if !safeName.MatchString(name) || len(value) == 0 || len(value) > 65536 {
		return secret.Meta{}, ErrInvalid
	}
	switch kind {
	case "pve":
		if _, err := newPVE(Cluster{URL: "https://localhost"}, value); err != nil {
			return secret.Meta{}, err
		}
	case "ssh":
		var c SSHCredential
		if err := json.Unmarshal([]byte(value), &c); err != nil || c.Password == "" && c.PrivateKey == "" {
			return secret.Meta{}, fmt.Errorf("SSH 凭证必须包含 password 或 private_key")
		}
	case "api":
	default:
		return secret.Meta{}, ErrInvalid
	}
	return s.Secrets.Put(ctx, "infra."+kind+"."+name, value, actor, ip, "基础设施专用凭证")
}
func (s *Service) RevealCredential(ctx context.Context, id, actor string) (string, error) {
	return s.resolve(ctx, id, actor, "infra.credential.reveal")
}

// DiscoverAddress 只读取 Guest Agent 数据，不自动信任 SSH 主机指纹。
func (s *Service) DiscoverAddress(ctx context.Context, id, actor string) (Host, error) {
	h, err := s.Host(ctx, id)
	if err != nil {
		return h, err
	}
	p, err := s.pve(ctx, h.ClusterID, actor)
	if err != nil {
		return h, err
	}
	h.Address, err = p.guestAddress(ctx, h)
	if err != nil {
		return h, err
	}
	return mutate[Host](ctx, s.Store, "host", h.ID, func(current *Host) error { current.Address = h.Address; return nil })
}

func (s *Service) SavePolicy(ctx context.Context, p Policy) (Policy, error) {
	h, err := s.Host(ctx, p.HostID)
	if err != nil {
		return p, err
	}
	if _, err := load[Software](ctx, s.Store, "software", p.SoftwareID); err != nil {
		return p, err
	}
	p.ID = h.ID + "-" + p.SoftwareID
	if p.Timezone == "" {
		p.Timezone = "Asia/Shanghai"
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return p, ErrInvalid
	}
	if p.IntervalMinutes < 1 || p.IntervalMinutes > 10080 {
		return p, ErrInvalid
	}
	if _, err := parseClock(p.Start); err != nil {
		return p, err
	}
	if _, err := parseClock(p.End); err != nil {
		return p, err
	}
	if p.PinnedReleaseID != "" {
		r, err := load[Release](ctx, s.Store, "release", p.PinnedReleaseID)
		if err != nil || r.SoftwareID != p.SoftwareID || r.OS != h.OS || r.Arch != h.Arch {
			return p, ErrInvalid
		}
	}
	result, err := mutate[Policy](ctx, s.Store, "policy", p.ID, func(old *Policy) error {
		p.LastChecked = old.LastChecked
		p.AvailableReleaseID = old.AvailableReleaseID
		p.LastOperationID = ""
		if p.AutoInstall == old.AutoInstall && p.Enabled == old.Enabled && p.PinnedReleaseID == old.PinnedReleaseID {
			p.LastOperationID = old.LastOperationID
		}
		p.Revision = old.Revision + 1
		*old = p
		return nil
	})
	if err == ErrNotFound {
		p.Revision = 1
		return p, save(ctx, s.Store, "policy", p.ID, p, true)
	}
	return result, err
}

// policyProgress 只合并同一策略修订的运行结果，不能重新启用已被管理员停用的策略。
func (s *Service) policyProgress(ctx context.Context, p Policy) error {
	_, err := mutate[Policy](ctx, s.Store, "policy", p.ID, func(current *Policy) error {
		if current.Revision != p.Revision {
			return nil
		}
		current.LastChecked = p.LastChecked
		current.LastError = p.LastError
		current.AvailableReleaseID = p.AvailableReleaseID
		current.LastOperationID = p.LastOperationID
		return nil
	})
	return err
}
func parseClock(v string) (int, error) {
	t, err := time.Parse("15:04", v)
	if err != nil {
		return 0, fmt.Errorf("%w: 维护窗口须使用 HH:MM", ErrInvalid)
	}
	return t.Hour()*60 + t.Minute(), nil
}
func inWindow(p Policy, now time.Time) bool {
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return false
	}
	start, err := parseClock(p.Start)
	if err != nil {
		return false
	}
	end, err := parseClock(p.End)
	if err != nil {
		return false
	}
	v := now.In(loc)
	n := v.Hour()*60 + v.Minute()
	if start == end {
		return true
	}
	if start < end {
		return n >= start && n < end
	}
	return n >= start || n < end
}

func (s *Service) artifactPath(id string) string { return filepath.Join(s.Root, id+".pkg") }
func hostAddress(h Host) string                  { return net.JoinHostPort(h.Address, fmt.Sprint(h.SSHPort)) }
