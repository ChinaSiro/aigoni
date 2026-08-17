package content

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceRejectsSlugConflict(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	service := NewService(repo)
	input := SaveInput{Type: TypePost, Title: "One", Date: time.Now().UTC(), Slug: "same"}
	if _, _, err := service.Create(input); err != nil {
		t.Fatal(err)
	}
	input.Title = "Two"
	if _, _, err := service.Create(input); !errors.Is(err, ErrSlugConflict) {
		t.Fatalf("error = %v, want ErrSlugConflict", err)
	}
}

func TestServiceRevisionConflict(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(filepath.Join(dir, "posts"), filepath.Join(dir, "pages"), filepath.Join(dir, "notes"))
	service := NewService(repo)
	item, revision, err := service.Create(SaveInput{
		Type: TypePage, Title: "Page", Date: time.Now().UTC(), Slug: "page", Body: "one",
	})
	if err != nil {
		t.Fatal(err)
	}
	input := SaveInput{ID: item.ID, Type: TypePage, Title: "Page", Date: item.Date, Slug: "page", Body: "two"}
	updated, nextRevision, err := service.Update(input, revision)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Body != "two" || nextRevision == revision {
		t.Fatalf("updated = %+v, revisions = %q %q", updated, revision, nextRevision)
	}
	input.Body = "stale"
	if _, _, err := service.Update(input, revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("error = %v, want ErrRevisionConflict", err)
	}
	if err := service.Delete(item.ID, TypePage, revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("delete error = %v, want ErrRevisionConflict", err)
	}
}

func TestCleanupExpiredDraftsRemovesOnlyExpiredValidDraftDirs(t *testing.T) {
	dir := t.TempDir()
	svc := NewResourceService(nil, filepath.Join(dir, "content"), nil)
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	draftsDir := filepath.Join(dir, "content", ".drafts")

	expired := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.assets"
	fresh := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.assets"
	invalid := "not-a-valid-token.assets"
	plainFile := "cccccccccccccccccccccccccccccccc.assets"

	mustMkdir(t, filepath.Join(draftsDir, expired))
	mustMkdir(t, filepath.Join(draftsDir, fresh))
	mustMkdir(t, filepath.Join(draftsDir, invalid))
	mustWriteFile(t, filepath.Join(draftsDir, plainFile), "file")
	mustChtimes(t, filepath.Join(draftsDir, expired), now.Add(-8*24*time.Hour))
	mustChtimes(t, filepath.Join(draftsDir, fresh), now.Add(-time.Hour))
	mustChtimes(t, filepath.Join(draftsDir, invalid), now.Add(-8*24*time.Hour))
	mustChtimes(t, filepath.Join(draftsDir, plainFile), now.Add(-8*24*time.Hour))

	if got := svc.cleanupExpiredDrafts(now); got != 1 {
		t.Fatalf("removed = %d, want 1", got)
	}
	assertNotExist(t, filepath.Join(draftsDir, expired))
	assertExists(t, filepath.Join(draftsDir, fresh))
	assertExists(t, filepath.Join(draftsDir, invalid))
	assertExists(t, filepath.Join(draftsDir, plainFile))
}

func TestCleanupExpiredDraftsContinuesAfterRemoveFailure(t *testing.T) {
	dir := t.TempDir()
	svc := NewResourceService(nil, filepath.Join(dir, "content"), nil)
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	draftsDir := filepath.Join(dir, "content", ".drafts")
	failed := filepath.Join(draftsDir, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.assets")
	removed := filepath.Join(draftsDir, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.assets")
	mustMkdir(t, failed)
	mustMkdir(t, removed)
	mustChtimes(t, failed, now.Add(-8*24*time.Hour))
	mustChtimes(t, removed, now.Add(-8*24*time.Hour))

	got := svc.cleanupExpiredDraftsWithRemove(now, func(path string) error {
		if path == failed {
			return errors.New("remove failed")
		}
		return os.RemoveAll(path)
	})
	if got != 1 {
		t.Fatalf("removed = %d, want 1", got)
	}
	assertExists(t, failed)
	assertNotExist(t, removed)
}

func TestStartDraftCleanupRunsImmediatelyAndStopsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	svc := NewResourceService(nil, filepath.Join(dir, "content"), nil)
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	expired := filepath.Join(dir, "content", ".drafts", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.assets")
	mustMkdir(t, expired)
	mustChtimes(t, expired, now.Add(-8*24*time.Hour))

	ctx, cancel := context.WithCancel(context.Background())
	done := svc.startDraftCleanup(ctx, time.Hour, func() time.Time { return now })
	waitForPathRemoved(t, expired)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("draft cleanup did not stop after context cancel")
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustChtimes(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s should exist: %v", path, err)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s should not exist: %v", path, err)
	}
}

func waitForPathRemoved(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%s still exists", path)
}
