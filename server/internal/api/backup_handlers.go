package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/backup"
)

// handleBackupOverview 获取备份控制台总览数据
func (d Deps) handleBackupOverview(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	stats, err := d.Backup.Store.GetOverviewStats(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("获取备份总览统计失败: %v", err))
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// Destination API

type createDestReq struct {
	Name   string          `json:"name"`
	Type   string          `json:"type"` // LOCAL | S3 | SFTP | SMB
	Config json.RawMessage `json:"config"`
}

func (d Deps) handleListBackupDestinations(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	list, err := d.Backup.Store.ListDestinations(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []backup.Destination{}
	}

	// 脱敏处理并解析 config
	for i := range list {
		list[i].Config = d.maskDestConfig(list[i].Type, list[i].ConfigEncrypted)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleGetBackupDestination(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	dest, err := d.Backup.Store.GetDestination(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "存储目标不存在")
		return
	}
	dest.Config = d.maskDestConfig(dest.Type, dest.ConfigEncrypted)
	writeJSON(w, http.StatusOK, dest)
}

func (d Deps) handleCreateBackupDestination(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	var req createDestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeErr(w, http.StatusBadRequest, "目标名称不能为空")
		return
	}

	// 检查是否已存在相同类型且路径相同的存储目标，禁止重复配置
	newKey := d.getDestTargetKey(req.Type, req.Config)
	if newKey != "" {
		existingDests, err := d.Backup.Store.ListDestinations(r.Context())
		if err == nil {
			for _, ed := range existingDests {
				if ed.Type != req.Type {
					continue
				}
				decrypted, decErr := d.Backup.Crypto.DecryptText(ed.ConfigEncrypted)
				if decErr == nil && decrypted != "" {
					edKey := d.getDestTargetKey(ed.Type, []byte(decrypted))
					if edKey != "" && edKey == newKey {
						writeErr(w, http.StatusBadRequest, fmt.Sprintf("已存在相同类型且路径相同的存储目标「%s」，不允许重复创建相同配置的目标", ed.Name))
						return
					}
				}
			}
		}
	}

	// 凭据加密保存
	cfgStr := string(req.Config)
	encryptedCfg, err := d.Backup.Crypto.EncryptText(cfgStr)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("凭据加密失败: %v", err))
		return
	}

	destID := fmt.Sprintf("dest-%d", time.Now().UnixNano())
	dest := &backup.Destination{
		ID:              destID,
		Name:            req.Name,
		Type:            req.Type,
		ConfigEncrypted: encryptedCfg,
		Enabled:         false, // 默认禁用，待测试通过后手动启用
		CreatedBy:       sess.UserID,
		UpdatedBy:       sess.UserID,
	}

	if err := d.Backup.Store.CreateDestination(r.Context(), dest); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("创建存储目标失败: %v", err))
		return
	}

	d.auditUser(r, sess, "backup.destination.create", map[string]string{
		"dest_id": destID,
		"name":    req.Name,
		"type":    req.Type,
	})

	dest.Config = d.maskDestConfig(dest.Type, encryptedCfg)
	writeJSON(w, http.StatusCreated, dest)
}

