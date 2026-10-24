package workflowmcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxAssetBytes = 16 << 20

var reservedWindowsName = regexp.MustCompile(`^(COM|LPT)[1-9]$`)

// Older ZIP imports stored directory entries as zero-byte files. Keep those
// records untouched, but never expose or validate them as actual assets.
func isLegacySkillDirectory(f SkillFile) bool {
	return strings.HasSuffix(f.Path, "/") && f.Size == 0 && len(f.Content) == 0 && f.ContentB64 == "" && ValidateAssetPath(strings.TrimSuffix(f.Path, "/")) == nil
}

// AssetPatch is an explicit, atomic change set. Absent paths are untouched.
type AssetPatch struct {
	Revision int64
	Writes   []SkillFile
	Deletes  []string
}

func ValidateAssetPath(p string) error {
	if p == "" || len(p) > 1024 || path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\:") {
		return fmt.Errorf("%w: 非法文件路径 %q", ErrInvalidInput, p)
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." || strings.TrimSpace(part) == "" || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return fmt.Errorf("%w: 非法文件路径 %q", ErrInvalidInput, p)
		}
		if strings.ContainsAny(part, "<>\"|?*") || strings.ContainsFunc(part, unicode.IsControl) {
			return fmt.Errorf("%w: 非法文件路径 %q", ErrInvalidInput, p)
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || reservedWindowsName.MatchString(base) {
			return fmt.Errorf("%w: 保留文件名 %q", ErrInvalidInput, p)
		}
	}
	if strings.EqualFold(p, "SKILL.md") && p != "SKILL.md" {
		return fmt.Errorf("%w: 主文件名必须为 SKILL.md", ErrInvalidInput)
	}
	if p == ".aie_content_hash" {
		return fmt.Errorf("%w: 同步标记由系统管理", ErrInvalidInput)
	}
	return nil
}

func fileWithHash(p string, content []byte) SkillFile {
	// A nil byte slice is encoded as SQL NULL; empty files must remain BYTEA ''.
	if content == nil {
		content = []byte{}
	}
	sum := sha256.Sum256(content)
	return SkillFile{Path: p, Content: content, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}
}

// diffSkillFiles also validates collisions that cannot be installed on Windows.
func diffSkillFiles(old, desired []SkillFile) ([]SkillFile, []string, error) {
	byPath := map[string]SkillFile{}
	for _, f := range old {
		if isLegacySkillDirectory(f) {
			continue
		}
		byPath[f.Path] = f
	}
	seen := map[string]bool{}
	writes := []SkillFile{}
	for _, f := range desired {
		if isLegacySkillDirectory(f) {
			continue
		}
		if err := ValidateAssetPath(f.Path); err != nil {
			return nil, nil, err
		}
		key := strings.ToLower(f.Path)
		if seen[key] {
			return nil, nil, fmt.Errorf("%w: 文件路径重复 %s", ErrInvalidInput, f.Path)
		}
		seen[key] = true
		if previous, ok := byPath[f.Path]; !ok || !bytes.Equal(previous.Content, f.Content) {
			if len(f.Content) > MaxAssetBytes {
				return nil, nil, fmt.Errorf("%w: 单文件不能超过 16 MiB", ErrInvalidInput)
			}
			writes = append(writes, fileWithHash(f.Path, f.Content))
		}
		delete(byPath, f.Path)
	}
	for key := range seen {
		for dir := path.Dir(key); dir != "."; dir = path.Dir(dir) {
			if seen[dir] || dir == "skill.md" {
				return nil, nil, fmt.Errorf("%w: 文件与目录路径冲突", ErrInvalidInput)
			}
		}
	}
	deletes := []string{}
	for p := range byPath {
		deletes = append(deletes, p)
	}
	return writes, deletes, nil
}

