package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/backup/database"
	"github.com/ai-employee-platform/server/internal/backup/destination"
)

// BackupExecutor 备份执行编排器
type BackupExecutor struct {
	store      Store
	dbProvider database.DatabaseBackupProvider
	crypto     *CryptoManager
	caDir      string
	secretDir  string
	artDir     string
	workDir    string

	mu          sync.Mutex
	runningJobs map[string]bool // key: policy_id (空为手动即时任务)
}

// NewBackupExecutor 初始化执行器
func NewBackupExecutor(
	store Store,
	db database.DatabaseBackupProvider,
	crypto *CryptoManager,
	caDir, secretDir, artDir, workDir string,
) *BackupExecutor {
	if workDir == "" {
		workDir = "/data/backup_work"
	}
	_ = os.MkdirAll(workDir, 0o750)
	return &BackupExecutor{
		store:       store,
		dbProvider:  db,
		crypto:      crypto,
		caDir:       caDir,
		secretDir:   secretDir,
		artDir:      artDir,
		workDir:     workDir,
		runningJobs: make(map[string]bool),
	}
}

// RunOptions 备份启动选项
type RunOptions struct {
	PolicyID          string
	TriggerType       string // MANUAL | SCHEDULED
	CreatedBy         string
	EncryptionEnabled *bool  // 可选手动覆盖策略中的加密开关
	DestinationIDs    []string
}