func (d Deps) handleUpdateBackupDestination(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	var req struct {
		Name    string          `json:"name"`
		Type    string          `json:"type"`
		Enabled *bool           `json:"enabled"`
		Config  json.RawMessage `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求参数格式错误")
		return
	}

	dest, err := d.Backup.Store.GetDestination(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "存储目标不存在")
		return
	}

	newType := dest.Type
	if req.Type != "" {
		newType = req.Type
	}
	var newCfgBytes []byte
	if len(req.Config) > 0 && string(req.Config) != "null" && string(req.Config) != "{}" {
		newCfgBytes = req.Config
	} else {
		decrypted, _ := d.Backup.Crypto.DecryptText(dest.ConfigEncrypted)
		newCfgBytes = []byte(decrypted)
	}

	newKey := d.getDestTargetKey(newType, newCfgBytes)
	if newKey != "" {
		existingDests, err := d.Backup.Store.ListDestinations(r.Context())
		if err == nil {
			for _, ed := range existingDests {
				if ed.ID == dest.ID || ed.Type != newType {
					continue
				}
				decrypted, decErr := d.Backup.Crypto.DecryptText(ed.ConfigEncrypted)
				if decErr == nil && decrypted != "" {
					edKey := d.getDestTargetKey(ed.Type, []byte(decrypted))
					if edKey != "" && edKey == newKey {
						writeErr(w, http.StatusBadRequest, fmt.Sprintf("已存在相同类型且路径相同的存储目标「%s」，不允许修改为重复的目标路径", ed.Name))
						return
					}
				}
			}
		}
	}

	if req.Name != "" {
		dest.Name = req.Name
	}
	if req.Type != "" {
		dest.Type = req.Type
	}
	if req.Enabled != nil {
		dest.Enabled = *req.Enabled
	}

	if len(req.Config) > 0 && string(req.Config) != "null" && string(req.Config) != "{}" {
		// 校验并加密新配置
		enc, err := d.Backup.Crypto.EncryptText(string(req.Config))
		if err == nil {
			dest.ConfigEncrypted = enc
		}
	}

	dest.UpdatedBy = sess.UserID
	if err := d.Backup.Store.UpdateDestination(r.Context(), dest); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	d.auditUser(r, sess, "backup.destination.update", map[string]string{"dest_id": id})
	dest.Config = d.maskDestConfig(dest.Type, dest.ConfigEncrypted)
	writeJSON(w, http.StatusOK, dest)
}

func (d Deps) handleDeleteBackupDestination(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	if err := d.Backup.Store.DeleteDestination(r.Context(), id); err != nil {
		if err == backup.ErrDestinationInUse {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.auditUser(r, sess, "backup.destination.delete", map[string]string{"dest_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d Deps) handleTestBackupDestination(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	err := d.Backup.TestDestination(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	d.auditUser(r, sess, "backup.destination.test", map[string]string{"dest_id": id, "status": "success"})
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "端到端连通性测试通过（连接、鉴权、写测试文件与删除验证均成功）",
	})
}

// Policy API

func (d Deps) handleListBackupPolicies(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	list, err := d.Backup.Store.ListPolicies(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []backup.Policy{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleGetBackupPolicy(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	pol, err := d.Backup.Store.GetPolicy(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "备份策略不存在")
		return
	}
	writeJSON(w, http.StatusOK, pol)
}

func (d Deps) handleCreateBackupPolicy(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	var p backup.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		writeErr(w, http.StatusBadRequest, "策略名称不能为空")
		return
	}
	if p.Scope == "" {
		p.Scope = backup.ScopeCenterFull
	}
	if p.ScheduleType == "" {
		p.ScheduleType = backup.ScheduleTypeCron
	}
	if p.Timezone == "" {
		p.Timezone = "Asia/Shanghai"
	}
	if p.CronExpression == "" && p.ScheduleType == backup.ScheduleTypeCron {
		p.CronExpression = "0 2 * * *"
	}
	if p.RetentionKeepLast <= 0 {
		p.RetentionKeepLast = 7
	}
	if p.CompressionAlgorithm == "" {
		p.CompressionAlgorithm = backup.CompressZstd
	}
	if p.CompressionLevel <= 0 {
		p.CompressionLevel = 3
	}
	// 默认 key version
	if p.EncryptionKeyVersion <= 0 {
		p.EncryptionKeyVersion = 1
	}
	if p.EncryptionAlgorithm == "" {
		p.EncryptionAlgorithm = backup.EncryptAES256GCM
	}

	p.ID = fmt.Sprintf("pol-%d", time.Now().UnixNano())
	p.CreatedBy = sess.UserID
	p.UpdatedBy = sess.UserID

	// 计算下一次执行时间
	if p.Enabled && p.ScheduleType == backup.ScheduleTypeCron {
		next, err := d.Backup.Scheduler.CalculateNextRun(p.CronExpression, p.Timezone, time.Now())
		if err == nil {
			p.NextRunAt = next
		}
	}

	if err := d.Backup.Store.CreatePolicy(r.Context(), &p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	d.auditUser(r, sess, "backup.policy.create", map[string]string{"policy_id": p.ID, "name": p.Name})
	writeJSON(w, http.StatusCreated, p)
}

func (d Deps) handleUpdateBackupPolicy(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	var p backup.Policy
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	p.ID = id
	p.UpdatedBy = sess.UserID

	// 重新计算下次执行时间
	if p.Enabled && p.ScheduleType == backup.ScheduleTypeCron {
		next, err := d.Backup.Scheduler.CalculateNextRun(p.CronExpression, p.Timezone, time.Now())
		if err == nil {
			p.NextRunAt = next
		}
	}

	if err := d.Backup.Store.UpdatePolicy(r.Context(), &p); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	d.auditUser(r, sess, "backup.policy.update", map[string]string{"policy_id": id})
	writeJSON(w, http.StatusOK, p)
}

func (d Deps) handleDeleteBackupPolicy(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	if err := d.Backup.Store.DeletePolicy(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.auditUser(r, sess, "backup.policy.delete", map[string]string{"policy_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d Deps) handleEnableBackupPolicy(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	d.setBackupPolicyEnabled(w, r, sess, true)
}

func (d Deps) handleDisableBackupPolicy(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	d.setBackupPolicyEnabled(w, r, sess, false)
}

func (d Deps) setBackupPolicyEnabled(w http.ResponseWriter, r *http.Request, sess *auth.Session, enabled bool) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	if err := d.Backup.Store.SetPolicyEnabled(r.Context(), id, enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	action := "backup.policy.enable"
	if !enabled {
		action = "backup.policy.disable"
	}
	d.auditUser(r, sess, action, map[string]string{"policy_id": id})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "enabled": enabled})
}

// handleRunBackupPolicy 异步手动执行策略备份 (§54)
func (d Deps) handleRunBackupPolicy(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")

	// 异步拉起备份执行
	go func() {
		_, _ = d.Backup.RunPolicy(context.Background(), id, sess.UserID)
	}()

	d.auditUser(r, sess, "backup.policy.run", map[string]string{"policy_id": id})
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  backup.StatusPending,
		"message": "备份任务已提交并在后台执行中",
	})
}

// handleRunBackupManual 手动即时备份（支持指定存储目标与加密开关）
func (d Deps) handleRunBackupManual(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	var req struct {
		DestinationIDs    []string `json:"destination_ids"`
		EncryptionEnabled *bool    `json:"encryption_enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	enc := true
	if req.EncryptionEnabled != nil {
		enc = *req.EncryptionEnabled
	}

	go func() {
		_, _ = d.Backup.RunManualAdHoc(context.Background(), req.DestinationIDs, enc, sess.UserID)
	}()

	d.auditUser(r, sess, "backup.manual.run", map[string]string{
		"encryption_enabled": fmt.Sprintf("%v", enc),
	})
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  backup.StatusPending,
		"message": "手动备份任务已提交并在后台运行",
	})
}

