package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/backup/database"
	"github.com/ai-employee-platform/server/internal/backup/destination"
)

// RestoreOptions 恢复参数
type RestoreOptions struct {
	RestoreJobID string
	BackupRunID  string
	CADir        string
	SecretDir    string
	ArtifactDir  string
	WorkDir      string
	DBProvider   database.DatabaseBackupProvider
	Crypto       *CryptoManager
}

// RestoreManager 负责容灾恢复全流程
type RestoreManager struct {
	operation  sync.Mutex
	store      Store
	dbProvider database.DatabaseBackupProvider
	crypto     *CryptoManager
	caDir      string
	secretDir  string
	artDir     string
	workDir    string
}

// NewRestoreManager 初始化
func NewRestoreManager(
	store Store,
	db database.DatabaseBackupProvider,
	crypto *CryptoManager,
	caDir, secretDir, artDir, workDir string,
) *RestoreManager {
	if workDir == "" {
		workDir = "/data/backup_work"
	}
	return &RestoreManager{
		store:      store,
		dbProvider: db,
		crypto:     crypto,
		caDir:      caDir,
		secretDir:  secretDir,
		artDir:     artDir,
		workDir:    workDir,
	}
}

// ExecuteRestore 执行系统恢复核心流程
func (m *RestoreManager) ExecuteRestore(ctx context.Context, jobID, backupRunID, userID string, getDest func(destID string) (destination.BackupDestination, error)) error {
	if !m.operation.TryLock() {
		return fmt.Errorf("已有恢复任务执行中")
	}
	defer m.operation.Unlock()
	job, err := m.store.GetRestoreJob(ctx, jobID)
	if err != nil {
		return err
	}

	run, err := m.store.GetRun(ctx, backupRunID)
	if err != nil {
		return fmt.Errorf("找不到待恢复的备份记录: %w", err)
	}

	// 1. 寻找可用存储目标读取备份包
	var srcDest destination.BackupDestination
	for _, rd := range run.Destinations {
		if rd.Status == StatusSuccess {
			if d, err := getDest(rd.DestinationID); err == nil {
				srcDest = d
				break
			}
		}
	}
	if srcDest == nil {
		return fmt.Errorf("未找到包含成功产物的存储目标")
	}

	// 更新任务状态为 RUNNING
	now := time.Now().UTC()
	job.Status = RestoreStatusRunning
	job.StartedAt = &now
	_ = m.store.UpdateRestoreJob(ctx, job)

	// 2. 核心安全防护：恢复前强制创建系统紧急快照 (Pre-Restore Emergency Backup)
	emergencyID := fmt.Sprintf("emergency-%s-%d", run.BackupID, time.Now().Unix())
	emerResult, err := CreateSnapshot(ctx, SnapshotConfig{
		BackupID:          emergencyID,
		ServerVersion:     "emergency",
		CADir:             m.caDir,
		SecretDir:         m.secretDir,
		ArtifactDir:       m.artDir,
		DBProvider:        m.dbProvider,
		WorkDir:           m.workDir,
		CompressionAlgo:   CompressZstd,
		CompressionLevel:  3,
		EncryptionEnabled: true,
		EncryptionKeyVer:  2,
		Crypto:            m.crypto,
	})
	if err != nil || emerResult == nil {
		if err == nil {
			err = fmt.Errorf("应急快照未生成")
		}
		m.failJob(ctx, job, "ERR_EMERGENCY_BACKUP", err.Error())
		return err
	}
	if err == nil && emerResult != nil {
		now := time.Now().UTC()
		mBytes, _ := json.Marshal(emerResult.Manifest)
		emergencyRunID := "run-" + emergencyID

		// 将应急快照归档持久化到本地备份目录 (/data/backups)
		localBackupDir := "/data/backups"
		allDests, _ := m.store.ListDestinations(ctx)
		var localDestID string
		for _, d := range allDests {
			if d.Type == DestTypeLocal && d.Enabled {
				localDestID = d.ID
				break
			}
		}
		if localDestID == "" {
			for _, d := range allDests {
				if d.Type == DestTypeLocal {
					localDestID = d.ID
					break
				}
			}
		}

		_ = os.MkdirAll(localBackupDir, 0o750)
		destFilePath := filepath.Join(localBackupDir, emergencyID+".backup")
		if copyErr := copyFile(emerResult.ArtifactPath, destFilePath); copyErr == nil {
			_ = os.Remove(emerResult.ArtifactPath)
		} else {
			destFilePath = emerResult.ArtifactPath
		}

		emergencyRun := &BackupRun{
			ID:           emergencyRunID,
			PolicyName:   "容灾还原前应急快照 (Emergency Backup)",
			BackupID:     emergencyID,
			Scope:        ScopeCenterFull,
			Status:       StatusSuccess,
			TriggerType:  "EMERGENCY",
			StartedAt:    &now,
			CompletedAt:  &now,
			DurationMS:   0,
			ArtifactSize: emerResult.ArtifactSize,
			FileCount:    emerResult.FileCount,
			Checksum:     emerResult.Checksum,
			ManifestJSON: mBytes,
			CreatedBy:    userID,
		}

		if createErr := m.store.CreateRun(ctx, emergencyRun); createErr == nil {
			if localDestID != "" {
				rd := &RunDestination{
					ID:            fmt.Sprintf("rd-%s-%s", emergencyRunID, localDestID),
					BackupRunID:   emergencyRunID,
					DestinationID: localDestID,
					Status:        StatusSuccess,
					RemotePath:    destFilePath,
					RemoteSize:    emerResult.ArtifactSize,
					UploadedAt:    &now,
					VerifiedAt:    &now,
				}
				_ = m.store.CreateRunDestination(ctx, rd)
			}
			job.EmergencyBackupRunID = emergencyRunID
			_ = m.store.UpdateRestoreJob(ctx, job)
		}
	}

	// 3. 读取备份文件流
	reader, err := srcDest.Get(ctx, run.BackupID)
	if err != nil {
		m.failJob(ctx, job, "ERR_READ_SOURCE", fmt.Sprintf("读取远程备份失败: %v", err))
		return err
	}
	defer reader.Close()

	// 4. 解密并解压
	job.Status = RestoreStatusRestoringDB
	_ = m.store.UpdateRestoreJob(ctx, job)

	if err := os.MkdirAll(m.workDir, 0700); err != nil {
		return err
	}
	stageDir, err := os.MkdirTemp(m.workDir, "restore-verified-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)
	fail := func(err error) error { m.failJob(ctx, job, "ERR_VERIFY_RESTORE", err.Error()); return err }
	raw, err := readBounded(reader, MaxArtifactBytes)
	if err != nil {
		return fail(err)
	}
	var expected Manifest
	if err = json.Unmarshal(run.ManifestJSON, &expected); err != nil || expected.Encryption.Algorithm == "" {
		return fail(fmt.Errorf("缺少可信备份清单记录，禁止猜测加密状态"))
	}
	manifest, err := unpackVerified(ctx, raw, run.Checksum, expected.Encryption.Algorithm, run.BackupID, stageDir, m.crypto)
	if err != nil {
		return fail(err)
	}
	// No data directory writes occur until every archive file has passed validation.
	for _, file := range manifest.Files {
		var base, relative string
		switch {
		case strings.HasPrefix(file.Path, "ca/"):
			base = m.caDir
			relative = strings.TrimPrefix(file.Path, "ca/")
		case strings.HasPrefix(file.Path, "secrets/"):
			base = m.secretDir
			relative = strings.TrimPrefix(file.Path, "secrets/")
		case strings.HasPrefix(file.Path, "artifacts/"):
			base = m.artDir
			relative = strings.TrimPrefix(file.Path, "artifacts/")
		}
		if relative != "" {
			if err = installVerified(base, filepath.FromSlash(relative), filepath.Join(stageDir, filepath.FromSlash(file.Path))); err != nil {
				return fail(err)
			}
		}
	}
	if m.dbProvider != nil {
		dbFile, err := os.Open(filepath.Join(stageDir, "database", "database.dump"))
		if err != nil {
			return fail(err)
		}
		err = m.dbProvider.Restore(ctx, dbFile)
		closeErr := dbFile.Close()
		if err != nil {
			return fail(err)
		}
		if closeErr != nil {
			return fail(closeErr)
		}
	}

	// 恢复成功
	completedAt := time.Now().UTC()
	job.Status = RestoreStatusSuccess
	job.CompletedAt = &completedAt
	return m.store.UpdateRestoreJob(ctx, job)
}

func (m *RestoreManager) failJob(ctx context.Context, job *RestoreJob, code, msg string) {
	completedAt := time.Now().UTC()
	job.Status = RestoreStatusFailed
	job.CompletedAt = &completedAt
	job.ErrorCode = code
	job.ErrorMessage = msg
	_ = m.store.UpdateRestoreJob(ctx, job)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
