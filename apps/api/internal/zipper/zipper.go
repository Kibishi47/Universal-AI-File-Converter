package zipper

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/storage"
)

type FileEntry struct {
	FileName   string
	StorageKey string
}

// StreamZip writes a ZIP archive on the fly directly to the output writer without saving to disk
func StreamZip(ctx context.Context, store *storage.Storage, entries []FileEntry, out io.Writer) error {
	zipWriter := zip.NewWriter(out)
	defer zipWriter.Close()

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		obj, err := store.Download(ctx, entry.StorageKey)
		if err != nil {
			return fmt.Errorf("failed to read file %s from storage: %w", entry.StorageKey, err)
		}

		header := &zip.FileHeader{
			Name:     entry.FileName,
			Method:   zip.Deflate,
			Modified: time.Now(),
		}

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			_ = obj.Close()
			return fmt.Errorf("failed to create zip header for %s: %w", entry.FileName, err)
		}

		if _, err := io.Copy(writer, obj); err != nil {
			_ = obj.Close()
			return fmt.Errorf("failed to stream content for %s: %w", entry.FileName, err)
		}
		_ = obj.Close()
	}

	return nil
}
