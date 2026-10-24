package workflowmcp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

const assetTestMD = "---\r\nname: assets-test\r\nid: skill.assets\r\nversion: 1.0.0\r\ncustom-key: keep-me # comment\r\n---\r\n# Guide\r\n"

func TestLegacyDirectoryDoesNotBlockAssetSave(t *testing.T) {
	ctx := context.Background()
	store := &recordingAssetsStore{MemoryStore: NewMemoryStore()}
	svc := NewService(store)
	sp, err := ParseSkillMD(assetTestMD)
	if err != nil {
		t.Fatal(err)
	}
	files := []SkillFile{fileWithHash("scripts/", nil), fileWithHash("scripts/run.py", []byte("keep")), fileWithHash("empty.txt", nil)}
	if err := store.SaveSkillChanges(ctx, sp, files, nil, 0); err != nil {
		t.Fatal(err)
	}
	_, index, err := svc.ListAssets(ctx, sp.ID)
	if err != nil || len(index) != 3 {
		t.Fatalf("index includes directory: %+v %v", index, err)
	}
	md := assetTestMD + "Updated instructions.\r\n"
	updated, changed, err := svc.PatchAssets(ctx, sp.ID, AssetPatch{Revision: 1, Writes: []SkillFile{{Path: "SKILL.md", Content: []byte(md)}}})
	if err != nil || !changed || updated.Revision != 2 {
		t.Fatalf("main document blocked: %+v %v", updated, err)
	}
	if len(store.writes) != 0 || len(store.deletes) != 0 {
		t.Fatal("untouched assets were written", store.writes, store.deletes)
	}
	exported, exportedMD, err := svc.ExportSkill(ctx, sp.ID)
	if err != nil || exportedMD != md || len(exported.Files) != 2 {
		t.Fatalf("export: %+v %q %v", exported, exportedMD, err)
	}
	if _, _, err := svc.PatchAssets(ctx, sp.ID, AssetPatch{Revision: 2, Writes: []SkillFile{{Path: "scripts/run.py", Content: []byte("updated")}}}); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetSkillFiles(ctx, sp.ID)
	if len(stored) != 3 {
		t.Fatal("historical marker or empty file was deleted", stored)
	}
	for _, invalid := range []SkillFile{{Path: "scripts/", Content: []byte("not a directory")}, {Path: "../scripts/"}} {
		if _, _, err := diffSkillFiles(nil, []SkillFile{invalid}); err == nil {
			t.Fatal("invalid file accepted", invalid.Path)
		}
	}
	if _, _, err := svc.PatchAssets(ctx, sp.ID, AssetPatch{Revision: 3, Writes: []SkillFile{{Path: "new/"}}}); err == nil {
		t.Fatal("new directory accepted as a file")
	}
	if _, _, err := svc.UpsertSkillMD(ctx, md, nil, "patch"); err != nil {
		t.Fatal("legacy omitted files still blocked", err)
	}
}

type recordingAssetsStore struct {
	*MemoryStore
	writes  []SkillFile
	deletes []string
	saves   int
}

func (m *recordingAssetsStore) SaveSkillChanges(ctx context.Context, sp *SkillPackage, writes []SkillFile, deletes []string, revision int64) error {
	err := m.MemoryStore.SaveSkillChanges(ctx, sp, writes, deletes, revision)
	if err == nil {
		m.writes = writes
		m.deletes = deletes
		m.saves++
	}
	return err
}

func TestAssetPatchOnlyWritesChangedFiles(t *testing.T) {
	ctx := context.Background()
	store := &recordingAssetsStore{MemoryStore: NewMemoryStore()}
	svc := NewService(store)
	sp, _, err := svc.UpsertSkillMD(ctx, assetTestMD, []SkillFile{{Path: "scripts/run.py", Content: []byte("old")}, {Path: "assets/raw.bin", Content: []byte{0, 255, 1}}, {Path: "delete.txt", Content: []byte("gone")}}, "patch")
	if err != nil {
		t.Fatal(err)
	}
	oldHash := sp.ContentHash
	updated, changed, err := svc.PatchAssets(ctx, sp.ID, AssetPatch{Revision: sp.Revision, Writes: []SkillFile{{Path: "scripts/run.py", Content: []byte("new")}, {Path: "new/empty.txt"}}, Deletes: []string{"delete.txt"}})
	if err != nil || !changed || updated.Revision != 2 {
		t.Fatalf("patch: %+v %v %v", updated, changed, err)
	}
	if len(store.writes) != 2 || len(store.deletes) != 1 {
		t.Fatalf("not incremental: %+v %+v", store.writes, store.deletes)
	}
	for _, f := range store.writes {
		if f.Content == nil {
			t.Fatal("empty content would be stored as SQL NULL")
		}
	}
	exported, md, err := svc.ExportSkill(ctx, sp.ID)
	if err != nil || md != assetTestMD || exported.Version != "1.0.0" || exported.ContentHash == oldHash {
		t.Fatalf("export modified original md: %q %+v %v", md, exported, err)
	}
	untouched, _, err := svc.ReadAsset(ctx, sp.ID, "assets/raw.bin")
	if err != nil || string(untouched.Content) != string([]byte{0, 255, 1}) {
		t.Fatalf("binary changed: %+v %v", untouched, err)
	}
	if _, _, err := svc.ReadAsset(ctx, sp.ID, "delete.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted file still exists")
	}
	empty, _, err := svc.ReadAsset(ctx, sp.ID, "new/empty.txt")
	if err != nil || empty.Size != 0 {
		t.Fatal("empty file not saved", err)
	}
	beforeSaves := store.saves
	noop, changed, err := svc.PatchAssets(ctx, sp.ID, AssetPatch{Revision: 2, Writes: []SkillFile{{Path: "SKILL.md", Content: []byte(md)}, {Path: "scripts/run.py", Content: []byte("new")}}})
	if err != nil || changed || store.saves != beforeSaves || noop.Revision != 2 || noop.ContentHash != updated.ContentHash {
		t.Fatalf("noop wrote: %+v %v %v", noop, changed, err)
	}
	check, err := svc.CheckSkill(ctx, sp.ID, "1.0.0", oldHash)
	if err != nil || check["status"] != "outdated" {
		t.Fatalf("hash update undetected: %v %v", check, err)
	}
	check, err = svc.CheckSkill(ctx, sp.ID, "1.0.0", updated.ContentHash)
	if err != nil || check["status"] != "current" {
		t.Fatal(check, err)
	}
}