// Execute 触发一次全量备份
func (e *BackupExecutor) Execute(ctx context.Context, opts RunOptions, getDest func(destID string) (destination.BackupDestination, error)) (*BackupRun, error) {
	e.mu.Lock()
	lockKey := opts.PolicyID
	if lockKey != "" && e.runningJobs[lockKey] {
		e.mu.Unlock()
		return nil, ErrPolicyAlreadyBusy
	}
	if lockKey != "" {
		e.runningJobs[lockKey] = true
	}
	e.mu.Unlock()

	defer func() {
		if lockKey != "" {
			e.mu.Lock()
			delete(e.runningJobs, lockKey)
			e.mu.Unlock()
		}
	}()

	var pol *Policy
	if opts.PolicyID != "" {
		var err error
		pol, err = e.store.GetPolicy(ctx, opts.PolicyID)
		if err != nil {
			return nil, fmt.Errorf("读取备份策略失败: %w", err)
		}
	}

	// 汇总目标 ID
	destIDs := opts.DestinationIDs
	if len(destIDs) == 0 && pol != nil {
		destIDs = pol.DestinationIDs
	}
	if len(destIDs) == 0 {
		return nil, errors.New("备份策略未配置任何有效存储目标")
	}

	// 加密配置：优先手动选项，若无则使用策略配置（默认开启）
	encEnabled := true
	if pol != nil {
		encEnabled = pol.EncryptionEnabled
	}
	if opts.EncryptionEnabled != nil {
		encEnabled = *opts.EncryptionEnabled
	}

	backupID := fmt.Sprintf("backup-%s-%d", time.Now().UTC().Format("20060102T150405Z"), time.Now().UnixNano()%1000000)
	runID := "run-" + backupID

	run := &BackupRun{
		ID:          runID,
		PolicyID:    opts.PolicyID,
		PolicyName:  "",
		BackupID:    backupID,
		Scope:       ScopeCenterFull,
		Status:      StatusRunning,
		TriggerType: opts.TriggerType,
		CreatedBy:   opts.CreatedBy,
	}
	if pol != nil {
		run.PolicyName = pol.Name
	}

	startTime := time.Now().UTC()
	run.StartedAt = &startTime
	if err := e.store.CreateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("创建备份运行记录失败: %w", err)
	}

	// 1. 生成快照
	snapResult, err := CreateSnapshot(ctx, SnapshotConfig{
		BackupID:          backupID,
		ServerVersion:     "v1.0.0",
		CADir:             e.caDir,
		SecretDir:         e.secretDir,
		ArtifactDir:       e.artDir,
		DBProvider:        e.dbProvider,
		WorkDir:           e.workDir,
		CompressionAlgo:   CompressZstd,
		CompressionLevel:  3,
		EncryptionEnabled: encEnabled, // 用户设置的加密开关生效
		EncryptionKeyVer:  1,
		Crypto:            e.crypto,
	})

	if err != nil {
		endTime := time.Now().UTC()
		run.Status = StatusFailed
		run.CompletedAt = &endTime
		run.DurationMS = endTime.Sub(startTime).Milliseconds()
		run.ErrorCode = "ERR_SNAPSHOT"
		run.ErrorMessage = err.Error()
		_ = e.store.UpdateRun(ctx, run)
		return run, err
	}

	run.ArtifactSize = snapResult.ArtifactSize
	run.FileCount = snapResult.FileCount
	run.Checksum = snapResult.Checksum
	mBytes, _ := json.Marshal(snapResult.Manifest)
	run.ManifestJSON = mBytes
	run.Status = StatusUploading
	_ = e.store.UpdateRun(ctx, run)

	// 2. 分发上传到存储目标 (去重处理)
	var uploadSuccessCount int
	var destWg sync.WaitGroup
	var destMu sync.Mutex

	dedupDestIDs := make([]string, 0, len(destIDs))
	seenDest := make(map[string]bool)
	for _, id := range destIDs {
		if id != "" && !seenDest[id] {
			seenDest[id] = true
			dedupDestIDs = append(dedupDestIDs, id)
		}
	}
	destIDs = dedupDestIDs

	// 控制最大并发上传目标数 = 2
	sem := make(chan struct{}, 2)

	for _, destID := range destIDs {
		destID := destID
		destWg.Add(1)
		go func() {
			defer destWg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			rd := &RunDestination{
				ID:            fmt.Sprintf("rd-%s-%s", run.ID, destID),
				BackupRunID:   run.ID,
				DestinationID: destID,
				Status:        StatusRunning,
			}
			_ = e.store.CreateRunDestination(ctx, rd)

			destObj, err := getDest(destID)
			if err != nil {
				rd.Status = StatusFailed
				rd.ErrorCode = "ERR_INIT_DEST"
				rd.ErrorMessage = err.Error()
				_ = e.store.UpdateRunDestination(ctx, rd)
				return
			}

			// 打开本地生成的快照文件进行流式上传
			f, err := os.Open(snapResult.ArtifactPath)
			if err != nil {
				rd.Status = StatusFailed
				rd.ErrorCode = "ERR_OPEN_ARTIFACT"
				rd.ErrorMessage = err.Error()
				_ = e.store.UpdateRunDestination(ctx, rd)
				return
			}
			defer f.Close()

			remotePath, err := destObj.Put(ctx, backupID, f, snapResult.ArtifactSize)
			if err != nil {
				rd.Status = StatusFailed
				rd.ErrorCode = "ERR_UPLOAD"
				rd.ErrorMessage = err.Error()
				_ = e.store.UpdateRunDestination(ctx, rd)
				return
			}

			upTime := time.Now().UTC()
			rd.RemotePath = remotePath
			rd.RemoteSize = snapResult.ArtifactSize
			rd.UploadedAt = &upTime

			// 验证存在性
			exists, err := destObj.Exists(ctx, backupID)
			if err == nil && exists {
				rd.Status = StatusSuccess
				rd.VerifiedAt = &upTime
				destMu.Lock()
				uploadSuccessCount++
				destMu.Unlock()
			} else {
				rd.Status = StatusFailed
				rd.ErrorCode = "ERR_VERIFY_EXISTS"
				rd.ErrorMessage = "上传后目标端未能成功校验到对象"
			}
			_ = e.store.UpdateRunDestination(ctx, rd)
		}()
	}

	destWg.Wait()

	// 清理本地生成的临时快照文件
	_ = os.Remove(snapResult.ArtifactPath)

	endTime := time.Now().UTC()
	run.CompletedAt = &endTime
	run.DurationMS = endTime.Sub(startTime).Milliseconds()

	if uploadSuccessCount == len(destIDs) {
		run.Status = StatusSuccess
	} else if uploadSuccessCount > 0 {
		run.Status = StatusPartialSuccess
	} else {
		run.Status = StatusFailed
		run.ErrorCode = "ERR_ALL_DEST_FAILED"
		run.ErrorMessage = "所有目标存储分发均失败"
	}

	_ = e.store.UpdateRun(ctx, run)

	// 更新策略的最后运行状态
	if pol != nil {
		_ = e.store.UpdatePolicyRunStatus(ctx, pol.ID, endTime, run.Status, pol.NextRunAt)
		// 3. 执行超期保留清理 (Retention Cleanup)
		if pol.RetentionKeepLast > 0 && run.Status == StatusSuccess {
			go e.cleanupRetention(context.Background(), pol, getDest)
		}
	}

	return run, nil
}

// cleanupRetention 清理超过 keep_last 的历史快照物理文件
func (e *BackupExecutor) cleanupRetention(ctx context.Context, pol *Policy, getDest func(destID string) (destination.BackupDestination, error)) {
	runs, _, err := e.store.ListRuns(ctx, 100, 0, StatusSuccess, pol.ID)
	if err != nil || len(runs) <= pol.RetentionKeepLast {
		return
	}

	// 超过数量的旧记录
	expiredRuns := runs[pol.RetentionKeepLast:]
	for _, oldRun := range expiredRuns {
		for _, rd := range oldRun.Destinations {
			if d, err := getDest(rd.DestinationID); err == nil {
				_ = d.Delete(ctx, oldRun.BackupID)
			}
		}
	}
}
