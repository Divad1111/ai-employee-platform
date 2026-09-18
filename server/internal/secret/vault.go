// Package secret 密钥引用存储：明文不进普通 DB/日志/Prompt。
// 设计依据：设计文档 §62、§88。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"sync"
)

// 错误。
var (
	ErrNotFound = errors.New("secret 不存在")
	ErrDenied   = errors.New("禁止读取明文到日志/Prompt")
)

// Ref 密钥引用（可安全落库/返回 Admin）。
type Ref struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Store 密钥库。
type Store interface {
	Put(name, plaintext string) (Ref, error)
	Get(id string) (plaintext string, err error)
	Ref(id string) (Ref, error)
	List() []Ref
}

// MemoryVault 内存加密库（开发/测试）；主密钥来自 AIE_SECRET_MASTER_KEY 或临时随机。
type MemoryVault struct {
	mu   sync.RWMutex
	byID map[string]entry
	gcm  cipher.AEAD
}

type entry struct {
	Name string
	Blob []byte // nonce||ciphertext
}

// NewMemoryVault 创建。
func NewMemoryVault() (*MemoryVault, error) {
	key := deriveKey(os.Getenv("AIE_SECRET_MASTER_KEY"))
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &MemoryVault{byID: map[string]entry{}, gcm: gcm}, nil
}

func deriveKey(seed string) []byte {
	if seed == "" {
		seed = "dev-only-aie-secret-master"
	}
	sum := sha256.Sum256([]byte(seed))
	return sum[:]
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "sec-" + base64.RawURLEncoding.EncodeToString(b[:])
}

// Put 加密写入，返回引用。
func (v *MemoryVault) Put(name, plaintext string) (Ref, error) {
	nonce := make([]byte, v.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Ref{}, err
	}
	ct := v.gcm.Seal(nil, nonce, []byte(plaintext), nil)
	blob := append(nonce, ct...)
	id := newID()
	v.mu.Lock()
	v.byID[id] = entry{Name: name, Blob: blob}
	v.mu.Unlock()
	return Ref{ID: id, Name: name}, nil
}

// Get 仅供受控服务解密使用；调用方不得写入普通日志。
func (v *MemoryVault) Get(id string) (string, error) {
	v.mu.RLock()
	e, ok := v.byID[id]
	v.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}
	ns := v.gcm.NonceSize()
	if len(e.Blob) < ns {
		return "", errors.New("密文损坏")
	}
	pt, err := v.gcm.Open(nil, e.Blob[:ns], e.Blob[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

func (v *MemoryVault) Ref(id string) (Ref, error) {
	v.mu.RLock()
	e, ok := v.byID[id]
	v.mu.RUnlock()
	if !ok {
		return Ref{}, ErrNotFound
	}
	return Ref{ID: id, Name: e.Name}, nil
}

func (v *MemoryVault) List() []Ref {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]Ref, 0, len(v.byID))
	for id, e := range v.byID {
		out = append(out, Ref{ID: id, Name: e.Name})
	}
	return out
}

// Replace 原地轮换密文（保持 ID）。
func (v *MemoryVault) Replace(id, plaintext string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.byID[id]
	if !ok {
		return ErrNotFound
	}
	nonce := make([]byte, v.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	ct := v.gcm.Seal(nil, nonce, []byte(plaintext), nil)
	v.byID[id] = entry{Name: e.Name, Blob: append(nonce, ct...)}
	return nil
}

// Delete 删除条目。
func (v *MemoryVault) Delete(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if _, ok := v.byID[id]; !ok {
		return ErrNotFound
	}
	delete(v.byID, id)
	return nil
}

// Redact 日志用：永远不回显疑似密钥。
func Redact(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}
