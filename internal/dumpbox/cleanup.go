package dumpbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	tempPrefix      = ".upload-"
	staleUploadAge  = 12 * time.Hour
	cleanupInterval = time.Hour
)

// CleanIncompleteUploads removes temporary files left behind by uploads that
// never completed, for example after a crash or a restart. Files younger than
// olderThan are kept so that uploads in progress are not disturbed.
func (s *Server) CleanIncompleteUploads(olderThan time.Duration) {
	cutoff := s.now().Add(-olderThan)
	directories, err := os.ReadDir(s.dataDir)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Error("scan data directory", "error", err)
		}
		return
	}
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		path := filepath.Join(s.dataDir, directory.Name())
		entries, err := os.ReadDir(path)
		if err != nil {
			s.logger.Error("scan user directory", "directory", directory.Name(), "error", err)
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), tempPrefix) {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(cutoff) {
				continue
			}
			if err := os.Remove(filepath.Join(path, entry.Name())); err != nil && !os.IsNotExist(err) {
				s.logger.Error("remove incomplete upload", "file", entry.Name(), "error", err)
				continue
			}
			s.logger.Info("removed incomplete upload", "file", entry.Name())
		}
	}
}

// RunCleanup removes leftovers from incomplete uploads immediately and then
// repeats the sweep until the context is cancelled.
func (s *Server) RunCleanup(ctx context.Context) {
	s.CleanIncompleteUploads(0)
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.CleanIncompleteUploads(staleUploadAge)
		}
	}
}
