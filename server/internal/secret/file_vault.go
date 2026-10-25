package secret

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileVault 开发/简易生产：磁盘 AES-GCM 加密文件（可插拔 Store）。
// 生产可替换为 OS Credential Store / 外部 Vault，接口不变。
type FileVault struct {
	mu    sync.Mutex
	path  string
	inner *MemoryVault
}

type filePersisted struct {
	Entries map[string]fileEntry `json:"entries"`
}

type fileEntry struct {
	Name string `json:"name"`
	Blob []byte `json:"blob"`
}

// NewFileVault 打开或创建文件库；主密钥环境变量同 MemoryVault。
func NewFileVault(dir string) (*FileVault, error) {
	if len(os.Getenv("AIE_SECRET_MASTER_KEY")) < 32 {
		return nil, fmt.Errorf("文件 Vault 必须配置至少 32 字符的稳定主密钥；旧数据请先迁移")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "secrets.vault")
	mv, err := NewMemoryVault()
	if err != nil {
		return nil, err
	}
	fv := &FileVault{path: path, inner: mv}
	if err := fv.load(); err != nil {
		return nil, err
	}
	return fv, nil
}

func (v *FileVault) load() error {
	b, err := os.ReadFile(v.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var p filePersisted
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	v.inner.mu.Lock()
	defer v.inner.mu.Unlock()
	for id, e := range p.Entries {
		nonceSize := v.inner.gcm.NonceSize()
		if len(e.Blob) < nonceSize {
			return fmt.Errorf("Secret Vault 密文损坏")
		}
		if _, err := v.inner.gcm.Open(nil, e.Blob[:nonceSize], e.Blob[nonceSize:], nil); err != nil {
			return fmt.Errorf("Secret Vault 主密钥不匹配，请先执行密钥迁移")
		}
		v.inner.byID[id] = entry{Name: e.Name, Blob: e.Blob}
	}
	return nil
}

func (v *FileVault) persistLocked() error {
	p := filePersisted{Entries: map[string]fileEntry{}}
	v.inner.mu.RLock()
	for id, e := range v.inner.byID {
		p.Entries[id] = fileEntry{Name: e.Name, Blob: append([]byte{}, e.Blob...)}
	}
	v.inner.mu.RUnlock()
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, v.path)
}

func (v *FileVault) Put(name, plaintext string) (Ref, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	ref, err := v.inner.Put(name, plaintext)
	if err != nil {
		return Ref{}, err
	}
	if err := v.persistLocked(); err != nil {
		_ = v.inner.Delete(ref.ID)
		return Ref{}, err
	}
	return ref, nil
}

func (v *FileVault) Encrypt(plaintext string) (string, error) { return v.inner.Encrypt(plaintext) }
func (v *FileVault) Get(id string) (string, error)            { return v.inner.Get(id) }
func (v *FileVault) Ref(id string) (Ref, error)               { return v.inner.Ref(id) }
func (v *FileVault) List() []Ref                              { return v.inner.List() }

func (v *FileVault) Replace(id, plaintext string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.inner.mu.RLock()
	old, exists := v.inner.byID[id]
	v.inner.mu.RUnlock()
	if err := v.inner.Replace(id, plaintext); err != nil {
		return err
	}
	if err := v.persistLocked(); err != nil {
		if exists {
			v.inner.mu.Lock()
			v.inner.byID[id] = old
			v.inner.mu.Unlock()
		}
		return err
	}
	return nil
}

func (v *FileVault) Delete(id string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.inner.mu.RLock()
	old, exists := v.inner.byID[id]
	v.inner.mu.RUnlock()
	if err := v.inner.Delete(id); err != nil {
		return err
	}
	if err := v.persistLocked(); err != nil {
		if exists {
			v.inner.mu.Lock()
			v.inner.byID[id] = old
			v.inner.mu.Unlock()
		}
		return err
	}
	return nil
}
