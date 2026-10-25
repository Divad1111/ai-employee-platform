package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/ai-employee-platform/server/internal/backup/destination"
	"os"
)

var ErrChecksumMismatch = errors.New("备份校验失败：SHA-256 校验和不匹配")
var ErrManifestMissing = errors.New("备份缺少有效清单")

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

func VerifyArtifact(ctx context.Context, dest destination.BackupDestination, id, expected string, crypto *CryptoManager) (*VerificationReport, error) {
	reader, err := dest.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := readBounded(reader, MaxArtifactBytes)
	if err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp("", "aie-verify-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	report := &VerificationReport{BackupID: id, ExpectedHash: expected}
	sum := sha256.Sum256(raw)
	report.Checksum = hex.EncodeToString(sum[:])
	manifest, err := unpackVerified(ctx, raw, expected, "", id, stage, crypto)
	if err != nil {
		return report, err
	}
	report.Passed = true
	report.FileCount = len(manifest.Files)
	report.ServerVer = manifest.ServerVersion
	report.Encryption = manifest.Encryption.Algorithm
	report.Compression = manifest.Compression.Algorithm
	report.Details = []string{fmt.Sprintf("摘要、加密与全部 %d 个文件校验通过", report.FileCount)}
	return report, nil
}
