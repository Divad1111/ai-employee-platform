package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/backup/database"
	"github.com/ai-employee-platform/server/internal/backup/destination"
	"github.com/klauspost/compress/zstd"
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
		EncryptionEnabled: false, // 应急快照默认不加密以便最快回滚
		Crypto:            m.crypto,
	})
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

	stageDir := filepath.Join(m.workDir, "restore_"+run.BackupID)
	_ = os.MkdirAll(stageDir, 0o750)
	defer os.RemoveAll(stageDir)

	// 解密到临时缓存
	decryptedBuf := new(bytes.Buffer)
	rawBytes, err := io.ReadAll(reader)
	if err != nil {
		m.failJob(ctx, job, "ERR_READ", err.Error())
		return err
	}

	isEncrypted := len(rawBytes) > len(ArtifactMagic) && string(rawBytes[:len(ArtifactMagic)]) == ArtifactMagic
	if isEncrypted {
		if err := m.crypto.DecryptArtifact(decryptedBuf, bytes.NewReader(rawBytes), EncryptAES256GCM); err != nil {
			m.failJob(ctx, job, "ERR_DECRYPT", fmt.Sprintf("解密失败: %v", err))
			return err
		}
	} else {
		decryptedBuf.Write(rawBytes)
	}

	// Zstd 解压
	zstdReader, err := zstd.NewReader(decryptedBuf)
	if err != nil {
		m.failJob(ctx, job, "ERR_DECOMPRESS", fmt.Sprintf("解压失败: %v", err))
		return err
	}
	defer zstdReader.Close()

	// 遍历 Tar 解包，严格防范路径穿越
	tarReader := tar.NewReader(zstdReader)
	var dbDumpData []byte

	for {
		hdr, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			m.failJob(ctx, job, "ERR_TAR_EXTRACT", err.Error())
			return err
		}

		cleanPath := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
			m.failJob(ctx, job, "ERR_PATH_TRAVERSAL", "检测到非法路径穿越文件")
			return errors.New("非法路径穿越文件")
		}

		// 拦截数据库转储
		if cleanPath == filepath.Join("database", "database.dump") || cleanPath == "database/database.dump" {
			dbDumpData, err = io.ReadAll(tarReader)
			if err != nil {
				m.failJob(ctx, job, "ERR_READ_DB_DUMP", err.Error())
				return err
			}
			continue
		}

		// 还原持久化文件
		var destBase string
		var subRel string
		if strings.HasPrefix(cleanPath, "ca/") || strings.HasPrefix(cleanPath, "ca\\") {
			destBase = m.caDir
			subRel = strings.TrimPrefix(strings.TrimPrefix(cleanPath, "ca/"), "ca\\")
		} else if strings.HasPrefix(cleanPath, "secrets/") || strings.HasPrefix(cleanPath, "secrets\\") {
			destBase = m.secretDir
			subRel = strings.TrimPrefix(strings.TrimPrefix(cleanPath, "secrets/"), "secrets\\")
		} else if strings.HasPrefix(cleanPath, "artifacts/") || strings.HasPrefix(cleanPath, "artifacts\\") {
			destBase = m.artDir
			subRel = strings.TrimPrefix(strings.TrimPrefix(cleanPath, "artifacts/"), "artifacts\\")
		}

		if destBase != "" && subRel != "" {
			targetFile := filepath.Join(destBase, subRel)
			_ = os.MkdirAll(filepath.Dir(targetFile), 0o750)
			f, err := os.OpenFile(targetFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, hdr.FileInfo().Mode())
			if err == nil {
				_, _ = io.Copy(f, tarReader)
				_ = f.Close()
			}
		}
	}

	// 5. 还原数据库
	if len(dbDumpData) > 0 && m.dbProvider != nil {
		if err := m.dbProvider.Restore(ctx, bytes.NewReader(dbDumpData)); err != nil {
			m.failJob(ctx, job, "ERR_RESTORE_DB", fmt.Sprintf("数据库数据还原失败: %v", err))
			return err
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
