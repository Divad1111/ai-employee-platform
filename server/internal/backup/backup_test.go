package backup

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/backup/destination"
)

func TestCryptoManager_Text(t *testing.T) {
	cm := NewCryptoManager("test-secret-key-32-bytes-long!")
	plaintext := "S3_Super_Secret_Password_12345"
	encrypted, err := cm.EncryptText(plaintext)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if encrypted == plaintext {
		t.Fatalf("加密结果不应与明文相同")
	}

	decrypted, err := cm.DecryptText(encrypted)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("解密结果不匹配: got %q, want %q", decrypted, plaintext)
	}
}

func TestCryptoManager_Artifact_Encrypted(t *testing.T) {
	cm := NewCryptoManager("test-key")
	data := []byte("hello world backup payload content")

	// 加密写入
	encBuf := new(bytes.Buffer)
	n, err := cm.EncryptArtifact(encBuf, bytes.NewReader(data), true, 1)
	if err != nil {
		t.Fatalf("加密备份包失败: %v", err)
	}
	if n <= int64(len(data)) {
		t.Fatalf("加密结果长度异常: %d", n)
	}

	// 解密还原
	decBuf := new(bytes.Buffer)
	err = cm.DecryptArtifact(decBuf, bytes.NewReader(encBuf.Bytes()), EncryptAES256GCM)
	if err != nil {
		t.Fatalf("解密备份包失败: %v", err)
	}
	if string(decBuf.Bytes()) != string(data) {
		t.Fatalf("解密还原不一致: got %q, want %q", string(decBuf.Bytes()), string(data))
	}
}

func TestCryptoManager_Artifact_Unencrypted(t *testing.T) {
	cm := NewCryptoManager("test-key")
	data := []byte("plain unencrypted backup payload")

	// 不加密写入
	buf := new(bytes.Buffer)
	n, err := cm.EncryptArtifact(buf, bytes.NewReader(data), false, 1)
	if err != nil {
		t.Fatalf("写入未加密包失败: %v", err)
	}
	if n != int64(len(data)) {
		t.Fatalf("未加密模式应为直通明文大小: got %d, want %d", n, len(data))
	}

	// 解密读取（NONE 模式直通）
	decBuf := new(bytes.Buffer)
	err = cm.DecryptArtifact(decBuf, bytes.NewReader(buf.Bytes()), EncryptNone)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if string(decBuf.Bytes()) != string(data) {
		t.Fatalf("结果不匹配: got %q", string(decBuf.Bytes()))
	}
}

func TestLocalDestination(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aie_backup_local_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	loc := destination.NewLocalDestination(tmpDir)
	ctx := context.Background()

	// 1. 测试连通性
	if err := loc.TestConnection(ctx); err != nil {
		t.Fatalf("本地目标连通性测试失败: %v", err)
	}

	// 2. 上传文件
	content := "test-artifact-data"
	remotePath, err := loc.Put(ctx, "bkp-001", strings.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	if !strings.HasSuffix(remotePath, "bkp-001.backup") {
		t.Fatalf("预期文件路径以 .backup 结尾: %s", remotePath)
	}

	// 3. 检查存在
	exists, err := loc.Exists(ctx, "bkp-001")
	if err != nil || !exists {
		t.Fatalf("Exists 预期为 true, got %v, err=%v", exists, err)
	}

	// 4. 读取
	rc, err := loc.Get(ctx, "bkp-001")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	readBytes, _ := io.ReadAll(rc)
	rc.Close()
	if string(readBytes) != content {
		t.Fatalf("读取内容不一致: got %s, want %s", string(readBytes), content)
	}

	// 5. 列表
	list, err := loc.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List 预期 1 个对象，got %d (err=%v)", len(list), err)
	}
	if list[0].BackupID != "bkp-001" {
		t.Fatalf("List BackupID 不匹配: %s", list[0].BackupID)
	}

	// 6. 删除
	if err := loc.Delete(ctx, "bkp-001"); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	exists, _ = loc.Exists(ctx, "bkp-001")
	if exists {
		t.Fatalf("删除后对象不应存在")
	}
}

func TestSnapshot_Creation(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "aie_snapshot_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	caDir := filepath.Join(tmpDir, "ca")
	_ = os.MkdirAll(caDir, 0o750)
	_ = os.WriteFile(filepath.Join(caDir, "ca.crt"), []byte("test-ca-cert"), 0o600)

	workDir := filepath.Join(tmpDir, "work")
	_ = os.MkdirAll(workDir, 0o750)

	ctx := context.Background()

	// 1. 创建未加密快照
	resPlain, err := CreateSnapshot(ctx, SnapshotConfig{
		BackupID:          "bkp-plain-001",
		ServerVersion:     "v1.0.0",
		CADir:             caDir,
		WorkDir:           workDir,
		EncryptionEnabled: false, // 不加密
		Crypto:            NewCryptoManager("test-key"),
	})
	if err != nil {
		t.Fatalf("创建未加密快照失败: %v", err)
	}
	if resPlain.Checksum == "" || resPlain.ArtifactSize == 0 {
		t.Fatalf("快照产物尺寸或校验和为空")
	}
	if resPlain.Manifest.Encryption.Algorithm != EncryptNone {
		t.Fatalf("未加密快照的 Manifest 算法应为 NONE: %s", resPlain.Manifest.Encryption.Algorithm)
	}

	// 2. 创建加密快照
	resEnc, err := CreateSnapshot(ctx, SnapshotConfig{
		BackupID:          "bkp-enc-001",
		ServerVersion:     "v1.0.0",
		CADir:             caDir,
		WorkDir:           workDir,
		EncryptionEnabled: true, // 加密
		EncryptionKeyVer:  1,
		Crypto:            NewCryptoManager("test-key"),
	})
	if err != nil {
		t.Fatalf("创建加密快照失败: %v", err)
	}
	if resEnc.Manifest.Encryption.Algorithm != EncryptAES256GCM {
		t.Fatalf("加密快照的 Manifest 算法应为 AES-256-GCM: %s", resEnc.Manifest.Encryption.Algorithm)
	}
}
