// Package storage provides the disk abstraction for file storage: local
// private/public roots and S3-compatible object storage (aws-sdk-go-v2).
package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/config"
)

// Disk names used by the application.
const (
	DiskPrivate = "private"
	DiskPublic  = "public"
	DiskS3      = "s3"
)

// Disk stores and serves file contents.
type Disk interface {
	Put(ctx context.Context, path string, contents io.Reader, size int64) error
	Get(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
	Exists(ctx context.Context, path string) (bool, error)
	Size(ctx context.Context, path string) (int64, error)
	URL(ctx context.Context, path string) (string, error)
}

// Registry exposes the configured disks by name.
type Registry struct {
	disks map[string]Disk
}

// New builds the disk registry: a private local disk, a public local disk
// served at {APP_URL}/storage, and (when configured) the S3 disk.
func New(cfg *config.Config, logger *slog.Logger) (*Registry, error) {
	disks := map[string]Disk{
		DiskPrivate: newLocalDisk(DiskPrivate, cfg.Storage.PrivateDir, ""),
		DiskPublic:  newLocalDisk(DiskPublic, cfg.Storage.PublicDir, cfg.App.URL),
	}
	if cfg.Storage.S3.Configured() {
		s3, err := newS3Disk(cfg.Storage.S3)
		if err != nil {
			return nil, err
		}
		disks[DiskS3] = s3
	} else if logger != nil {
		logger.Debug("s3 disk not configured; S3_* settings are empty")
	}
	return &Registry{disks: disks}, nil
}

// Disk returns a configured disk by name.
func (r *Registry) Disk(name string) (Disk, error) {
	disk, ok := r.disks[name]
	if !ok {
		return nil, fmt.Errorf("storage: disk %q is not configured", name)
	}
	return disk, nil
}

// Names lists the configured disk names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.disks))
	for name := range r.disks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