// Runs API

func (d Deps) handleListBackupRuns(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	status := r.URL.Query().Get("status")
	policyID := r.URL.Query().Get("policy_id")

	items, total, err := d.Backup.Store.ListRuns(r.Context(), limit, offset, status, policyID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []backup.BackupRun{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
	})
}

func (d Deps) handleGetBackupRun(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	run, err := d.Backup.Store.GetRun(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "备份记录不存在")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (d Deps) handleVerifyBackupRun(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	report, err := d.Backup.VerifyRun(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"passed": false,
			"error":  err.Error(),
			"report": report,
		})
		return
	}
	d.auditUser(r, sess, "backup.run.verify", map[string]string{"run_id": id})
	writeJSON(w, http.StatusOK, map[string]any{
		"passed": true,
		"report": report,
	})
}

func (d Deps) handleDeleteBackupRun(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	if err := d.Backup.DeleteRun(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.auditUser(r, sess, "backup.run.delete", map[string]string{"run_id": id})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Restore API

func (d Deps) handleRestoreBackupRun(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	job, err := d.Backup.StartRestoreJob(r.Context(), id, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("发起恢复任务失败: %v", err))
		return
	}
	d.auditUser(r, sess, "backup.run.restore", map[string]string{"run_id": id, "job_id": job.ID})
	writeJSON(w, http.StatusAccepted, job)
}

func (d Deps) handleListRestoreJobs(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	items, err := d.Backup.Store.ListRestoreJobs(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []backup.RestoreJob{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleGetRestoreJob(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Backup == nil {
		writeErr(w, http.StatusServiceUnavailable, "备份服务未就绪")
		return
	}
	id := r.PathValue("id")
	job, err := d.Backup.Store.GetRestoreJob(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "恢复任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (d Deps) maskDestConfig(destType, encryptedJSON string) any {
	if encryptedJSON == "" || d.Backup == nil {
		return map[string]any{}
	}
	decrypted, err := d.Backup.Crypto.DecryptText(encryptedJSON)
	if err != nil || decrypted == "" {
		return map[string]any{}
	}

	switch destType {
	case backup.DestTypeLocal:
		var cfg backup.LocalConfig
		_ = json.Unmarshal([]byte(decrypted), &cfg)
		return cfg
	case backup.DestTypeS3:
		var cfg backup.S3Config
		_ = json.Unmarshal([]byte(decrypted), &cfg)
		if cfg.SecretKey != "" {
			cfg.SecretKey = "******"
		}
		return cfg
	case backup.DestTypeSFTP:
		var cfg backup.SFTPConfig
		_ = json.Unmarshal([]byte(decrypted), &cfg)
		if cfg.Password != "" {
			cfg.Password = "******"
		}
		if cfg.PrivateKey != "" {
			cfg.PrivateKey = "******"
		}
		return cfg
	case backup.DestTypeSMB:
		var cfg backup.SMBConfig
		_ = json.Unmarshal([]byte(decrypted), &cfg)
		if cfg.Password != "" {
			cfg.Password = "******"
		}
		return cfg
	default:
		return map[string]any{}
	}
}

// getDestTargetKey 解析存储目标的类型与物理路径/前缀唯一特征值，用于防止重复创建与路径冲突
func (d Deps) getDestTargetKey(destType string, rawJSON []byte) string {
	if len(rawJSON) == 0 {
		return ""
	}
	switch destType {
	case backup.DestTypeLocal:
		var cfg backup.LocalConfig
		if err := json.Unmarshal(rawJSON, &cfg); err == nil {
			p := strings.TrimSpace(cfg.Path)
			if p == "" {
				p = "/data/backups"
			}
			return "LOCAL:" + filepath.Clean(p)
		}
	case backup.DestTypeS3:
		var cfg backup.S3Config
		if err := json.Unmarshal(rawJSON, &cfg); err == nil {
			ep := strings.ToLower(strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/"))
			bucket := strings.TrimSpace(cfg.Bucket)
			prefix := strings.Trim(strings.TrimSpace(cfg.Prefix), "/")
			return fmt.Sprintf("S3:%s/%s/%s", ep, bucket, prefix)
		}
	case backup.DestTypeSFTP:
		var cfg backup.SFTPConfig
		if err := json.Unmarshal(rawJSON, &cfg); err == nil {
			host := strings.ToLower(strings.TrimSpace(cfg.Host))
			port := cfg.Port
			if port <= 0 {
				port = 22
			}
			remPath := path.Clean("/" + strings.Trim(strings.TrimSpace(cfg.RemotePath), "/"))
			return fmt.Sprintf("SFTP:%s:%d%s", host, port, remPath)
		}
	case backup.DestTypeSMB:
		var cfg backup.SMBConfig
		if err := json.Unmarshal(rawJSON, &cfg); err == nil {
			srv := strings.ToLower(strings.TrimSpace(cfg.Server))
			share := strings.Trim(strings.TrimSpace(cfg.Share), "/")
			remPath := path.Clean("/" + strings.Trim(strings.TrimSpace(cfg.RemotePath), "/"))
			return fmt.Sprintf("SMB:%s/%s%s", srv, share, remPath)
		}
	}
	return ""
}
