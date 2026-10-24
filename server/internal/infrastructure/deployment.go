package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"
)

// Queue 先持久化再执行；同一幂等键只能对应完全相同的参数。
func (s *Service) Queue(ctx context.Context, spec Spec, actor, key string) (*Operation, error) {
	if len(key) > 160 {
		return nil, ErrInvalid
	}
	if key == "" {
		key = newID()
	}
	key = actor + ":" + key
	if spec.Action == "clone" {
		v := spec.Create
		if v == nil || v.VMID < 100 || v.TemplateID < 100 || !safeName.MatchString(v.Name) || !safeName.MatchString(v.Node) || !safeName.MatchString(v.TemplateNode) || !safeName.MatchString(v.SSHUser) || v.Cores < 1 || v.Cores > 512 || v.MemoryMB < 256 || v.MemoryMB > 4194304 || v.ClusterID == "" || v.VMID == v.TemplateID {
			return nil, ErrInvalid
		}
		if _, err := load[Cluster](ctx, s.Store, "cluster", v.ClusterID); err != nil {
			return nil, err
		}
		if v.IPConfig == "" {
			v.IPConfig = "ip=dhcp"
		}
		if v.Arch == "" {
			v.Arch = "amd64"
		}
		if v.Arch != "amd64" && v.Arch != "arm64" {
			return nil, ErrInvalid
		}
		if v.DiskDevice == "" {
			v.DiskDevice = "scsi0"
		}
		if v.DiskGB < 0 || v.DiskGB > 1048576 || !safeName.MatchString(v.DiskDevice) {
			return nil, ErrInvalid
		}
		if v.SecretID != "" {
			if err := s.validateSecret(v.SecretID, "ssh"); err != nil {
				return nil, err
			}
		}
		if v.Bridge != "" && !safeName.MatchString(v.Bridge) {
			return nil, ErrInvalid
		}
		if v.Storage != "" && !safeName.MatchString(v.Storage) {
			return nil, ErrInvalid
		}
		if strings.ContainsAny(v.PublicKey, "\r\n") {
			return nil, ErrInvalid
		}
		if len(v.Releases) > 20 {
			return nil, ErrInvalid
		}
		for _, id := range v.Releases {
			r, err := load[Release](ctx, s.Store, "release", id)
			if err != nil {
				return nil, err
			}
			if r.OS != "linux" || r.Arch != v.Arch {
				return nil, ErrInvalid
			}
		}
		if v.ServerURL != "" {
			if err := validateURL(v.ServerURL, false); err != nil {
				return nil, err
			}
		}
		spec.HostID = vmHostID(v.ClusterID, v.VMID)
		spec.ServerURL = v.ServerURL
	} else {
		h, err := s.Host(ctx, spec.HostID)
		if err != nil {
			return nil, err
		}
		if h.Template {
			return nil, fmt.Errorf("不能对系统模板执行此操作")
		}
		switch spec.Action {
		case "install", "rollback":
			r, err := load[Release](ctx, s.Store, "release", spec.ReleaseID)
			if err != nil {
				return nil, err
			}
			if r.OS != h.OS || r.Arch != h.Arch {
				return nil, fmt.Errorf("软件包与主机系统或架构不匹配")
			}
			spec.SoftwareID = r.SoftwareID
			if spec.Action == "rollback" {
				inst, err := load[Installation](ctx, s.Store, "installation", h.ID+"-"+r.SoftwareID)
				if err != nil || inst.PreviousReleaseID != r.ID {
					return nil, fmt.Errorf("只能回滚到该主机保留的上一版本")
				}
			}
		case "inspect", "login":
			sw, err := load[Software](ctx, s.Store, "software", spec.SoftwareID)
			if err != nil {
				return nil, err
			}
			if spec.Action == "login" {
				if sw.Recipe != "codex" {
					return nil, ErrInvalid
				}
				if err := s.validateSecret(spec.SecretID, "api"); err != nil {
					return nil, err
				}
			}
		case "start", "shutdown", "reboot":
			if h.ClusterID == "" || h.Status == "missing" {
				return nil, fmt.Errorf("此主机没有可用的 PVE 关联")
			}
		default:
			return nil, ErrInvalid
		}
		if spec.ServerURL != "" {
			if err := validateURL(spec.ServerURL, false); err != nil {
				return nil, err
			}
		}
	}
	now := time.Now().UTC()
	op := &Operation{ID: newID(), HostID: spec.HostID, Actor: actor, Key: key, Spec: spec, Status: "QUEUED", CreatedAt: now, UpdatedAt: now, NotBefore: now}
	return s.Store.Enqueue(ctx, op)
}

