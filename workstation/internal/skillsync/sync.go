// Package skillsync 将技能包同步到本机 Cursor skills 目录。
package skillsync

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
)

// TargetDir 返回技能安装根目录。
func TargetDir() string {
	if d := os.Getenv("CURSOR_SKILLS_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".cursor", "skills")
	}
	return filepath.Join(home, ".cursor", "skills")
}

// Result 同步结果。
type Result struct {
	Installed []string `json:"installed"`
	Updated   []string `json:"updated"`
	Skipped   []string `json:"skipped"`
	Removed   []string `json:"removed"`
}

// Sync 批量落盘技能包；按 content_hash 跳过未变包；prune 托管文件但保留 workspace/ 与 config.json。
func Sync(packages []*aiev1.SkillPackage, targetDir string, pruneNames []string) (*Result, error) {
	if targetDir == "" {
		targetDir = TargetDir()
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return nil, err
	}
	res := &Result{}
	keep := map[string]bool{}
	for _, pkg := range packages {
		if pkg == nil || pkg.CursorName == "" {
			continue
		}
		keep[pkg.CursorName] = true
		dir := filepath.Join(targetDir, pkg.CursorName)
		hashFile := filepath.Join(dir, ".aie_content_hash")
		if existing, err := os.ReadFile(hashFile); err == nil && strings.TrimSpace(string(existing)) == pkg.ContentHash && pkg.ContentHash != "" {
			res.Skipped = append(res.Skipped, pkg.CursorName)
			continue
		}
		existed := false
		if _, err := os.Stat(dir); err == nil {
			existed = true
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return res, err
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(pkg.SkillMd), 0o644); err != nil {
			return res, err
		}
		managed := map[string]bool{"SKILL.md": true, ".aie_content_hash": true}
		for _, f := range pkg.Files {
			if f == nil || f.Path == "" {
				continue
			}
			rel := filepath.Clean(f.Path)
			if strings.HasPrefix(rel, "..") {
				continue
			}
			full := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return res, err
			}
			mode := os.FileMode(0o644)
			if f.Mode != 0 {
				mode = os.FileMode(f.Mode)
			}
			if err := os.WriteFile(full, f.Content, mode); err != nil {
				return res, err
			}
			managed[filepath.ToSlash(rel)] = true
		}
		_ = pruneManaged(dir, managed)
		hash := pkg.ContentHash
		if hash == "" {
			sum := sha256.Sum256([]byte(pkg.SkillMd))
			hash = hex.EncodeToString(sum[:])
		}
		_ = os.WriteFile(hashFile, []byte(hash), 0o644)
		if existed {
			res.Updated = append(res.Updated, pkg.CursorName)
		} else {
			res.Installed = append(res.Installed, pkg.CursorName)
		}
	}
	for _, name := range pruneNames {
		if keep[name] {
			continue
		}
		dir := filepath.Join(targetDir, name)
		if err := os.RemoveAll(dir); err == nil {
			res.Removed = append(res.Removed, name)
		}
	}
	return res, nil
}

// pruneManaged 删除不在托管清单内的文件，但保留 workspace/ 与 config.json。
func pruneManaged(dir string, managed map[string]bool) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return nil
		}
		slash := filepath.ToSlash(rel)
		base := filepath.Base(path)
		if base == "workspace" || strings.HasPrefix(slash, "workspace/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if base == "config.json" {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if managed[slash] {
			return nil
		}
		// 不删除 .aie_content_hash
		if base == ".aie_content_hash" {
			return nil
		}
		return os.Remove(path)
	})
}

// SyncFromStartJob 从 StartJobPayload 同步技能包。
func SyncFromStartJob(start *aiev1.StartJobPayload) (*Result, error) {
	if start == nil || len(start.SkillPackages) == 0 {
		return &Result{}, nil
	}
	return Sync(start.SkillPackages, "", nil)
}