func (s *Service) ListAssets(ctx context.Context, id string) (*SkillPackage, []SkillFile, error) {
	sp, err := s.GetSkill(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	md, err := RenderSkillMD(sp)
	if err != nil {
		return nil, nil, err
	}
	f := fileWithHash("SKILL.md", []byte(md))
	f.Content = nil
	return sp, append([]SkillFile{f}, sp.Files...), nil
}

func (s *Service) ReadAsset(ctx context.Context, id, p string) (SkillFile, int64, error) {
	if err := ValidateAssetPath(p); err != nil {
		return SkillFile{}, 0, err
	}
	sp, err := s.store.GetSkill(ctx, id)
	if err != nil {
		return SkillFile{}, 0, err
	}
	if p == "SKILL.md" {
		md, err := RenderSkillMD(sp)
		return fileWithHash(p, []byte(md)), sp.Revision, err
	}
	f, err := s.store.GetSkillFile(ctx, sp.ID, p)
	if err != nil {
		return f, 0, err
	}
	// Detect concurrent writes between metadata and content reads.
	latest, err := s.store.GetSkill(ctx, sp.ID)
	if err != nil {
		return f, 0, err
	}
	if latest.Revision != sp.Revision {
		return f, 0, ErrConflict
	}
	return f, sp.Revision, nil
}

func (s *Service) PatchAssets(ctx context.Context, id string, patch AssetPatch) (*SkillPackage, bool, error) {
	sp, err := s.store.GetSkill(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if patch.Revision < 1 || sp.Revision != patch.Revision {
		return nil, false, ErrConflict
	}
	old, err := s.store.GetSkillFiles(ctx, sp.ID)
	if err != nil {
		return nil, false, err
	}
	md, err := RenderSkillMD(sp)
	if err != nil {
		return nil, false, err
	}
	desired := map[string]SkillFile{}
	for _, f := range old {
		desired[f.Path] = f
	}
	seen := map[string]bool{}
	for _, p := range patch.Deletes {
		if err := ValidateAssetPath(p); err != nil {
			return nil, false, err
		}
		if p == "SKILL.md" || seen[p] {
			return nil, false, fmt.Errorf("%w: 主文件不可删除或操作重复", ErrInvalidInput)
		}
		if _, ok := desired[p]; !ok {
			return nil, false, ErrNotFound
		}
		seen[p] = true
		delete(desired, p)
	}
	mdChanged := false
	for _, f := range patch.Writes {
		if err := ValidateAssetPath(f.Path); err != nil {
			return nil, false, err
		}
		if seen[f.Path] {
			return nil, false, fmt.Errorf("%w: 文件操作重复", ErrInvalidInput)
		}
		seen[f.Path] = true
		if len(f.Content) > MaxAssetBytes {
			return nil, false, fmt.Errorf("%w: 单文件不能超过 16 MiB", ErrInvalidInput)
		}
		if f.Path == "SKILL.md" {
			if !utf8.Valid(f.Content) {
				return nil, false, fmt.Errorf("%w: SKILL.md 必须为 UTF-8", ErrInvalidInput)
			}
			parsed, err := ParseSkillMD(string(f.Content))
			if err != nil {
				return nil, false, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
			if parsed.ID != sp.ID {
				return nil, false, fmt.Errorf("%w: 不可修改技能 id", ErrInvalidInput)
			}
			mdChanged = md != string(f.Content)
			parsed.CreatedAt = sp.CreatedAt
			parsed.Revision = sp.Revision
			parsed.ContentHash = sp.ContentHash
			sp = parsed
		} else {
			desired[f.Path] = f
		}
	}
	all := make([]SkillFile, 0, len(desired))
	for _, f := range desired {
		all = append(all, f)
	}
	writes, deletes, err := diffSkillFiles(old, all)
	if err != nil {
		return nil, false, err
	}
	if !mdChanged && len(writes) == 0 && len(deletes) == 0 {
		return sp, false, nil
	}
	sp.ContentHash = ComputeContentHash(sp, all)
	if err := s.store.SaveSkillChanges(ctx, sp, writes, deletes, patch.Revision); err != nil {
		return nil, false, err
	}
	return sp, true, nil
}

var versionLine = regexp.MustCompile(`(?m)^version:[^\r\n]*`)

func replaceSkillVersion(md, version string) string {
	m := frontMatterRe.FindStringSubmatch(strings.TrimPrefix(md, "\uFEFF"))
	if m == nil {
		return md
	}
	parsed, err := ParseSkillMD(md)
	if err == nil && parsed.Version == version {
		return md
	}
	header := m[1]
	if versionLine.MatchString(header) {
		header = versionLine.ReplaceAllString(header, "version: "+version)
	} else {
		header += "\nversion: " + version
	}
	return strings.Replace(md, m[1], header, 1)
}
