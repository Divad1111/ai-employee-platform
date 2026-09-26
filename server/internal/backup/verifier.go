package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/ai-employee-platform/server/internal/backup/destination"
	"github.com/klauspost/compress/zstd"
)

var (
	ErrChecksumMismatch = errors.New("备份校验失败：SHA-256 校验和不匹配")
	ErrManifestMissing  = errors.New("备份包中缺少 manifest.json 元数据描述")
)

// VerificationReport 校验报告
type VerificationReport struct {
	BackupID     string   `json:"backup_id"`
	Checksum     string   `json:"checksum"`
	ExpectedHash string   `json:"expected_hash"`
	Passed       bool     `json:"passed"`
	FileCount    int      `json:"file_count"`
	ServerVer    string   `json:"server_version"`
	Encryption   string   `json:"encryption"`
	Compression  string   `json:"compression"`
	Details      []string `json:"details"`
}

// VerifyArtifact 从存储目标流式下载并全面校验备份产物
func VerifyArtifact(ctx context.Context, dest destination.BackupDestination, backupID, expectedHash string, crypto *CryptoManager) (*VerificationReport, error) {
	reader, err := dest.Get(ctx, backupID)
	if err != nil {
		return nil, fmt.Errorf("从存储目标读取备份失败: %w", err)
	}
	defer reader.Close()

	// 1. 全文计算 SHA-256，同时暂存到 buffer 中用于解包探测
	hasher := sha256.New()
	buf := new(bytes.Buffer)
	mw := io.MultiWriter(hasher, buf)

	if _, err := io.Copy(mw, reader); err != nil {
		return nil, fmt.Errorf("流式读取备份产物失败: %w", err)
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	report := &VerificationReport{
		BackupID:     backupID,
		Checksum:     actualHash,
		ExpectedHash: expectedHash,
		Details:      make([]string, 0),
	}

	if expectedHash != "" && actualHash != expectedHash {
		report.Passed = false
		report.Details = append(report.Details, fmt.Sprintf("哈希校验不匹配: 实际 %s != 预期 %s", actualHash, expectedHash))
		return report, ErrChecksumMismatch
	}

	report.Details = append(report.Details, "SHA-256 校验和完全匹配")

	// 2. 尝试解密（若为加密包）
	decryptedBuf := new(bytes.Buffer)
	if crypto == nil {
		crypto = NewCryptoManager("")
	}

	// 优先尝试 AES-256-GCM 解密，若非加密包则直通
	dataBytes := buf.Bytes()
	isEncrypted := len(dataBytes) > len(ArtifactMagic) && string(dataBytes[:len(ArtifactMagic)]) == ArtifactMagic

	if isEncrypted {
		report.Encryption = EncryptAES256GCM
		if err := crypto.DecryptArtifact(decryptedBuf, bytes.NewReader(dataBytes), EncryptAES256GCM); err != nil {
			report.Passed = false
			report.Details = append(report.Details, fmt.Sprintf("解密失败: %v", err))
			return report, err
		}
		report.Details = append(report.Details, "AES-256-GCM 解密成功，魔数与 Nonce 验证通过")
	} else {
		report.Encryption = EncryptNone
		decryptedBuf.Write(dataBytes)
		report.Details = append(report.Details, "检测到未加密归档包，跳过解密流程")
	}

	// 3. Zstd 解压缩
	zstdReader, err := zstd.NewReader(decryptedBuf)
	if err != nil {
		report.Passed = false
		report.Details = append(report.Details, fmt.Sprintf("Zstd 解压失败: %v", err))
		return report, err
	}
	defer zstdReader.Close()

	// 4. Tar 遍历读取 Manifest
	tarReader := tar.NewReader(zstdReader)
	var manifestFound bool
	var fileCount int

	for {
		hdr, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			report.Passed = false
			report.Details = append(report.Details, fmt.Sprintf("读取 Tar 归档头失败: %v", err))
			return report, err
		}

		fileCount++
		if hdr.Name == "manifest.json" {
			var mf Manifest
			if err := json.NewDecoder(tarReader).Decode(&mf); err == nil {
				manifestFound = true
				report.ServerVer = mf.ServerVersion
				report.Compression = mf.Compression.Algorithm
				report.Details = append(report.Details, fmt.Sprintf("成功读取 Manifest: 格式版本 v%d, 备份类型 %s, 文件数 %d", mf.FormatVersion, mf.BackupType, len(mf.Files)))
			}
		}
	}

	if !manifestFound {
		report.Passed = false
		report.Details = append(report.Details, "归档内未找到有效 manifest.json")
		return report, ErrManifestMissing
	}

	report.FileCount = fileCount
	report.Passed = true
	report.Details = append(report.Details, fmt.Sprintf("完整性校验成功：全部 %d 个文件结构完好", fileCount))
	return report, nil
}