func TestAssetPatchValidationIsAtomic(t *testing.T) {
	cases := []struct {
		name  string
		patch AssetPatch
	}{
		{"traversal", AssetPatch{Writes: []SkillFile{{Path: "../bad"}}}},
		{"backslash", AssetPatch{Writes: []SkillFile{{Path: `a\bad`}}}},
		{"drive", AssetPatch{Writes: []SkillFile{{Path: "C:/bad"}}}},
		{"delete-main", AssetPatch{Deletes: []string{"SKILL.md"}}},
		{"duplicate", AssetPatch{Writes: []SkillFile{{Path: "new"}, {Path: "new"}}}},
		{"case-collision", AssetPatch{Writes: []SkillFile{{Path: "KEEP.TXT"}}}},
		{"directory-collision", AssetPatch{Writes: []SkillFile{{Path: "keep.txt/child"}}}},
		{"main-directory-collision", AssetPatch{Writes: []SkillFile{{Path: "SKILL.md/child"}}}},
		{"reserved", AssetPatch{Writes: []SkillFile{{Path: "scripts/CON.py"}}}},
		{"invalid-md", AssetPatch{Writes: []SkillFile{{Path: "SKILL.md", Content: []byte("no id")}}}},
		{"changed-id", AssetPatch{Writes: []SkillFile{{Path: "SKILL.md", Content: []byte(strings.ReplaceAll(assetTestMD, "skill.assets", "other.id"))}}}},
		{"double-operation", AssetPatch{Writes: []SkillFile{{Path: "keep.txt"}}, Deletes: []string{"keep.txt"}}},
		{"missing-delete", AssetPatch{Deletes: []string{"absent"}}},
		{"oversized", AssetPatch{Writes: []SkillFile{{Path: "too-large", Content: make([]byte, MaxAssetBytes+1)}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc := NewService(NewMemoryStore())
			sp, _, err := svc.UpsertSkillMD(ctx, assetTestMD, []SkillFile{{Path: "keep.txt", Content: []byte("keep")}}, "")
			if err != nil {
				t.Fatal(err)
			}
			tc.patch.Revision = sp.Revision
			if _, _, err := svc.PatchAssets(ctx, sp.ID, tc.patch); err == nil {
				t.Fatal("invalid patch accepted")
			}
			after, md, err := svc.ExportSkill(ctx, sp.ID)
			if err != nil || after.Revision != 1 || len(after.Files) != 1 || string(after.Files[0].Content) != "keep" || md != assetTestMD {
				t.Fatalf("partial commit: %+v %v", after, err)
			}
		})
	}
}

func TestAssetWritersConflictAndLegacyOmissionPreservesFiles(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryStore())
	sp, _, err := svc.UpsertSkillMD(ctx, assetTestMD, []SkillFile{{Path: "keep", Content: []byte("keep")}}, "")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, value := range []string{"one", "two"} {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			_, _, err := svc.PatchAssets(ctx, sp.ID, AssetPatch{Revision: 1, Writes: []SkillFile{{Path: "keep", Content: []byte(value)}}})
			results <- err
		}(value)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("writers: success=%d conflicts=%d", success, conflicts)
	}
	sp, _, err = svc.UpsertSkillMD(ctx, assetTestMD, nil, "patch")
	if err != nil || sp.Revision != 3 {
		t.Fatal(sp, err)
	}
	exported, md, err := svc.ExportSkill(ctx, sp.ID)
	if err != nil || len(exported.Files) != 1 || !strings.Contains(md, "custom-key: keep-me # comment") || !strings.Contains(md, "version: 1.0.1") {
		t.Fatal(exported, md, err)
	}
	if _, _, err := svc.PatchAssets(ctx, sp.ID, AssetPatch{Revision: 2}); !errors.Is(err, ErrConflict) {
		t.Fatal("legacy writer didn't invalidate stale drafts", err)
	}
}