// Run 启动执行器与更新调度；任务租约由独立心跳续期。
func (s *Service) Run(ctx context.Context) {
	go s.inventoryLoop(ctx)
	for i := 0; i < 2; i++ {
		go s.worker(ctx, newID())
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}
func (s *Service) worker(ctx context.Context, owner string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			op, err := s.Store.Claim(ctx, owner)
			if err != nil || op == nil {
				continue
			}
			s.execute(ctx, op)
		}
	}
}

func (s *Service) step(ctx context.Context, o *Operation, message string) error {
	o.Step = message
	o.UpdatedAt = time.Now().UTC()
	o.Log += o.UpdatedAt.Format(time.RFC3339) + " " + message + "\n"
	if len(o.Log) > 32768 {
		o.Log = o.Log[len(o.Log)-32768:]
	}
	return s.Store.SaveOperation(ctx, o)
}
func (s *Service) waitOperation(ctx context.Context, o *Operation, message string) {
	o.Status = "WAITING"
	o.Step = message
	o.NotBefore = time.Now().UTC().Add(time.Minute)
	_ = s.Store.SaveOperation(ctx, o)
}

func (s *Service) execute(parent context.Context, o *Operation) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Minute)
	defer cancel()
	renewDone := make(chan struct{})
	defer close(renewDone)
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-renewDone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				renewCtx, renewCancel := context.WithTimeout(ctx, 10*time.Second)
				err := s.Store.Renew(renewCtx, o.ID, o.Owner)
				renewCancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	if o.Spec.Automatic {
		p, err := load[Policy](ctx, s.Store, "policy", o.HostID+"-"+o.Spec.SoftwareID)
		if err != nil || !p.Enabled || !p.AutoInstall || p.Revision != o.Spec.PolicyRevision || p.PinnedReleaseID != "" && p.PinnedReleaseID != o.Spec.ReleaseID {
			s.finish(o, fmt.Errorf("更新策略已停用或目标版本已变更"))
			return
		}
		if !inWindow(p, time.Now()) {
			s.waitOperation(ctx, o, "等待维护窗口")
			return
		}
	}
	if h, err := s.Host(ctx, o.HostID); err == nil && h.WorkstationID != "" && s.Busy != nil {
		busy, err := s.Busy(ctx, h.WorkstationID)
		if err != nil {
			s.finish(o, err)
			return
		}
		if busy {
			s.waitOperation(ctx, o, "等待工作站任务结束")
			return
		}
	}
	err := s.perform(ctx, o)
	if errors.Is(err, errWaiting) {
		return
	}
	s.finish(o, err)
}
func (s *Service) finish(o *Operation, err error) {
	o.Status = "SUCCESS"
	o.Step = "执行完成"
	if err != nil {
		o.Status = "FAILED"
		o.Error = err.Error()
		o.Step = "执行失败"
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			o.Status = "INTERRUPTED"
			o.Step = "执行中断，请核实远端状态"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Store.SaveOperation(ctx, o)
}

var errWaiting = errors.New("等待外部条件")

func (s *Service) perform(ctx context.Context, o *Operation) error {
	if o.Spec.Action == "clone" {
		return s.provision(ctx, o)
	}
	h, err := s.Host(ctx, o.HostID)
	if err != nil {
		return err
	}
	switch o.Spec.Action {
	case "start", "shutdown", "reboot":
		p, err := s.pve(ctx, h.ClusterID, o.Actor)
		if err != nil {
			return err
		}
		if err := s.step(ctx, o, "执行虚拟机电源操作: "+o.Spec.Action); err != nil {
			return err
		}
		if err := p.power(ctx, h, o.Spec.Action); err != nil {
			return err
		}
		return s.Sync(ctx, h.ClusterID, o.Actor)
	case "install", "rollback":
		return s.install(ctx, o, h, o.Spec.ReleaseID)
	case "inspect", "login":
		return s.inspect(ctx, o, h)
	}
	return ErrInvalid
}

func (s *Service) provision(ctx context.Context, o *Operation) error {
	v := *o.Spec.Create
	h, err := s.Host(ctx, o.HostID)
	if err != nil && err != ErrNotFound {
		return err
	}
	if err == ErrNotFound || h.ProvisionedBy != o.ID {
		p, err := s.pve(ctx, v.ClusterID, o.Actor)
		if err != nil {
			return err
		}
		if err := p.clone(ctx, v, func(message string) error {
			return s.step(ctx, o, message)
		}); err != nil {
			return err
		}
		h = Host{ID: o.HostID, ClusterID: v.ClusterID, VMID: v.VMID, Node: v.Node, Name: v.Name, Status: "running", SSHUser: v.SSHUser, SSHPort: 22, SecretID: v.SecretID, Address: v.Address, OS: "linux", Arch: v.Arch, ProvisionedBy: o.ID}
		if err := save(ctx, s.Store, "host", h.ID, h, false); err != nil {
			return err
		}
	}
	if len(v.Releases) == 0 {
		return s.Sync(ctx, v.ClusterID, o.Actor)
	}
	if h.Address == "" {
		p, err := s.pve(ctx, v.ClusterID, o.Actor)
		if err != nil {
			return err
		}
		if address, err := p.guestAddress(ctx, h); err == nil {
			h.Address = address
			if err := save(ctx, s.Store, "host", h.ID, h, false); err != nil {
				return err
			}
		}
	}
	if h.Address == "" || h.HostKey == "" || h.SecretID == "" {
		s.waitOperation(ctx, o, "虚拟机已创建，等待配置 SSH 地址、凭证并核对指纹")
		return errWaiting
	}
	for _, id := range v.Releases {
		r, err := load[Release](ctx, s.Store, "release", id)
		if err != nil {
			return err
		}
		inst, err := load[Installation](ctx, s.Store, "installation", h.ID+"-"+r.SoftwareID)
		if err == nil && inst.ReleaseID == id && inst.Status == "INSTALLED" {
			continue
		}
		if r.OS != h.OS || r.Arch != h.Arch {
			return fmt.Errorf("所选软件包与虚拟机架构不匹配")
		}
		if err := s.install(ctx, o, h, id); err != nil {
			return err
		}
		h, err = s.Host(ctx, h.ID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) install(ctx context.Context, o *Operation, h Host, id string) error {
	r, err := load[Release](ctx, s.Store, "release", id)
	if err != nil {
		return err
	}
	sw, err := load[Software](ctx, s.Store, "software", r.SoftwareID)
	if err != nil {
		return err
	}
	existing, err := load[Installation](ctx, s.Store, "installation", h.ID+"-"+sw.ID)
	if err != nil && err != ErrNotFound {
		return err
	}
	if existing.ReleaseID == r.ID && existing.Status == "INSTALLED" {
		return s.step(ctx, o, "已是目标版本: "+sw.Name+" "+r.Version)
	}
	if err := s.step(ctx, o, "准备软件包: "+sw.Name+" "+r.Version); err != nil {
		return err
	}
	r, f, err := s.CachedPackage(ctx, id, o.Actor)
	if err != nil {
		return err
	}
	defer f.Close()
	// CachedPackage 已重新校验内容，不能仅依赖上传时计算的摘要。
	reader, err := binaryReader(f, r, sw.Binary)
	if err != nil {
		return err
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}
	remote, err := s.Remote.Connect(ctx, h, o.Actor)
	if err != nil {
		return err
	}
	defer remote.Close()
	if _, err := remote.Run(ctx, remoteEnv+"test \"$(uname -s)\" = Linux\ncase \"$(uname -m)\" in x86_64) actual=amd64;; aarch64|arm64) actual=arm64;; *) exit 1;; esac\ntest \"$actual\" = "+shellQuote(r.Arch)+"\n"); err != nil {
		return fmt.Errorf("主机实际系统或架构与软件包不匹配")
	}
	stage, err := remote.Run(ctx, "umask 077\nmktemp -d /tmp/aie.XXXXXXXXXX\n")
	stage = strings.TrimSpace(stage)
	if err != nil || !stagePath.MatchString(stage) {
		return fmt.Errorf("无法创建远端临时目录")
	}
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = remote.Run(cleanCtx, "rm -f -- "+shellQuote(stage+"/program")+"\nrmdir -- "+shellQuote(stage)+"\n")
	}()
	if err := remote.Upload(ctx, stage+"/program", reader); err != nil {
		return err
	}
	if err := s.step(ctx, o, "安装并检查程序: "+sw.Name); err != nil {
		return err
	}
	old, oldErr := load[Installation](ctx, s.Store, "installation", h.ID+"-"+sw.ID)
	if oldErr != nil && oldErr != ErrNotFound {
		return oldErr
	}
	installScript := remoteEnv + "dest=\"$HOME/.local/share/aie-software/" + sw.ID + "/" + r.ID + "\"\nlink=\"$HOME/.local/bin/" + sw.Binary + "\"\nmkdir -p \"$dest\" \"$HOME/.local/bin\"\ncp -- " + shellQuote(stage+"/program") + " \"$dest/program.new\"\nchmod 755 \"$dest/program.new\"\nmv -f \"$dest/program.new\" \"$dest/program\"\nold=$(readlink \"$link\" || true)\n"
	// 不覆盖用户自行安装的普通文件；平台只维护自己的软链接。
	installScript += "if [ -e \"$link\" ] && [ ! -L \"$link\" ]; then echo '目标路径已有非平台文件'; exit 1; fi\nrestore() { rc=$?; if [ \"$rc\" -ne 0 ]; then if [ -n \"$old\" ]; then ln -sfn \"$old\" \"$link.restore\"; mv -Tf \"$link.restore\" \"$link\"; else rm -f \"$link\"; fi; fi; exit \"$rc\"; }\ntrap restore EXIT\nln -sfn \"$dest/program\" \"$link.next\"\nmv -Tf \"$link.next\" \"$link\"\n"
	installScript += shellQuote(sw.Binary)
	for _, arg := range sw.VersionArgs {
		installScript += " " + shellQuote(arg)
	}
	installScript += "\n"
	if sw.HealthCommand != "" {
		installScript += "sh -c " + shellQuote(sw.HealthCommand) + "\n"
	}
	installScript += "trap - EXIT\n"
	output, err := remote.Run(ctx, installScript)
	if err != nil {
		return fmt.Errorf("安装或健康检查失败，已尝试恢复原程序: %s", safeOutput(output))
	}
	if sw.Recipe == "aew" {
		if err := s.startAEW(ctx, o, h, remote); err != nil {
			if old.ReleaseID != "" {
				if restoreErr := s.restoreLink(ctx, remote, sw, old.ReleaseID); restoreErr != nil {
					old.Status = "ERROR"
					old.UpdatedAt = time.Now().UTC()
					_ = save(ctx, s.Store, "installation", old.ID, old, false)
					return fmt.Errorf("%v; 上一版本恢复也未通过健康检查，请在 SSH 终端核实服务", err)
				}
			}
			return err
		}
	}
	inst := Installation{ID: h.ID + "-" + sw.ID, HostID: h.ID, SoftwareID: sw.ID, ReleaseID: r.ID, PreviousReleaseID: old.ReleaseID, Version: r.Version, DetectedVersion: safeOutput(output), Status: "INSTALLED", AuthStatus: "NOT_REQUIRED", UpdatedAt: time.Now().UTC()}
	if sw.Recipe == "codex" {
		auth, err := remote.Run(ctx, remoteEnv+"if codex login status >/dev/null 2>&1; then printf READY; else printf REQUIRED; fi\n")
		if err != nil {
			return err
		}
		inst.AuthStatus = strings.TrimSpace(auth)
	}
	if err := save(ctx, s.Store, "installation", inst.ID, inst, false); err != nil {
		return fmt.Errorf("远端已安装，但安装记录保存失败，请重新探测")
	}
	if h.WorkstationID != "" && s.RefreshModels != nil {
		_ = s.RefreshModels(h.WorkstationID)
	}
	return s.step(ctx, o, "软件已安装: "+sw.Name+" "+r.Version)
}

func safeOutput(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 1500 {
		v = v[:1500]
	}
	return v
}

func (s *Service) restoreLink(ctx context.Context, remote RemoteSession, sw Software, release string) error {
	script := remoteEnv + "ln -sfn \"$HOME/.local/share/aie-software/" + sw.ID + "/" + release + "/program\" \"$HOME/.local/bin/" + sw.Binary + ".restore\"\nmv -Tf \"$HOME/.local/bin/" + sw.Binary + ".restore\" \"$HOME/.local/bin/" + sw.Binary + "\"\n"
	if sw.Recipe == "aew" {
		script += "aew service install\naew service restart\n"
	}
	_, err := remote.Run(ctx, script)
	if err == nil && sw.Recipe == "aew" {
		return aewPing(ctx, remote)
	}
	return err
}

func (s *Service) startAEW(ctx context.Context, o *Operation, h Host, remote RemoteSession) error {
	identity, err := remote.Run(ctx, remoteEnv+"if [ -s \"$AIE_DATA_DIR/identity/workstation-id\" ]; then cat \"$AIE_DATA_DIR/identity/workstation-id\"; fi\n")
	if err != nil {
		return err
	}
	if strings.TrimSpace(identity) == "" {
		if o.Spec.ServerURL == "" || s.Enroll == nil {
			return fmt.Errorf("首次安装 aew 需要填写虚拟机可访问的 Control Plane 地址")
		}
		token, err := s.Enroll(ctx, h.Name, o.Actor)
		if err != nil {
			return err
		}
		output, err := remote.Run(ctx, remoteEnv+"aew register --server "+shellQuote(o.Spec.ServerURL)+" --token "+shellQuote(token)+"\n")
		if err != nil {
			return fmt.Errorf("aew 注册失败: %s", safeOutput(redact(output, token)))
		}
		identity, err = remote.Run(ctx, remoteEnv+"cat \"$AIE_DATA_DIR/identity/workstation-id\"\n")
		if err != nil {
			return err
		}
	}
	wsID := strings.TrimSpace(identity)
	if !safeName.MatchString(wsID) {
		return fmt.Errorf("工作站身份格式无效")
	}
	// user systemd 使用同一登录用户，保持 Codex 认证、PATH 和 aew 身份一致。
	script := remoteEnv + `if [ "$(id -u)" -ne 0 ]; then
  sudo -n loginctl enable-linger "$(id -un)"
  sudo -n systemctl start "user@$(id -u).service"
  export XDG_RUNTIME_DIR="/run/user/$(id -u)"
  unit_dir="$HOME/.config/systemd/user/aie-aew.service.d"
else
  unit_dir=/etc/systemd/system/aie-aew.service.d
fi
mkdir -p "$unit_dir"
printf '[Service]\nEnvironment="PATH=%s/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"\n' "$HOME" > "$unit_dir/path.conf"
aew service install
aew service restart
sleep 2
`
	output, err := remote.Run(ctx, script)
	if err != nil {
		return fmt.Errorf("aew 服务启动失败，请检查 systemd 与免密 sudo: %s", safeOutput(output))
	}
	if err := aewPing(ctx, remote); err != nil {
		return err
	}
	if _, err := mutate[Host](ctx, s.Store, "host", h.ID, func(current *Host) error { current.WorkstationID = wsID; return nil }); err != nil {
		return err
	}
	if s.WorkstationReady != nil {
		deadline := time.NewTimer(60 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for !s.WorkstationReady(ctx, wsID) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return fmt.Errorf("aew 已启动，尚未收到在线心跳，请检查 gRPC 网络和证书")
			case <-tick.C:
			}
		}
	}
	return nil
}

func (s *Service) inspect(ctx context.Context, o *Operation, h Host) error {
	sw, err := load[Software](ctx, s.Store, "software", o.Spec.SoftwareID)
	if err != nil {
		return err
	}
	remote, err := s.Remote.Connect(ctx, h, o.Actor)
	if err != nil {
		return err
	}
	defer remote.Close()
	if o.Spec.Action == "login" {
		key, err := s.resolve(ctx, o.Spec.SecretID, o.Actor, "codex.login")
		if err != nil {
			return err
		}
		out, err := remote.Run(ctx, remoteEnv+"printf '%s' "+shellQuote(key)+" | codex login --with-api-key\n")
		if err != nil {
			return fmt.Errorf("Codex 登录失败: %s", safeOutput(redact(out, key)))
		}
	}
	cmd := remoteEnv + shellQuote(sw.Binary)
	for _, a := range sw.VersionArgs {
		cmd += " " + shellQuote(a)
	}
	cmd += "\n"
	out, err := remote.Run(ctx, cmd)
	if err != nil {
		return fmt.Errorf("程序探测失败: %s", safeOutput(out))
	}
	inst, err := load[Installation](ctx, s.Store, "installation", h.ID+"-"+sw.ID)
	if err != nil && err != ErrNotFound {
		return err
	}
	inst.ID = h.ID + "-" + sw.ID
	inst.HostID = h.ID
	inst.SoftwareID = sw.ID
	inst.Status = "INSTALLED"
	inst.DetectedVersion = safeOutput(out)
	inst.UpdatedAt = time.Now().UTC()
	inst.AuthStatus = "NOT_REQUIRED"
	if sw.Recipe == "codex" {
		out, err := remote.Run(ctx, remoteEnv+"if codex login status >/dev/null 2>&1; then printf READY; else printf REQUIRED; fi\n")
		if err != nil {
			return err
		}
		inst.AuthStatus = strings.TrimSpace(out)
	}
	if err := save(ctx, s.Store, "installation", inst.ID, inst, false); err != nil {
		return err
	}
	if h.WorkstationID != "" && s.RefreshModels != nil {
		_ = s.RefreshModels(h.WorkstationID)
	}
	return nil
}

// HostMaintenance 供业务调度器使用；失败时调用方应暂停给该工作站分配新任务。
func (s *Service) HostMaintenance(ctx context.Context, wsID string) (bool, error) {
	hosts, err := s.Hosts(ctx)
	if err != nil {
		return true, err
	}
	ids := map[string]bool{}
	for _, h := range hosts {
		if h.WorkstationID == wsID {
			ids[h.ID] = true
		}
	}
	if len(ids) == 0 {
		return false, nil
	}
	ops, err := s.Store.Operations(ctx)
	if err != nil {
		return true, err
	}
	for _, o := range ops {
		if ids[o.HostID] && active(o.Status) && o.Spec.Action != "start" {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) reconcile(ctx context.Context) {
	policies, err := s.Policies(ctx)
	if err != nil {
		return
	}
	checked := map[string]error{}
	for _, p := range policies {
		if !p.Enabled {
			continue
		}
		// 发现版本按配置周期执行；部署窗口每分钟评估，避免检查时间落在窗口外就永远无法升级。
		if time.Since(p.LastChecked) >= time.Duration(p.IntervalMinutes)*time.Minute {
			p.LastChecked = time.Now().UTC()
			p.LastError = ""
			if _, ok := checked[p.SoftwareID]; !ok {
				_, checked[p.SoftwareID] = s.CheckUpdates(ctx, p.SoftwareID, "system")
			}
			if err := checked[p.SoftwareID]; err != nil {
				p.LastError = err.Error()
				_ = s.policyProgress(ctx, p)
				continue
			}
		}
		h, err := s.Host(ctx, p.HostID)
		if err != nil {
			p.LastError = err.Error()
			_ = s.policyProgress(ctx, p)
			continue
		}
		releases, err := s.softwareReleases(ctx, p.SoftwareID)
		if err != nil {
			continue
		}
		var target *Release
		for _, r := range releases {
			if p.PinnedReleaseID == "" && strings.Contains(strings.SplitN(r.Version, "+", 2)[0], "-") {
				continue
			}
			if r.OS == h.OS && r.Arch == h.Arch && (p.PinnedReleaseID == "" || p.PinnedReleaseID == r.ID) {
				v := r
				target = &v
				break
			}
		}
		if target != nil {
			p.AvailableReleaseID = target.ID
			if p.LastOperationID != "" {
				previous, err := s.Store.Operation(ctx, p.LastOperationID)
				if err == nil && previous.Spec.ReleaseID == target.ID && (previous.Status == "FAILED" || previous.Status == "INTERRUPTED" || previous.Status == "CANCELLED") {
					p.LastError = "该版本自动部署未成功，请核对日志后手动重试，或重新启用自动部署"
					_ = s.policyProgress(ctx, p)
					continue
				}
			}
			inst, installErr := load[Installation](ctx, s.Store, "installation", h.ID+"-"+p.SoftwareID)
			if installErr != nil && installErr != ErrNotFound {
				p.LastError = installErr.Error()
			} else if p.AutoInstall && inWindow(p, time.Now()) && inst.ReleaseID != target.ID && (inst.ReleaseID == "" || p.PinnedReleaseID != "" || newer(target.Version, inst.Version)) {
				// 无现存安装不自动进行首次注册，避免后台擅自生成机器身份。
				if inst.ReleaseID != "" {
					var op *Operation
					op, err = s.Queue(ctx, Spec{Action: "install", HostID: h.ID, ReleaseID: target.ID, Automatic: true, PolicyRevision: p.Revision}, "system", "update-"+p.ID+"-"+target.ID+"-"+time.Now().UTC().Format("200601021504"))
					if err == nil {
						p.LastOperationID = op.ID
					}
					if err != nil && !errors.Is(err, ErrConflict) {
						p.LastError = err.Error()
					}
				}
			}
		}
		_ = s.policyProgress(ctx, p)
	}
}

// Retry 复用原任务身份，使创建成功后的 SSH 等待阶段不会再次克隆虚拟机。
func (s *Service) Retry(ctx context.Context, id, actor string) (*Operation, error) {
	return s.Store.Retry(ctx, id)
}

var stagePath = regexp.MustCompile(`^/tmp/aie\.[A-Za-z0-9]{10}$`)

var aewEndpoint = regexp.MustCompile(`(?:^|\s)endpoint=([^\s]+)`)

func aewPing(ctx context.Context, remote RemoteSession) error {
	config, err := remote.Run(ctx, remoteEnv+"aew config show\n")
	if err != nil {
		return fmt.Errorf("无法读取 aew 注册后的服务地址")
	}
	match := aewEndpoint.FindStringSubmatch(config)
	if len(match) != 2 {
		return fmt.Errorf("aew 未返回有效的 gRPC 配置")
	}
	if _, _, err := net.SplitHostPort(match[1]); err != nil {
		return fmt.Errorf("aew gRPC 配置格式无效")
	}
	output, err := remote.Run(ctx, remoteEnv+"aew ping --grpc "+shellQuote(match[1])+"\n")
	if err != nil {
		return fmt.Errorf("aew 连接检查失败: %s", safeOutput(output))
	}
	return nil
}

// inventoryLoop 自动刷新只读清单，PVE 不可达时保留上次观测并记录错误。
func (s *Service) inventoryLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			clusters, err := s.Clusters(ctx)
			if err != nil {
				continue
			}
			for _, c := range clusters {
				syncCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
				_ = s.Sync(syncCtx, c.ID, "system")
				cancel()
			}
		}
	}
}
