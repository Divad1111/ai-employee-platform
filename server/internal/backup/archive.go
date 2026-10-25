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
	"github.com/klauspost/compress/zstd"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func unpackVerified(ctx context.Context, raw []byte, expectedHash, expectedAlgorithm, backupID, stage string, crypto *CryptoManager) (*Manifest, error) {
	sum := sha256.Sum256(raw)
	if len(expectedHash) != 64 || !strings.EqualFold(hex.EncodeToString(sum[:]), expectedHash) {
		return nil, ErrChecksumMismatch
	}
	encrypted := bytes.HasPrefix(raw, []byte(ArtifactMagic))
	if expectedAlgorithm != "" && (expectedAlgorithm == EncryptAES256GCM) != encrypted {
		return nil, fmt.Errorf("备份加密状态与服务端记录不一致")
	}
	if expectedAlgorithm != "" && expectedAlgorithm != EncryptAES256GCM && expectedAlgorithm != EncryptNone {
		return nil, ErrUnsupportedCrypto
	}
	var content bytes.Buffer
	if encrypted {
		if crypto == nil {
			return nil, ErrDecryptionFailed
		}
		if err := crypto.DecryptArtifact(&boundedWriter{&content, MaxArtifactBytes}, bytes.NewReader(raw), EncryptAES256GCM); err != nil {
			return nil, err
		}
	} else {
		content.Write(raw)
	}
	decoder, err := zstd.NewReader(&content, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(64<<20), zstd.WithDecoderMaxWindow(32<<20))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	tr := tar.NewReader(io.LimitReader(decoder, MaxExpandedBytes+(16<<20)))
	actual := map[string]ManifestFile{}
	var manifest *Manifest
	var total int64
	count := 0
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		h, readErr := tr.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
		count++
		if count > MaxArchiveFiles {
			return nil, fmt.Errorf("备份文件数量超过上限")
		}
		name := h.Name
		if name == "" || name != path.Clean(name) || strings.Contains(name, "\\") || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("非法归档路径")
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > MaxArtifactBytes {
			return nil, fmt.Errorf("备份只允许有界普通文件")
		}
		total += h.Size
		if total > MaxExpandedBytes {
			return nil, fmt.Errorf("解压总大小超过上限")
		}
		if _, exists := actual[name]; exists {
			return nil, fmt.Errorf("重复归档文件")
		}
		if name != "manifest.json" && !strings.HasPrefix(name, "database/") && !strings.HasPrefix(name, "ca/") && !strings.HasPrefix(name, "secrets/") && !strings.HasPrefix(name, "artifacts/") {
			return nil, fmt.Errorf("不支持的归档文件")
		}
		if name == "manifest.json" {
			if manifest != nil || h.Size > 2<<20 {
				return nil, ErrManifestMissing
			}
			var m Manifest
			b, err := readBounded(tr, 2<<20)
			if err != nil {
				return nil, err
			}
			if err = json.Unmarshal(b, &m); err != nil {
				return nil, err
			}
			manifest = &m
			continue
		}
		target := filepath.Join(stage, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		n, copyErr := io.CopyN(io.MultiWriter(f, hash), tr, h.Size)
		closeErr := f.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		actual[name] = ManifestFile{Path: name, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil))}
	}
	if manifest == nil || manifest.FormatVersion != FormatVersion || manifest.BackupID != backupID {
		return nil, ErrManifestMissing
	}
	algorithm := EncryptNone
	if encrypted {
		algorithm = EncryptAES256GCM
	}
	if manifest.Encryption.Algorithm != algorithm {
		return nil, fmt.Errorf("归档清单加密算法不一致")
	}
	if len(manifest.Files) != len(actual) {
		return nil, fmt.Errorf("清单文件数量不一致")
	}
	listed := map[string]bool{}
	for _, mf := range manifest.Files {
		got, ok := actual[mf.Path]
		if !ok || listed[mf.Path] || got.Size != mf.Size || !strings.EqualFold(got.SHA256, mf.SHA256) {
			return nil, fmt.Errorf("归档清单文件校验失败")
		}
		listed[mf.Path] = true
	}
	if !listed["database/database.dump"] {
		return nil, fmt.Errorf("备份缺少数据库转储")
	}
	return manifest, nil
}

// installVerified writes only verified regular files, refusing symlink parents and targets.
func installVerified(base, relative, source string) error {
	if base == "" {
		return fmt.Errorf("恢复目标目录未配置")
	}
	clean := filepath.Clean(relative)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("非法恢复路径")
	}
	current := base
	if err := os.MkdirAll(base, 0750); err != nil {
		return err
	}
	info, err := os.Lstat(base)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("恢复目标不允许符号链接")
	}
	for _, part := range strings.Split(filepath.Dir(clean), string(filepath.Separator)) {
		if part == "." {
			continue
		}
		current = filepath.Join(current, part)
		if err = os.Mkdir(current, 0750); err != nil && !os.IsExist(err) {
			return err
		}
		info, err = os.Lstat(current)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("恢复父目录不安全")
		}
	}
	target := filepath.Join(base, clean)
	if info, err = os.Lstat(target); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("恢复目标不是普通文件")
	}
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), ".restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, copyErr := io.Copy(tmp, src)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), target)
}
