package s3_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kasidit-wansudon/flowforge/internal/storage/s3"
)

// --- helpers ---

func newStore(t *testing.T) *s3.LocalArtifactStore {
	t.Helper()
	dir := t.TempDir()
	store, err := s3.NewLocalArtifactStore(dir)
	if err != nil {
		t.Fatalf("NewLocalArtifactStore: %v", err)
	}
	return store
}

func upload(t *testing.T, store *s3.LocalArtifactStore, key, content string) {
	t.Helper()
	ctx := context.Background()
	if err := store.Upload(ctx, key, strings.NewReader(content)); err != nil {
		t.Fatalf("Upload(%q): %v", key, err)
	}
}

// --- NewLocalArtifactStore ---

func TestNewLocalArtifactStore_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "sub")
	_, err := s3.NewLocalArtifactStore(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("base directory was not created: %s", dir)
	}
}

// --- Upload + Download ---

func TestLocalArtifactStore_UploadAndDownload_Roundtrip(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	content := "hello, artifact!"

	if err := store.Upload(ctx, "artifacts/test.txt", strings.NewReader(content)); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	rc, err := store.Download(ctx, "artifacts/test.txt")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if string(got) != content {
		t.Errorf("got %q, want %q", string(got), content)
	}
}

func TestLocalArtifactStore_Upload_NestedKey(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if err := store.Upload(ctx, "builds/2024/01/artifact.tar.gz", strings.NewReader("binary")); err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	rc, err := store.Download(ctx, "builds/2024/01/artifact.tar.gz")
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	rc.Close()
}

func TestLocalArtifactStore_Download_NotFound(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	_, err := store.Download(ctx, "nonexistent/file.txt")
	if err == nil {
		t.Fatal("expected error for missing artifact")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestLocalArtifactStore_Upload_FilePersistsOnDisk(t *testing.T) {
	dir := t.TempDir()
	store, err := s3.NewLocalArtifactStore(dir)
	if err != nil {
		t.Fatalf("NewLocalArtifactStore: %v", err)
	}

	ctx := context.Background()
	if err := store.Upload(ctx, "persistent.json", bytes.NewReader([]byte(`{}`))); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Verify file exists at the expected path.
	expected := filepath.Join(dir, "persistent.json")
	if _, err := os.Stat(expected); os.IsNotExist(err) {
		t.Errorf("file not found on disk at %s", expected)
	}
}

// --- Delete ---

func TestLocalArtifactStore_Delete_RemovesArtifact(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	upload(t, store, "to-delete.log", "some logs")

	if err := store.Delete(ctx, "to-delete.log"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err := store.Download(ctx, "to-delete.log")
	if err == nil {
		t.Error("expected error after delete, artifact still exists")
	}
}

func TestLocalArtifactStore_Delete_NonExistentIsIdempotent(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	// Deleting a non-existent key should not return an error.
	if err := store.Delete(ctx, "never-existed.txt"); err != nil {
		t.Errorf("Delete of non-existent key should be idempotent, got error: %v", err)
	}
}

// --- List ---

func TestLocalArtifactStore_List_EmptyPrefix(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	upload(t, store, "file-a.txt", "a")
	upload(t, store, "file-b.json", "b")
	upload(t, store, "other/file-c.yaml", "c")

	artifacts, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(artifacts) != 3 {
		t.Errorf("expected 3 artifacts, got %d", len(artifacts))
	}
}

func TestLocalArtifactStore_List_WithPrefix(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	upload(t, store, "logs/2024/01.log", "jan")
	upload(t, store, "logs/2024/02.log", "feb")
	upload(t, store, "artifacts/build.zip", "zip")

	artifacts, err := store.List(ctx, "logs/")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(artifacts) != 2 {
		t.Errorf("expected 2 artifacts with prefix 'logs/', got %d", len(artifacts))
	}
	for _, a := range artifacts {
		if !strings.HasPrefix(a.Key, "logs/") {
			t.Errorf("artifact key %q does not match prefix 'logs/'", a.Key)
		}
	}
}

func TestLocalArtifactStore_List_SortedByKey(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	upload(t, store, "z.txt", "z")
	upload(t, store, "a.txt", "a")
	upload(t, store, "m.txt", "m")

	artifacts, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for i := 1; i < len(artifacts); i++ {
		if artifacts[i-1].Key >= artifacts[i].Key {
			t.Errorf("list not sorted: %q >= %q at index %d", artifacts[i-1].Key, artifacts[i].Key, i)
		}
	}
}

func TestLocalArtifactStore_List_ArtifactInfoFields(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	content := "json content"
	upload(t, store, "data.json", content)

	artifacts, err := store.List(ctx, "data.json")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	a := artifacts[0]
	if a.Key != "data.json" {
		t.Errorf("expected key 'data.json', got %q", a.Key)
	}
	if a.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), a.Size)
	}
	if a.ContentType != "application/json" {
		t.Errorf("expected content type 'application/json', got %q", a.ContentType)
	}
	if a.LastModified.IsZero() {
		t.Error("LastModified should not be zero")
	}
}
