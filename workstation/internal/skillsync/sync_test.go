package skillsync_test

import (
	"os"
	"path/filepath"
	"testing"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/skillsync"
)

func TestSyncAndPrunePreserveWorkspace(t *testing.T) {
	dir := t.TempDir()
	pkgDir := filepath.Join(dir, "demo-skill")
	_ = os.MkdirAll(filepath.Join(pkgDir, "workspace"), 0o755)
	_ = os.WriteFile(filepath.Join(pkgDir, "config.json"), []byte(`{}`), 0o644)
	_ = os.WriteFile(filepath.Join(pkgDir, "old.txt"), []byte("gone"), 0o644)

	pkgs := []*aiev1.SkillPackage{{
		Id: "demo.skill", CursorName: "demo-skill", Version: "1.0.0",
		SkillMd: "---\nid: demo.skill\nname: demo-skill\n---\n# Demo\n",
		ContentHash: "hash1",
		Files: []*aiev1.SkillFile{
			{Path: "helper.py", Content: []byte("print(1)")},
		},
	}}
	res, err := skillsync.Sync(pkgs, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Installed)+len(res.Updated) == 0 && len(res.Skipped) == 0 {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(filepath.Join(pkgDir, "workspace")); err != nil {
		t.Fatal("workspace should remain")
	}
	if _, err := os.Stat(filepath.Join(pkgDir, "config.json")); err != nil {
		t.Fatal("config.json should remain")
	}
	if _, err := os.Stat(filepath.Join(pkgDir, "old.txt")); err == nil {
		t.Fatal("old.txt should be pruned")
	}
	// 增量跳过
	res2, err := skillsync.Sync(pkgs, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.Skipped) != 1 {
		t.Fatalf("expected skip: %+v", res2)
	}
}

func TestSyncFromStartJob(t *testing.T) {
	// 1. nil start job
	res, err := skillsync.SyncFromStartJob(nil)
	if err != nil || len(res.Installed) != 0 {
		t.Fatalf("expected empty result for nil: %v %+v", err, res)
	}

	// 2. populated start job
	dir := t.TempDir()
	t.Setenv("CURSOR_SKILLS_DIR", dir)

	start := &aiev1.StartJobPayload{
		SkillPackages: []*aiev1.SkillPackage{
			{
				Id:         "from.job",
				CursorName: "job-skill",
				Version:    "1.0.0",
				SkillMd:    "---\nid: from.job\nname: job-skill\n---\n# Content\n",
				Files: []*aiev1.SkillFile{
					{Path: "main.py", Content: []byte("print('job')")},
				},
			},
		},
	}
	res2, err := skillsync.SyncFromStartJob(start)
	if err != nil {
		t.Fatalf("SyncFromStartJob failed: %v", err)
	}
	if len(res2.Installed) != 1 || res2.Installed[0] != "job-skill" {
		t.Fatalf("unexpected installed: %+v", res2)
	}
	if _, err := os.Stat(filepath.Join(dir, "job-skill", "main.py")); err != nil {
		t.Fatal("expected main.py to exist")
	}
}

func TestSyncPathTraversalProtection(t *testing.T) {
	dir := t.TempDir()
	pkgs := []*aiev1.SkillPackage{
		{
			Id:         "traversal.test",
			CursorName: "traversal-skill",
			Version:    "1.0.0",
			SkillMd:    "---\nid: traversal.test\nname: traversal-skill\n---\n# Traversal\n",
			Files: []*aiev1.SkillFile{
				{Path: "../escaped.txt", Content: []byte("malicious")},
				{Path: "safe.txt", Content: []byte("safe content")},
			},
		},
	}
	res, err := skillsync.Sync(pkgs, dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Installed) != 1 {
		t.Fatalf("expected 1 installed, got %+v", res)
	}

	// 验证根目录下没有 escaped.txt
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); err == nil {
		t.Fatal("escaped.txt should NOT be written due to path traversal protection")
	}
	// 验证正常文件写入
	if _, err := os.Stat(filepath.Join(dir, "traversal-skill", "safe.txt")); err != nil {
		t.Fatal("safe.txt should exist")
	}
}

func TestSyncPruneNames(t *testing.T) {
	dir := t.TempDir()
	obsoleteDir := filepath.Join(dir, "old-obsolete-skill")
	_ = os.MkdirAll(obsoleteDir, 0o755)
	_ = os.WriteFile(filepath.Join(obsoleteDir, "SKILL.md"), []byte("old"), 0o644)

	pkgs := []*aiev1.SkillPackage{
		{
			Id:         "keep.skill",
			CursorName: "keep-skill",
			Version:    "1.0.0",
			SkillMd:    "---\nid: keep.skill\nname: keep-skill\n---\n# Keep\n",
		},
	}
	res, err := skillsync.Sync(pkgs, dir, []string{"old-obsolete-skill", "keep-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Removed) != 1 || res.Removed[0] != "old-obsolete-skill" {
		t.Fatalf("expected old-obsolete-skill removed: %+v", res)
	}
	if _, err := os.Stat(obsoleteDir); err == nil {
		t.Fatal("obsoleteDir should be removed")
	}
}

func TestTargetDirEnv(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-cursor-skills")
	t.Setenv("CURSOR_SKILLS_DIR", custom)
	if got := skillsync.TargetDir(); got != custom {
		t.Fatalf("expected %s, got %s", custom, got)
	}
}

