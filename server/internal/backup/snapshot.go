package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/backup/database"
	"github.com/klauspost/compress/zstd"
)

// SnapshotConfig 快照打包配置
type SnapshotConfig struct {
	BackupID          string
	ServerVersion     string
	CADir             string
	SecretDir         string
	ArtifactDir       string
	DBProvider        database.DatabaseBackupProvider
	WorkDir           string
	CompressionAlgo   string
	CompressionLevel  int
	EncryptionEnabled bool // 是否加密开关
	EncryptionKeyVer  int
	Crypto            *CryptoManager
}

// SnapshotResult 打包完成结果
type SnapshotResult struct {
	BackupID     string
	ArtifactPath string
	ArtifactSize int64
	FileCount    int
	Checksum     string // 最终产物 SHA-256
	Manifest     Manifest
}

// CreateSnapshot 执行 Center Server 全量快照构建
func CreateSnapshot(ctx context.Context, cfg SnapshotConfig) (*SnapshotResult, error) {
	if cfg.WorkDir == "" {
		cfg.WorkDir = "/data/backup_work"
	}
	stageDir := filepath.Join(cfg.WorkDir, "stage_"+cfg.BackupID)
	if err := os.MkdirAll(stageDir, 0o750); err != nil {
		return nil, fmt.Errorf("创建快照临时目录失败: %w", err)
	}
	defer os.RemoveAll(stageDir) // 最终清理临时暂存目录

	var manifestFiles []ManifestFile

	// 1. 导出数据库
	dbDir := filepath.Join(stageDir, "database")
	_ = os.MkdirAll(dbDir, 0o750)
	dbDumpPath := filepath.Join(dbDir, "database.dump")
	dbFile, err := os.OpenFile(dbDumpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建数据库转储文件失败: %w", err)
	}
	if cfg.DBProvider != nil {
		if err := cfg.DBProvider.Backup(ctx, dbFile); err != nil {
			_ = dbFile.Close()
			return nil, fmt.Errorf("导出数据库快照失败: %w", err)
		}
	}
	_ = dbFile.Close()

	if mf, err := recordFileManifest(stageDir, dbDumpPath); err == nil {
		manifestFiles = append(manifestFiles, mf)
	}

	// 2. 复制 CA 目录
	if cfg.CADir != "" {
		caFiles, _ := copyDirIntoStage(cfg.CADir, filepath.Join(stageDir, "ca"), stageDir)
		manifestFiles = append(manifestFiles, caFiles...)
	}

	// 3. 复制 Secrets 目录
	if cfg.SecretDir != "" {
		secFiles, _ := copyDirIntoStage(cfg.SecretDir, filepath.Join(stageDir, "secrets"), stageDir)
		manifestFiles = append(manifestFiles, secFiles...)
	}

	// 4. 复制 Artifacts 目录
	if cfg.ArtifactDir != "" {
		artFiles, _ := copyDirIntoStage(cfg.ArtifactDir, filepath.Join(stageDir, "artifacts"), stageDir)
		manifestFiles = append(manifestFiles, artFiles...)
	}

	// 5. 生成 manifest.json
	encAlgo := EncryptNone
	if cfg.EncryptionEnabled {
		encAlgo = EncryptAES256GCM
	}
	compAlgo := CompressZstd
	if cfg.CompressionAlgo == CompressGzip {
		compAlgo = CompressGzip
	}

	manifest := Manifest{
		FormatVersion: FormatVersion,
		BackupID:      cfg.BackupID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		BackupType:    ScopeCenterFull,
		ServerVersion: cfg.ServerVersion,
		DatabaseType:  "postgresql",
		Encryption: EncryptionInfo{
			Algorithm:  encAlgo,
			KeyVersion: cfg.EncryptionKeyVer,
		},
		Compression: CompressionInfo{
			Algorithm: compAlgo,
			Level:     cfg.CompressionLevel,
		},
		Files: manifestFiles,
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("序列化 Manifest 失败: %w", err)
	}
	manifestPath := filepath.Join(stageDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o600); err != nil {
		return nil, fmt.Errorf("写入 Manifest 失败: %w", err)
	}

	// 6. 打包为 Tar 流并使用 Zstd 压缩
	tarBuf := new(bytes.Buffer)
	zstdWriter, err := zstd.NewWriter(tarBuf, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("初始化 Zstd 压缩器失败: %w", err)
	}
	tarWriter := tar.NewWriter(zstdWriter)

	err = filepath.Walk(stageDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(stageDir, path)
		if err != nil {
			return err
		}
		rel = strings.ReplaceAll(rel, "\\", "/")

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tarWriter.WriteHeader(hdr); err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tarWriter, f)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("Tar 打包失败: %w", err)
	}

	_ = tarWriter.Close()
	_ = zstdWriter.Close()

	// 7. 写入最终输出文件（经过条件加密）
	finalArtifactPath := filepath.Join(cfg.WorkDir, cfg.BackupID+".backup")
	outFile, err := os.OpenFile(finalArtifactPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("创建备份产物文件失败: %w", err)
	}
	defer outFile.Close()

	hasher := sha256.New()
	mw := io.MultiWriter(outFile, hasher)

	crypto := cfg.Crypto
	if crypto == nil {
		crypto = NewCryptoManager("")
	}

	written, err := crypto.EncryptArtifact(mw, tarBuf, cfg.EncryptionEnabled, cfg.EncryptionKeyVer)
	if err != nil {
		_ = os.Remove(finalArtifactPath)
		return nil, fmt.Errorf("加密/写入产物失败: %w", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	return &SnapshotResult{
		BackupID:     cfg.BackupID,
		ArtifactPath: finalArtifactPath,
		ArtifactSize: written,
		FileCount:    len(manifestFiles) + 1, // 加上 manifest.json 本身
		Checksum:     checksum,
		Manifest:     manifest,
	}, nil
}

func recordFileManifest(stageDir, filePath string) (ManifestFile, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return ManifestFile{}, err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return ManifestFile{}, err
	}
	defer f.Close()

	h := sha256.New()
	_, _ = io.Copy(h, f)

	rel, _ := filepath.Rel(stageDir, filePath)
	return ManifestFile{
		Path:   strings.ReplaceAll(rel, "\\", "/"),
		Size:   info.Size(),
		SHA256: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func copyDirIntoStage(srcDir, destDir, stageDir string) ([]ManifestFile, error) {
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return nil, nil
	}
	var res []ManifestFile
	_ = os.MkdirAll(destDir, 0o750)

	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(destDir, rel)

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0o750)
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer out.Close()

		hasher := sha256.New()
		mw := io.MultiWriter(out, hasher)
		if _, err := io.Copy(mw, in); err != nil {
			return err
		}

		stageRel, _ := filepath.Rel(stageDir, targetPath)
		res = append(res, ManifestFile{
			Path:   strings.ReplaceAll(stageRel, "\\", "/"),
			Size:   info.Size(),
			SHA256: hex.EncodeToString(hasher.Sum(nil)),
		})
		return nil
	})

	return res, err
}
