package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ai-employee-platform/server/internal/backup/database"
	"github.com/ai-employee-platform/server/internal/backup/destination"
)

// Service 备份业务综合服务入口
type Service struct {
	Store      Store
	Executor   *BackupExecutor
	Scheduler  *BackupScheduler
	RestoreMgr *RestoreManager
	Crypto     *CryptoManager
}

// Config 服务启动配置
type Config struct {
	CADir       string
	SecretDir   string
	ArtifactDir string
	WorkDir     string
	MasterKey   string
}

// NewService 初始化备份服务
func NewService(store Store, db database.DatabaseBackupProvider, cfg Config) *Service {
	crypto := NewCryptoManager(cfg.MasterKey)
	executor := NewBackupExecutor(store, db, crypto, cfg.CADir, cfg.SecretDir, cfg.ArtifactDir, cfg.WorkDir)
	restoreMgr := NewRestoreManager(store, db, crypto, cfg.CADir, cfg.SecretDir, cfg.ArtifactDir, cfg.WorkDir)

	s := &Service{
		Store:      store,
		Executor:   executor,
		RestoreMgr: restoreMgr,
		Crypto:     crypto,
	}

	scheduler := NewBackupScheduler(store, executor, s.GetDestination)
	s.Scheduler = scheduler
	return s
}

// GetDestination 根据存储目标 ID 解密凭据并实例化 BackupDestination 适配器
func (s *Service) GetDestination(destID string) (destination.BackupDestination, error) {
	d, err := s.Store.GetDestination(context.Background(), destID)
	if err != nil {
		return nil, fmt.Errorf("读取存储目标失败: %w", err)
	}

	rawConfigJSON := d.ConfigEncrypted
	if rawConfigJSON != "" {
		decrypted, err := s.Crypto.DecryptText(rawConfigJSON)
		if err == nil && decrypted != "" {
			rawConfigJSON = decrypted
		}
	}

	switch d.Type {
	case DestTypeLocal:
		var cfg LocalConfig
		if rawConfigJSON != "" {
			_ = json.Unmarshal([]byte(rawConfigJSON), &cfg)
		}
		return destination.NewLocalDestination(cfg.Path), nil

	case DestTypeS3:
		var cfg S3Config
		if rawConfigJSON != "" {
			_ = json.Unmarshal([]byte(rawConfigJSON), &cfg)
		}
		return destination.NewS3Destination(destination.S3Options{
			Endpoint:  cfg.Endpoint,
			Region:    cfg.Region,
			Bucket:    cfg.Bucket,
			Prefix:    cfg.Prefix,
			AccessKey: cfg.AccessKey,
			SecretKey: cfg.SecretKey,
			UseSSL:    cfg.UseSSL,
			PathStyle: cfg.PathStyle,
		})

	case DestTypeSFTP:
		var cfg SFTPConfig
		if rawConfigJSON != "" {
			_ = json.Unmarshal([]byte(rawConfigJSON), &cfg)
		}
		return destination.NewSFTPDestination(destination.SFTPDestOptions{
			Host:       cfg.Host,
			Port:       cfg.Port,
			Username:   cfg.Username,
			Password:   cfg.Password,
			PrivateKey: cfg.PrivateKey,
			RemotePath: cfg.RemotePath,
		}), nil

	case DestTypeSMB:
		var cfg SMBConfig
		if rawConfigJSON != "" {
			_ = json.Unmarshal([]byte(rawConfigJSON), &cfg)
		}
		return destination.NewSMBDestination(destination.SMBDestOptions{
			Server:     cfg.Server,
			Share:      cfg.Share,
			Username:   cfg.Username,
			Password:   cfg.Password,
			Domain:     cfg.Domain,
			RemotePath: cfg.RemotePath,
		}), nil

	default:
		return nil, fmt.Errorf("未知的存储目标类型: %s", d.Type)
	}
}

// TestDestination 测试存储目标端到端连通性
func (s *Service) TestDestination(ctx context.Context, destID string) error {
	destObj, err := s.GetDestination(destID)
	if err != nil {
		_ = s.Store.UpdateDestinationTestStatus(ctx, destID, "FAILED", err.Error())
		return err
	}
	testErr := destObj.TestConnection(ctx)
	if testErr != nil {
		_ = s.Store.UpdateDestinationTestStatus(ctx, destID, "FAILED", testErr.Error())
		return testErr
	}
	_ = s.Store.UpdateDestinationTestStatus(ctx, destID, "SUCCESS", "连通性测试通过（写与删除验证正常）")
	return nil
}

// RunPolicy 立即触发一次策略备份
func (s *Service) RunPolicy(ctx context.Context, policyID, userID string) (*BackupRun, error) {
	return s.Executor.Execute(ctx, RunOptions{
		PolicyID:    policyID,
		TriggerType: "MANUAL",
		CreatedBy:   userID,
	}, s.GetDestination)
}

// RunManualAdHoc 执行一次手动非策略绑定备份
func (s *Service) RunManualAdHoc(ctx context.Context, destIDs []string, encEnabled bool, userID string) (*BackupRun, error) {
	return s.Executor.Execute(ctx, RunOptions{
		TriggerType:       "MANUAL",
		CreatedBy:         userID,
		DestinationIDs:    destIDs,
		EncryptionEnabled: &encEnabled,
	}, s.GetDestination)
}

// VerifyRun 校验备份运行产物
func (s *Service) VerifyRun(ctx context.Context, runID string) (*VerificationReport, error) {
	run, err := s.Store.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	var dest destination.BackupDestination
	for _, rd := range run.Destinations {
		if rd.Status == StatusSuccess {
			if d, err := s.GetDestination(rd.DestinationID); err == nil {
				dest = d
				break
			}
		}
	}
	if dest == nil {
		return nil, errors.New("该备份没有可用的成功存储目标供校验")
	}

	return VerifyArtifact(ctx, dest, run.BackupID, run.Checksum, s.Crypto)
}

// DeleteRun 删除备份记录及其远端物理产物
func (s *Service) DeleteRun(ctx context.Context, runID string) error {
	run, err := s.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}

	for _, rd := range run.Destinations {
		if d, err := s.GetDestination(rd.DestinationID); err == nil {
			_ = d.Delete(ctx, run.BackupID)
		}
	}

	return s.Store.DeleteRun(ctx, runID)
}

// StartRestoreJob 发起恢复任务
func (s *Service) StartRestoreJob(ctx context.Context, backupRunID, userID string) (*RestoreJob, error) {
	jobID := fmt.Sprintf("restore-%d", time.Now().UnixNano())
	job := &RestoreJob{
		ID:          jobID,
		BackupRunID: backupRunID,
		Status:      RestoreStatusPending,
		CreatedBy:   userID,
	}

	if err := s.Store.CreateRestoreJob(ctx, job); err != nil {
		return nil, fmt.Errorf("创建恢复任务失败: %w", err)
	}

	// 异步执行容灾恢复
	go func() {
		_ = s.RestoreMgr.ExecuteRestore(context.Background(), jobID, backupRunID, userID, s.GetDestination)
	}()

	return job, nil
}
