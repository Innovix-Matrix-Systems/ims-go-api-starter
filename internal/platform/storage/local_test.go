package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalDiskContract(t *testing.T) {
	disk := newLocalDisk(DiskPublic, t.TempDir(), "https://api.test")
	ctx := context.Background()
	const path = "files/abc123/photo.png"

	if exists, err := disk.Exists(ctx, path); err != nil || exists {
		t.Fatalf("Exists() before put = %v, %v", exists, err)
	}
	if err := disk.Put(ctx, path, strings.NewReader("binary"), 6); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if exists, err := disk.Exists(ctx, path); err != nil || !exists {
		t.Fatalf("Exists() after put = %v, %v", exists, err)
	}
	if size, err := disk.Size(ctx, path); err != nil || size != 6 {
		t.Fatalf("Size() = %d, %v", size, err)
	}
	reader, err := disk.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	contents, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(contents) != "binary" {
		t.Fatalf("Get() contents = %q, %v", contents, err)
	}
	url, err := disk.URL(ctx, path)
	if err != nil || url != "https://api.test/storage/"+path {
		t.Fatalf("URL() = %q, %v", url, err)
	}
	if err := disk.Delete(ctx, path); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if exists, _ := disk.Exists(ctx, path); exists {
		t.Fatal("file still exists after delete")
	}
	// Deleting a missing file is idempotent.
	if err := disk.Delete(ctx, path); err != nil {
		t.Fatalf("Delete() missing error = %v", err)
	}
}

func TestLocalDiskNeutralizesTraversal(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	disk := newLocalDisk(DiskPublic, root, "")
	ctx := context.Background()

	// The path is cleaned onto the disk root; nothing escapes.
	if err := disk.Put(ctx, "../escape.txt", strings.NewReader("x"), 1); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if exists, _ := disk.Exists(ctx, "escape.txt"); !exists {
		t.Error("cleaned path not stored inside the disk root")
	}
	if _, err := os.Stat(filepath.Join(base, "escape.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file escaped the disk root: %v", err)
	}
}

func TestPrivateDiskHasNoPublicURL(t *testing.T) {
	disk := newLocalDisk(DiskPrivate, t.TempDir(), "")
	if _, err := disk.URL(context.Background(), "jobs/export/x.xlsx"); err == nil {
		t.Fatal("URL() on a private disk = nil error")
	}
}
