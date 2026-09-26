package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	ErrInvalidCiphertext = errors.New("密文数据无效或已损坏")
	ErrDecryptionFailed  = errors.New("解密失败：密钥不匹配或数据已被篡改")
	ErrUnsupportedCrypto = errors.New("不支持的加密算法")
)

const (
	ArtifactMagic = "AIEBKP01" // 8 字节加密包文件魔数
)

// CryptoManager 提供凭证加解密与备份包流式加解密
type CryptoManager struct {
	masterKey []byte
}

// NewCryptoManager 初始化加密管理器
func NewCryptoManager(keyStr string) *CryptoManager {
	if keyStr == "" {
		keyStr = os.Getenv("AIE_MASTER_KEY")
	}
	if keyStr == "" {
		keyStr = os.Getenv("AIE_BACKUP_MASTER_KEY")
	}
	if keyStr == "" {
		// 缺省使用系统安全固化种子派生（保障单机环境可开箱即用，生产环境推荐环境变量注入）
		keyStr = "ai-employee-platform-default-master-key-v1"
	}
	h := sha256.Sum256([]byte(keyStr))
	return &CryptoManager{masterKey: h[:]}
}

// EncryptText 使用 AES-256-GCM 加密短文本（如 Destination 凭证）
func (c *CryptoManager) EncryptText(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(c.masterKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptText 解密短文本
func (c *CryptoManager) DecryptText(ciphertextBase64 string) (string, error) {
	if ciphertextBase64 == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(ciphertextBase64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(c.masterKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCiphertext
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrDecryptionFailed
	}
	return string(plaintext), nil
}

// EncryptArtifact 将输入数据进行条件加密写入输出目标。
// 若 enabled 为 false，则直接将明文流写入目标（满足用户关于支持不加密备份的要求）。
// 若 enabled 为 true，则写入文件头 [AIEBKP01][KeyVersion 4B][Nonce 12B]，后跟 AES-GCM 密文。
func (c *CryptoManager) EncryptArtifact(w io.Writer, r io.Reader, enabled bool, keyVersion int) (int64, error) {
	if !enabled {
		// 不加密：直接直通复制
		return io.Copy(w, r)
	}

	block, err := aes.NewCipher(c.masterKey)
	if err != nil {
		return 0, fmt.Errorf("创建 AES 密码器失败: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, fmt.Errorf("创建 GCM 模式失败: %w", err)
	}

	// 写入魔数 (8B)
	if _, err := w.Write([]byte(ArtifactMagic)); err != nil {
		return 0, err
	}
	// 写入密钥版本 (4B BigEndian)
	var verBuf [4]byte
	binary.BigEndian.PutUint32(verBuf[:], uint32(keyVersion))
	if _, err := w.Write(verBuf[:]); err != nil {
		return 0, err
	}
	// 生成并写入 Nonce (12B)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return 0, fmt.Errorf("生成随机 Nonce 失败: %w", err)
	}
	if _, err := w.Write(nonce); err != nil {
		return 0, err
	}

	// 读取全部输入并加密（Center Server 单机快照通常在数十 MB 到数 GB 之间）
	plainData, err := io.ReadAll(r)
	if err != nil {
		return 0, fmt.Errorf("读取快照明文流失败: %w", err)
	}

	cipherData := gcm.Seal(nil, nonce, plainData, []byte(ArtifactMagic))
	n, err := w.Write(cipherData)
	if err != nil {
		return 0, fmt.Errorf("写入加密快照失败: %w", err)
	}

	totalBytes := int64(len(ArtifactMagic) + 4 + len(nonce) + n)
	return totalBytes, nil
}

// DecryptArtifact 将备份包解密写入输出。
// 如果算法为 NONE 或数据不包含魔数，则作为明文流复制。
func (c *CryptoManager) DecryptArtifact(w io.Writer, r io.Reader, algorithm string) error {
	if algorithm == EncryptNone || algorithm == "" {
		_, err := io.Copy(w, r)
		return err
	}
	if algorithm != EncryptAES256GCM {
		return fmt.Errorf("%w: %s", ErrUnsupportedCrypto, algorithm)
	}

	// 检查头部魔数
	magicBuf := make([]byte, len(ArtifactMagic))
	if _, err := io.ReadFull(r, magicBuf); err != nil {
		return fmt.Errorf("读取文件头失败: %w", err)
	}
	if string(magicBuf) != ArtifactMagic {
		return fmt.Errorf("文件头魔数无效，非预期加密备份格式")
	}

	// 读取版本
	var verBuf [4]byte
	if _, err := io.ReadFull(r, verBuf[:]); err != nil {
		return fmt.Errorf("读取密钥版本失败: %w", err)
	}

	block, err := aes.NewCipher(c.masterKey)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(r, nonce); err != nil {
		return fmt.Errorf("读取 Nonce 失败: %w", err)
	}

	cipherData, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("读取密文流失败: %w", err)
	}

	plainData, err := gcm.Open(nil, nonce, cipherData, []byte(ArtifactMagic))
	if err != nil {
		return ErrDecryptionFailed
	}

	_, err = w.Write(plainData)
	return err
}
